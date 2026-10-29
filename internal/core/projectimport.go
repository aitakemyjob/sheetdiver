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
	"slices"
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
	im := &projectImport{s: s, ctx: ctx, p: p, parts: parts, runFiles: runFiles, out: &out, entries: map[string]int64{}, blocks: map[string]int64{}}
	if err := im.build(pf); err != nil {
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
// each of the file's part numbers (in capitals). A project names exact
// parts, so only an exact match counts (the part number or one of its
// orderable codes): a family or prefix match could be another value of a
// capacitor series.
func (s *Service) importParts(ctx context.Context, fps []FilePart, fetch bool, out *ProjectImportResult) (map[string]library.Part, error) {
	// What the library has already.
	had := make([]bool, len(fps))
	for i, fp := range fps {
		ok, err := s.partFromLibrary(ctx, fp)
		if err != nil {
			return nil, fmt.Errorf("adding %s: %w", fp.MPN, err)
		}
		had[i] = ok
	}
	// The rest downloaded, four at a time.
	fetchErr := make([]error, len(fps))
	if fetch {
		parallel(len(fps), 4, func(i int) {
			if !had[i] {
				fetchErr[i] = s.fetchPart(ctx, fps[i])
			}
		})
	}
	// What became of each.
	parts := map[string]library.Part{}
	for i, fp := range fps {
		row, part, err := s.partOutcome(ctx, fp, had[i], fetch, fetchErr[i])
		if err != nil {
			return nil, err
		}
		parts[strings.ToUpper(fp.MPN)] = part
		out.Parts = append(out.Parts, row)
		out.Counts[row.Status]++
	}
	return parts, nil
}

// partFromLibrary reports whether the library has a part: by its exact
// number, or by its datasheet under other part numbers (a series'
// datasheet), which the part then joins with nothing to download.
func (s *Service) partFromLibrary(ctx context.Context, fp FilePart) (bool, error) {
	if _, ok := s.exactPart(ctx, fp.MPN); ok {
		return true, nil
	}
	if fp.Datasheet == nil || fp.Datasheet.SHA256 == "" {
		return false, nil
	}
	d, ok, err := s.Lib.DatasheetBySHA(ctx, fp.Datasheet.SHA256)
	if err != nil || !ok {
		return false, err
	}
	return true, s.attachPart(ctx, fp, d)
}

// partOutcome says what became of a part, adding it without a datasheet
// when it couldn't be had, so the BOM stays complete.
func (s *Service) partOutcome(ctx context.Context, fp FilePart, had, fetch bool, fetchErr error) (ProjectImportPart, library.Part, error) {
	row := ProjectImportPart{MPN: fp.MPN}
	res, ok := s.exactPart(ctx, fp.MPN)
	switch {
	case !ok:
		p, err := s.Lib.AddPart(ctx, library.Part{MPN: fp.MPN, Manufacturer: fp.Manufacturer, Description: fp.Description, Category: fp.Category, Package: fp.Package})
		if err != nil {
			return row, p, fmt.Errorf("adding %s: %w", fp.MPN, err)
		}
		res.Part, row.Status = p, PartNoDatasheet
		switch {
		case fetchErr != nil:
			row.Note = fetchErr.Error()
		case !fetch:
			row.Note = "not in the library (fetching was off)"
		}
	case res.Part.DatasheetID == 0:
		row.Status = PartNoDatasheet
	case !had:
		row.Status = PartImported
	default:
		row.Status, row.Note = s.datasheetVersion(ctx, fp, res.Part)
	}
	if !strings.EqualFold(res.Part.MPN, fp.MPN) {
		row.Part = res.Part.MPN
	}
	return row, res.Part, nil
}

// datasheetVersion compares the library's datasheet of a part with the
// one the project was made with.
func (s *Service) datasheetVersion(ctx context.Context, fp FilePart, part library.Part) (string, string) {
	if fp.Datasheet == nil {
		return PartInLibrary, ""
	}
	d, err := s.Lib.Datasheet(ctx, part.DatasheetID)
	if err != nil || d.SHA256 == fp.Datasheet.SHA256 {
		return PartInLibrary, ""
	}
	return PartOtherVersion, "the library has " + firstNonEmpty(d.Meta["revision"], "another revision") + "; the project was made with " + firstNonEmpty(fp.Datasheet.Revision, fp.Datasheet.Filename, "another one")
}

// parallel runs f(0)…f(n-1), at most limit at a time.
func parallel(n, limit int, f func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, limit)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			f(i)
		}()
	}
	wg.Wait()
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

