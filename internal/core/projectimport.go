package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// ProjectImportRequest imports a project file.
type ProjectImportRequest struct {
	Data     []byte `json:"data,omitempty"`
	Path     string `json:"path,omitempty"`
	Filename string `json:"filename,omitempty"`
	// Name is the project's name here (default: the file's; a name that's
	// taken gets " (2)").
	Name string `json:"name,omitempty"`
	// Fetch downloads the datasheets of parts the library doesn't have:
	// from where they came from, else by part number.
	Fetch bool `json:"fetch"`
}

// Part outcomes of a project import.
const (
	PartInLibrary    = "in library"
	PartOtherVersion = "other datasheet"
	PartImported     = "imported"
	PartNoDatasheet  = "no datasheet"
)

// ProjectImportPart is what happened to one part.
type ProjectImportPart struct {
	MPN string `json:"mpn"`
	// Part is the library part it became, when its name differs.
	Part   string `json:"part,omitempty"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

// ProjectImportResult summarizes a project import.
type ProjectImportResult struct {
	Project string              `json:"project"`
	Parts   []ProjectImportPart `json:"parts"`
	Counts  map[string]int      `json:"counts"`
	// Notes are what didn't come across as it was (a model the library
	// already has in another version, a connection to a pin of a part
	// that couldn't be found).
	Notes []string `json:"notes,omitempty"`
	Tests int      `json:"tests"`
	Runs  int      `json:"runs"`
}

// Limits on what a project file may hold.
const (
	maxProjectFile  = 2 << 30   // the whole zip
	maxProjectJSON  = 64 << 20  // project.json
	maxProjectFiles = 20000     // entries in the zip
	maxUnpacked     = 4 << 30   // everything, unpacked
	maxRunFile      = 512 << 20 // one run file, unpacked
)

// runFileRe is a run's file in the zip: runs/<n>/<name>, no further
// down; runFolderRe is a run's folder as project.json names it.
var (
	runFileRe   = regexp.MustCompile(`^runs/(\d{1,6})/([^/]{1,200})$`)
	runFolderRe = regexp.MustCompile(`^runs/\d{1,6}$`)
)

// ImportProject reads a project file into the library: its parts are
// found in the library (by part number, orderable code or family) or,
// with Fetch, their datasheets downloaded, from where they came from or
// by part number; a part that can't be had joins as having no datasheet,
// so the BOM stays complete. Then the project is made with everything in
// it, its tests' runs with their files. Nothing is left behind when it
// fails.
func (s *Service) ImportProject(ctx context.Context, req ProjectImportRequest) (ProjectImportResult, error) {
	data := req.Data
	if len(data) == 0 && req.Path != "" {
		b, err := readSmallFile(expandHome(req.Path), maxProjectFile)
		if err != nil {
			return ProjectImportResult{}, err
		}
		data = b
	}
	pf, runFiles, err := readProjectFile(data)
	if err != nil {
		return ProjectImportResult{}, err
	}
	out := ProjectImportResult{Counts: map[string]int{}}
	parts, err := s.importParts(ctx, pf.Parts, req.Fetch, &out)
	if err != nil {
		return out, err
	}
	name, err := s.freeProjectName(ctx, firstNonEmpty(req.Name, pf.Name))
	if err != nil {
		return out, err
	}
	p, err := s.Lib.CreateProject(ctx, name, pf.Description)
	if err != nil {
		return out, err
	}
	out.Project = p.Name
	if err := s.buildProject(ctx, p, pf, parts, runFiles, &out); err != nil {
		_ = s.Lib.DeleteProject(context.WithoutCancel(ctx), p.ID)
		return ProjectImportResult{}, fmt.Errorf("importing %s: %w", pf.Name, err)
	}
	s.ProjectChanged(p.Name, "import")
	s.emit(EvLibrary, map[string]any{"action": "project-import"})
	return out, nil
}

// readProjectFile opens a project file, checking it's one this version
// can read and holds nothing it shouldn't: project.json and the runs'
// files, in bounds.
func readProjectFile(data []byte) (ProjectFile, map[string]*zip.File, error) {
	var pf ProjectFile
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return pf, nil, fmt.Errorf("not a project file (%s): %w", ProjectFileExt, err)
	}
	if len(zr.File) > maxProjectFiles {
		return pf, nil, fmt.Errorf("the project file holds %d files, more than the %d allowed", len(zr.File), maxProjectFiles)
	}
	var total uint64
	var js *zip.File
	runs := map[string]*zip.File{}
	for _, f := range zr.File {
		total += f.UncompressedSize64
		switch {
		case f.Name == projectJSON:
			js = f
		case strings.HasSuffix(f.Name, "/"):
			// a folder entry
		case runFileRe.MatchString(f.Name):
			if f.UncompressedSize64 > maxRunFile {
				return pf, nil, fmt.Errorf("%s is too large", f.Name)
			}
			runs[f.Name] = f
		default:
			return pf, nil, fmt.Errorf("unexpected file in the project file: %q", f.Name)
		}
	}
	if total > maxUnpacked {
		return pf, nil, errors.New("the project file unpacks to more than allowed")
	}
	if js == nil {
		return pf, nil, fmt.Errorf("not a project file: no %s", projectJSON)
	}
	b, err := readZipFile(js, maxProjectJSON)
	if err != nil {
		return pf, nil, err
	}
	if err := json.Unmarshal(b, &pf); err != nil {
		return pf, nil, fmt.Errorf("%s: %w", projectJSON, err)
	}
	if pf.Format != ProjectFileFormat {
		return pf, nil, fmt.Errorf("not a SheetDiver project file (format %q)", pf.Format)
	}
	if pf.Version > ProjectFileVersion {
		return pf, nil, fmt.Errorf("the project file is version %d, from a newer SheetDiver (this one reads up to %d): update SheetDiver", pf.Version, ProjectFileVersion)
	}
	if strings.TrimSpace(pf.Name) == "" {
		return pf, nil, errors.New("the project file names no project")
	}
	return pf, runs, nil
}

// readZipFile reads a zip entry, refusing more than max bytes.
func readZipFile(f *zip.File, max int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s is too large", f.Name)
	}
	return b, nil
}

// importParts finds or fetches each part, returning the library part for
// each of the file's part numbers. A project names exact parts, so only
// an exact match counts (the part number or one of its orderable codes):
// a family or prefix match could be another value of a capacitor series.
func (s *Service) importParts(ctx context.Context, fps []FilePart, fetch bool, out *ProjectImportResult) (map[string]library.Part, error) {
	parts := map[string]library.Part{}
	var missing []FilePart
	had := map[string]bool{}
	for _, fp := range fps {
		if _, ok := s.exactPart(ctx, fp.MPN); ok {
			had[strings.ToUpper(fp.MPN)] = true
			continue
		}
		// The library has its datasheet under other part numbers (a
		// series' datasheet): the part joins it, nothing to download.
		if fp.Datasheet != nil && fp.Datasheet.SHA256 != "" {
			if d, ok, err := s.Lib.DatasheetBySHA(ctx, fp.Datasheet.SHA256); err == nil && ok {
				if err := s.attachPart(ctx, fp, d); err != nil {
					return nil, fmt.Errorf("adding %s: %w", fp.MPN, err)
				}
				had[strings.ToUpper(fp.MPN)] = true
				continue
			}
		}
		missing = append(missing, fp)
	}
	fetchErr := map[string]string{}
	if fetch && len(missing) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for _, fp := range missing {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if err := s.fetchPart(ctx, fp); err != nil {
					mu.Lock()
					fetchErr[strings.ToUpper(fp.MPN)] = err.Error()
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
	}
	for _, fp := range fps {
		row := ProjectImportPart{MPN: fp.MPN}
		res, ok := s.exactPart(ctx, fp.MPN)
		switch {
		case ok && res.Part.DatasheetID != 0:
			row.Status = PartImported
			if had[strings.ToUpper(fp.MPN)] {
				row.Status = PartInLibrary
				if fp.Datasheet != nil {
					if d, err := s.Lib.Datasheet(ctx, res.Part.DatasheetID); err == nil && d.SHA256 != fp.Datasheet.SHA256 {
						row.Status = PartOtherVersion
						row.Note = "the library has " + firstNonEmpty(d.Meta["revision"], "another revision") + "; the project was made with " + firstNonEmpty(fp.Datasheet.Revision, fp.Datasheet.Filename, "another one")
					}
				}
			}
		case ok:
			row.Status = PartNoDatasheet
		default:
			// Keep the BOM complete: a part without a datasheet.
			p, err := s.Lib.AddPart(ctx, library.Part{MPN: fp.MPN, Manufacturer: fp.Manufacturer, Description: fp.Description, Category: fp.Category, Package: fp.Package})
			if err != nil {
				return nil, fmt.Errorf("adding %s: %w", fp.MPN, err)
			}
			res = library.Resolution{Part: p, Via: "mpn"}
			row.Status = PartNoDatasheet
			switch {
			case fetchErr[strings.ToUpper(fp.MPN)] != "":
				row.Note = fetchErr[strings.ToUpper(fp.MPN)]
			case !fetch:
				row.Note = "not in the library (fetching was off)"
			}
		}
		if !strings.EqualFold(res.Part.MPN, fp.MPN) {
			row.Part = res.Part.MPN
		}
		parts[strings.ToUpper(fp.MPN)] = res.Part
		out.Parts = append(out.Parts, row)
		out.Counts[row.Status]++
	}
	return parts, nil
}

// exactPart finds the part a part number names exactly: the part itself
// or one of its orderable codes, never a family or prefix match.
func (s *Service) exactPart(ctx context.Context, mpn string) (library.Resolution, bool) {
	res, err := s.Lib.Resolve(ctx, mpn)
	if err != nil || (res.Via != "mpn" && res.Via != "variant") {
		return res, false
	}
	return res, true
}

// fetchPart downloads a part's datasheet: from where the project's came
// from, else by part number from the manufacturer. A datasheet that
// doesn't name the part itself (a capacitor series, a JLCPCB copy) gets
// the part attached, as the project knew it.
func (s *Service) fetchPart(ctx context.Context, fp FilePart) error {
	var errs []string
	if fp.Datasheet != nil && fp.Datasheet.SourceURL != "" {
		res, err := s.Import(ctx, ImportRequest{URL: fp.Datasheet.SourceURL, Manufacturer: fp.Manufacturer})
		if err == nil {
			return s.attachPart(ctx, fp, res.Datasheet)
		}
		errs = append(errs, err.Error())
	}
	res, err := s.Import(ctx, ImportRequest{Lookup: fp.MPN, Manufacturer: fp.Manufacturer})
	if err == nil {
		return s.attachPart(ctx, fp, res.Datasheet)
	}
	if len(errs) > 0 {
		return fmt.Errorf("from %s: %s; by part number: %v", fp.Datasheet.SourceURL, errs[0], err)
	}
	return err
}

// attachPart makes sure a datasheet gives the part: when the library
// has no part by exactly that number, it's added for the datasheet with
// what the project knew of it.
func (s *Service) attachPart(ctx context.Context, fp FilePart, d library.Datasheet) error {
	if _, ok := s.exactPart(ctx, fp.MPN); ok {
		return nil
	}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	if _, ok := s.exactPart(ctx, fp.MPN); ok {
		return nil
	}
	_, err := s.Lib.AddPart(ctx, library.Part{MPN: fp.MPN, Manufacturer: firstNonEmpty(fp.Manufacturer, d.Manufacturer), Description: fp.Description, Category: fp.Category, Package: fp.Package, DatasheetID: d.ID})
	return err
}

// freeProjectName returns name, or name (2), (3)… when it's taken.
func (s *Service) freeProjectName(ctx context.Context, name string) (string, error) {
	name = strings.TrimSpace(name)
	for n := 1; n < 1000; n++ {
		try := name
		if n > 1 {
			try = name + " (" + strconv.Itoa(n) + ")"
		}
		if _, err := s.Lib.Project(ctx, try); errors.Is(err, library.ErrNotFound) {
			return try, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("no free name for %s", name)
}

// buildProject makes everything in a project file in a new project.
func (s *Service) buildProject(ctx context.Context, p library.Project, pf ProjectFile, parts map[string]library.Part, runFiles map[string]*zip.File, out *ProjectImportResult) error {
	note := func(format string, args ...any) { out.Notes = append(out.Notes, fmt.Sprintf(format, args...)) }
	for _, r := range pf.Rails {
		if _, err := s.Lib.SetRail(ctx, library.Rail{ProjectID: p.ID, Name: r.Name, VNom: r.VNom, VMin: r.VMin, VMax: r.VMax, Source: r.Source, CurrentLimit: r.CurrentLimit, OtherTyp: r.OtherTyp, OtherMax: r.OtherMax, Notes: r.Notes}); err != nil {
			return fmt.Errorf("rail %s: %w", r.Name, err)
		}
	}
	entries := map[string]int64{}
	for _, e := range pf.Entries {
		part, ok := parts[strings.ToUpper(e.Part)]
		if !ok {
			// A part the file's list missed: resolve it as it is.
			res, err := s.Lib.Resolve(ctx, e.Part)
			if err != nil {
				note("BOM entry %s: no part %s", e.Key, e.Part)
				continue
			}
			part = res.Part
		}
		pp, err := s.Lib.AddProjectPart(ctx, library.ProjectPart{ProjectID: p.ID, PartID: part.ID, Designator: e.Designator, Qty: e.Qty, Role: e.Role, SupplyRail: e.SupplyRail, OutputRail: e.OutputRail, ITyp: e.ITyp, IMax: e.IMax, Variant: e.Variant, Notes: e.Notes})
		if err != nil {
			return fmt.Errorf("BOM entry %s: %w", e.Key, err)
		}
		entries[e.Key] = pp.ID
	}
	for _, n := range pf.Nets {
		if _, err := s.Lib.SetNet(ctx, library.Net{ProjectID: p.ID, Name: n.Name, Kind: firstNonEmpty(n.Kind, "signal"), Bus: n.Bus, Voltage: n.Voltage, Notes: n.Notes, Source: firstNonEmpty(n.Source, library.SourceUser)}); err != nil {
			return fmt.Errorf("net %s: %w", n.Name, err)
		}
	}
	blocks := map[string]int64{}
	for _, b := range pf.Blocks {
		nb, err := s.Lib.SetBlock(ctx, library.Block{ProjectID: p.ID, Name: b.Name, Kind: b.Kind, Value: b.Value, Notes: b.Notes, Source: firstNonEmpty(b.Source, library.SourceUser)})
		if err != nil {
			return fmt.Errorf("block %s: %w", b.Name, err)
		}
		blocks[strings.ToUpper(b.Name)] = nb.ID
	}
	for _, c := range pf.Connections {
		lc := library.Connection{Net: c.Net, Ref: c.Ref, Pin: c.Pin, Notes: c.Notes, Source: firstNonEmpty(c.Source, library.SourceUser)}
		switch {
		case c.Entry != "":
			lc.EntryID = entries[c.Entry]
		case c.Block != "":
			lc.BlockID = blocks[strings.ToUpper(c.Block)]
		}
		if lc.EntryID == 0 && lc.BlockID == 0 {
			note("connection %s%s.%s on %s: no such part or block", c.Entry, c.Block, c.Pin, c.Net)
			continue
		}
		if _, err := s.Lib.Connect(ctx, p.ID, lc); err != nil {
			return fmt.Errorf("connection %s%s.%s: %w", c.Entry, c.Block, c.Pin, err)
		}
	}
	for _, g := range pf.Groups {
		if _, err := s.Lib.SetGroup(ctx, library.Group{ProjectID: p.ID, Name: g.Name, Description: g.Description, Notes: g.Notes, Below: g.Below, Source: firstNonEmpty(g.Source, library.SourceUser)}); err != nil {
			return fmt.Errorf("group %s: %w", g.Name, err)
		}
	}
	node := func(n string) string { return libraryNode(n, entries, blocks) }
	for _, a := range pf.Assignments {
		n := node(a.Node)
		if n == "" {
			note("assignment of %s: no such part or block", a.Node)
			continue
		}
		if err := s.Lib.Assign(ctx, p.ID, library.Assignment{Node: n, Group: a.Group, Supports: node(a.Supports), Cluster: a.Cluster, Column: a.Column, Row: a.Row, Source: firstNonEmpty(a.Source, library.SourceUser)}); err != nil {
			return fmt.Errorf("assignment of %s: %w", a.Node, err)
		}
	}
	for _, pos := range pf.Positions {
		if n := node(pos.Node); n != "" {
			if err := s.Lib.SetPosition(ctx, p.ID, library.Position{Node: n, X: pos.X, Y: pos.Y}); err != nil {
				return err
			}
		}
	}
	for _, c := range pf.Calculations {
		lc := library.Calculation{ProjectID: p.ID, Kind: c.Kind, Title: c.Title, Inputs: c.Inputs, Outputs: c.Outputs}
		if c.Part != "" {
			if part, ok := parts[strings.ToUpper(c.Part)]; ok {
				lc.PartID = part.ID
			} else if res, err := s.Lib.Resolve(ctx, c.Part); err == nil {
				lc.PartID = res.Part.ID
			}
		}
		if _, err := s.Lib.SaveCalculation(ctx, lc); err != nil {
			return fmt.Errorf("calculation %s: %w", c.Title, err)
		}
	}
	for _, n := range pf.Notes {
		if _, err := s.Lib.AddNote(ctx, library.Note{TargetKind: library.TargetProject, TargetID: p.Name, Body: n.Body, Author: firstNonEmpty(n.Author, library.SourceUser)}, 0); err != nil {
			return err
		}
	}
	if err := s.importModels(ctx, pf.Models, parts, note); err != nil {
		return err
	}
	return s.importTests(ctx, p.ID, pf.Tests, runFiles, out)
}

// importModels adds the parts' SPICE models the library doesn't have; a
// model it has in another version stays as the library has it.
func (s *Service) importModels(ctx context.Context, models []FileModel, parts map[string]library.Part, note func(string, ...any)) error {
	for _, m := range models {
		part, ok := parts[strings.ToUpper(m.Part)]
		if !ok {
			res, err := s.Lib.Resolve(ctx, m.Part)
			if err != nil {
				note("model %s: no part %s", m.Name, m.Part)
				continue
			}
			part = res.Part
		}
		have, err := s.Lib.SpiceModels(ctx, part.ID)
		if err != nil {
			return err
		}
		kept := false
		for _, h := range have {
			if strings.EqualFold(h.Name, m.Name) {
				kept = true
				if strings.TrimSpace(h.Text) != strings.TrimSpace(m.Text) {
					note("model %s of %s: the library's version was kept; the project's differs", m.Name, part.MPN)
				}
			}
		}
		if kept {
			continue
		}
		if _, err := s.Lib.SetSpiceModel(ctx, library.SpiceModel{PartID: part.ID, Name: m.Name, Dialect: m.Dialect, Text: m.Text, Notes: m.Notes, Source: firstNonEmpty(m.Source, library.SourceAgent)}); err != nil {
			return fmt.Errorf("model %s: %w", m.Name, err)
		}
	}
	return nil
}

// importTests makes the tests, their variations and their runs, with the
// runs' files in their new folders.
func (s *Service) importTests(ctx context.Context, projectID int64, tests []FileTest, runFiles map[string]*zip.File, out *ProjectImportResult) error {
	for _, t := range tests {
		sim, err := s.Lib.SaveSimulation(ctx, library.Simulation{ProjectID: projectID, Name: t.Name, Kind: t.Kind, Purpose: t.Purpose, Target: t.Target, Source: firstNonEmpty(t.Source, library.SourceAgent)})
		if err != nil {
			return fmt.Errorf("test %s: %w", t.Name, err)
		}
		out.Tests++
		for _, v := range t.Variations {
			lv, err := s.Lib.SaveVariation(ctx, library.Variation{SimulationID: sim.ID, Name: v.Name, Change: v.Change, Netlist: v.Netlist, Models: v.Models, Conditions: v.Conditions, Source: firstNonEmpty(v.Source, library.SourceAgent)})
			if err != nil {
				return fmt.Errorf("test %s, variation %s: %w", t.Name, v.Name, err)
			}
			for _, r := range v.Runs {
				run, err := s.Lib.AddRun(ctx, lv.ID, r.Condition)
				if err != nil {
					return err
				}
				run.State, run.Ngspice, run.DeckSHA, run.Started, run.Finished, run.TookMS = r.State, r.Ngspice, r.DeckSHA, r.Started, r.Finished, r.TookMS
				run.Error, run.Problems, run.Measures, run.Plots, run.Log = r.Error, r.Problems, r.Measures, r.Plots, r.Log
				if run.Problems == nil {
					run.Problems = []string{}
				}
				// A run that was under way when exported isn't any more.
				if run.State == "queued" || run.State == "running" {
					run.State, run.Error = "cancelled", "exported while it ran"
				}
				if err := s.Lib.SaveRun(ctx, run); err != nil {
					return err
				}
				if r.Files != "" {
					if !runFolderRe.MatchString(r.Files) {
						return fmt.Errorf("test %s: bad run folder %q", t.Name, r.Files)
					}
					if err := unpackRun(runFiles, r.Files, s.Lib.RunDir(run.ID)); err != nil {
						return fmt.Errorf("test %s, run files: %w", t.Name, err)
					}
				}
				out.Runs++
			}
		}
		if t.Presentation != nil {
			if err := s.Lib.SetPresentation(ctx, sim.ID, *t.Presentation); err != nil {
				return err
			}
		}
	}
	return nil
}

// unpackRun writes the files under prefix ("runs/3") into dir.
func unpackRun(files map[string]*zip.File, prefix, dir string) error {
	prefix = path.Clean(prefix) + "/"
	for name, f := range files {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		base := strings.TrimPrefix(name, prefix)
		if base == "" || strings.ContainsAny(base, `/\`) || base == "." || base == ".." {
			return fmt.Errorf("bad file name %q", name)
		}
		b, err := readZipFile(f, maxRunFile)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, base), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// libraryNode turns a file node ("part:U1", "part:R1,R2/R1",
// "block:USB-C") into the library's, or "" when it names nothing here.
func libraryNode(node string, entries, blocks map[string]int64) string {
	switch {
	case strings.HasPrefix(node, "part:"):
		rest := strings.TrimPrefix(node, "part:")
		// A key may hold "/" only as the designator separator; keys
		// themselves are designators or part numbers.
		key, ref := rest, ""
		if i := strings.LastIndex(rest, "/"); i >= 0 {
			if _, ok := entries[rest]; !ok {
				key, ref = rest[:i], rest[i+1:]
			}
		}
		id, ok := entries[key]
		if !ok {
			return ""
		}
		return library.RefNode(id, ref)
	case strings.HasPrefix(node, "block:"):
		id, ok := blocks[strings.ToUpper(strings.TrimPrefix(node, "block:"))]
		if !ok {
			return ""
		}
		return library.BlockNode(id)
	}
	return ""
}