// projectImport makes the things in a project file in a new project,
// a step per kind, keeping what later steps refer to.
type projectImport struct {
	s        *Service
	ctx      context.Context
	p        library.Project
	parts    map[string]library.Part // the file's part numbers, in capitals
	entries  map[string]int64        // BOM entries by key
	blocks   map[string]int64        // blocks by name, in capitals
	runFiles map[string]*zip.File
	out      *ProjectImportResult
}

// build runs the steps in order: what a step refers to is made before it.
func (im *projectImport) build(pf ProjectFile) error {
	for _, step := range []func(ProjectFile) error{im.rails, im.bom, im.nets, im.blockList, im.connections, im.groups, im.diagram, im.calculations, im.notes, im.models, im.tests} {
		if err := step(pf); err != nil {
			return err
		}
	}
	return nil
}

// note records what didn't come across as it was.
func (im *projectImport) note(format string, args ...any) {
	im.out.Notes = append(im.out.Notes, fmt.Sprintf(format, args...))
}

// part is the library part for one of the file's part numbers: as the
// parts step found it, else as the library resolves it.
func (im *projectImport) part(mpn string) (library.Part, bool) {
	if p, ok := im.parts[strings.ToUpper(mpn)]; ok {
		return p, true
	}
	res, err := im.s.Lib.Resolve(im.ctx, mpn)
	return res.Part, err == nil
}

// node is the library's node for a file node, or "".
func (im *projectImport) node(n string) string { return libraryNode(n, im.entries, im.blocks) }

// rails makes the supply rails.
func (im *projectImport) rails(pf ProjectFile) error {
	for _, r := range pf.Rails {
		if _, err := im.s.Lib.SetRail(im.ctx, library.Rail{ProjectID: im.p.ID, Name: r.Name, VNom: r.VNom, VMin: r.VMin, VMax: r.VMax, Source: r.Source, CurrentLimit: r.CurrentLimit, OtherTyp: r.OtherTyp, OtherMax: r.OtherMax, Notes: r.Notes}); err != nil {
			return fmt.Errorf("rail %s: %w", r.Name, err)
		}
	}
	return nil
}

// bom makes the BOM entries, keeping each one's ID by its key.
func (im *projectImport) bom(pf ProjectFile) error {
	for _, e := range pf.Entries {
		part, ok := im.part(e.Part)
		if !ok {
			im.note("BOM entry %s: no part %s", e.Key, e.Part)
			continue
		}
		pp, err := im.s.Lib.AddProjectPart(im.ctx, library.ProjectPart{ProjectID: im.p.ID, PartID: part.ID, Designator: e.Designator, Qty: e.Qty, Role: e.Role, SupplyRail: e.SupplyRail, OutputRail: e.OutputRail, ITyp: e.ITyp, IMax: e.IMax, Variant: e.Variant, Notes: e.Notes})
		if err != nil {
			return fmt.Errorf("BOM entry %s: %w", e.Key, err)
		}
		im.entries[e.Key] = pp.ID
	}
	return nil
}

// nets makes the nets.
func (im *projectImport) nets(pf ProjectFile) error {
	for _, n := range pf.Nets {
		if _, err := im.s.Lib.SetNet(im.ctx, library.Net{ProjectID: im.p.ID, Name: n.Name, Kind: n.Kind, Bus: n.Bus, Voltage: n.Voltage, Notes: n.Notes, Source: n.Source}); err != nil {
			return fmt.Errorf("net %s: %w", n.Name, err)
		}
	}
	return nil
}

// blockList makes the blocks, keeping each one's ID by its name.
func (im *projectImport) blockList(pf ProjectFile) error {
	for _, b := range pf.Blocks {
		nb, err := im.s.Lib.SetBlock(im.ctx, library.Block{ProjectID: im.p.ID, Name: b.Name, Kind: b.Kind, Value: b.Value, Notes: b.Notes, Source: b.Source})
		if err != nil {
			return fmt.Errorf("block %s: %w", b.Name, err)
		}
		im.blocks[strings.ToUpper(b.Name)] = nb.ID
	}
	return nil
}

// connections puts the pins of entries and blocks on their nets.
func (im *projectImport) connections(pf ProjectFile) error {
	for _, c := range pf.Connections {
		lc := library.Connection{Net: c.Net, EntryID: im.entries[c.Entry], BlockID: im.blocks[strings.ToUpper(c.Block)], Ref: c.Ref, Pin: c.Pin, Notes: c.Notes, Source: c.Source}
		if lc.EntryID == 0 && lc.BlockID == 0 {
			im.note("connection %s%s.%s on %s: no such part or block", c.Entry, c.Block, c.Pin, c.Net)
			continue
		}
		if _, err := im.s.Lib.Connect(im.ctx, im.p.ID, lc); err != nil {
			return fmt.Errorf("connection %s%s.%s: %w", c.Entry, c.Block, c.Pin, err)
		}
	}
	return nil
}

// groups makes the functional groups.
func (im *projectImport) groups(pf ProjectFile) error {
	for _, g := range pf.Groups {
		if _, err := im.s.Lib.SetGroup(im.ctx, library.Group{ProjectID: im.p.ID, Name: g.Name, Description: g.Description, Notes: g.Notes, Below: g.Below, Source: g.Source}); err != nil {
			return fmt.Errorf("group %s: %w", g.Name, err)
		}
	}
	return nil
}

// diagram places the parts and blocks: their groups and what they
// support, and where they were dragged.
func (im *projectImport) diagram(pf ProjectFile) error {
	for _, a := range pf.Assignments {
		n := im.node(a.Node)
		if n == "" {
			im.note("assignment of %s: no such part or block", a.Node)
			continue
		}
		if err := im.s.Lib.Assign(im.ctx, im.p.ID, library.Assignment{Node: n, Group: a.Group, Supports: im.node(a.Supports), Cluster: a.Cluster, Column: a.Column, Row: a.Row, Source: a.Source}); err != nil {
			return fmt.Errorf("assignment of %s: %w", a.Node, err)
		}
	}
	for _, pos := range pf.Positions {
		if n := im.node(pos.Node); n != "" {
			if err := im.s.Lib.SetPosition(im.ctx, im.p.ID, library.Position{Node: n, X: pos.X, Y: pos.Y}); err != nil {
				return err
			}
		}
	}
	return nil
}

// calculations saves the calculator runs, with their parts.
func (im *projectImport) calculations(pf ProjectFile) error {
	for _, c := range pf.Calculations {
		lc := library.Calculation{ProjectID: im.p.ID, Kind: c.Kind, Title: c.Title, Inputs: c.Inputs, Outputs: c.Outputs}
		if c.Part != "" {
			if part, ok := im.part(c.Part); ok {
				lc.PartID = part.ID
			}
		}
		if _, err := im.s.Lib.SaveCalculation(im.ctx, lc); err != nil {
			return fmt.Errorf("calculation %s: %w", c.Title, err)
		}
	}
	return nil
}

// notes adds the project's notes.
func (im *projectImport) notes(pf ProjectFile) error {
	for _, n := range pf.Notes {
		if _, err := im.s.Lib.AddNote(im.ctx, library.Note{TargetKind: library.TargetProject, TargetID: im.p.Name, Body: n.Body, Author: n.Author}, 0); err != nil {
			return err
		}
	}
	return nil
}

// models adds the parts' SPICE models the library doesn't have; a model
// it has in another version stays as the library has it.
func (im *projectImport) models(pf ProjectFile) error {
	for _, m := range pf.Models {
		part, ok := im.part(m.Part)
		if !ok {
			im.note("model %s: no part %s", m.Name, m.Part)
			continue
		}
		have, err := im.s.Lib.SpiceModels(im.ctx, part.ID)
		if err != nil {
			return err
		}
		if i := slices.IndexFunc(have, func(h library.SpiceModel) bool { return strings.EqualFold(h.Name, m.Name) }); i >= 0 {
			if strings.TrimSpace(have[i].Text) != strings.TrimSpace(m.Text) {
				im.note("model %s of %s: the library's version was kept; the project's differs", m.Name, part.MPN)
			}
			continue
		}
		if _, err := im.s.Lib.SetSpiceModel(im.ctx, library.SpiceModel{PartID: part.ID, Name: m.Name, Dialect: m.Dialect, Text: m.Text, Notes: m.Notes, Source: m.Source}); err != nil {
			return fmt.Errorf("model %s: %w", m.Name, err)
		}
	}
	return nil
}

// tests makes the tests, their variations and their runs.
func (im *projectImport) tests(pf ProjectFile) error {
	for _, t := range pf.Tests {
		sim, err := im.s.Lib.SaveSimulation(im.ctx, library.Simulation{ProjectID: im.p.ID, Name: t.Name, Kind: t.Kind, Purpose: t.Purpose, Target: t.Target, Source: t.Source})
		if err != nil {
			return fmt.Errorf("test %s: %w", t.Name, err)
		}
		im.out.Tests++
		for _, v := range t.Variations {
			lv, err := im.s.Lib.SaveVariation(im.ctx, library.Variation{SimulationID: sim.ID, Name: v.Name, Change: v.Change, Netlist: v.Netlist, Models: v.Models, Conditions: v.Conditions, Source: v.Source})
			if err != nil {
				return fmt.Errorf("test %s, variation %s: %w", t.Name, v.Name, err)
			}
			for _, r := range v.Runs {
				if err := im.run(lv.ID, r); err != nil {
					return fmt.Errorf("test %s, variation %s: %w", t.Name, v.Name, err)
				}
			}
		}
		if t.Presentation != nil {
			if err := im.s.Lib.SetPresentation(im.ctx, sim.ID, *t.Presentation); err != nil {
				return err
			}
		}
	}
	return nil
}

// run makes a run as it was, with its files in its new folder. A run
// under way when exported comes in cancelled.
func (im *projectImport) run(variationID int64, r FileRun) error {
	if r.Files != "" && !runFolderRe.MatchString(r.Files) {
		return fmt.Errorf("bad run folder %q", r.Files)
	}
	run, err := im.s.Lib.AddRun(im.ctx, variationID, r.Condition)
	if err != nil {
		return err
	}
	run.State, run.Ngspice, run.DeckSHA, run.Started, run.Finished, run.TookMS = r.State, r.Ngspice, r.DeckSHA, r.Started, r.Finished, r.TookMS
	run.Error, run.Problems, run.Measures, run.Plots, run.Log = r.Error, r.Problems, r.Measures, r.Plots, r.Log
	if run.Problems == nil {
		run.Problems = []string{}
	}
	if run.State == "queued" || run.State == "running" {
		run.State, run.Error = "cancelled", "exported while it ran"
	}
	if err := im.s.Lib.SaveRun(im.ctx, run); err != nil {
		return err
	}
	if r.Files != "" {
		if err := unpackRun(im.runFiles, r.Files, im.s.Lib.RunDir(run.ID)); err != nil {
			return fmt.Errorf("run files: %w", err)
		}
	}
	im.out.Runs++
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
