package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// A project file (.sdproj) carries a project out of one library and into
// another: a zip holding project.json, with everything the project holds
// and what it needs from the library (which parts, and where their
// datasheets come from), and each simulation run's folder under runs/.
// No library IDs leave the library: BOM entries are referred to by key
// (their designators, or the part number), blocks, nets, groups and
// tests by name.

// Project file format identifiers.
const (
	ProjectFileFormat  = "sheetdiver-project"
	ProjectFileVersion = 1
	// ProjectFileExt is the extension project files are saved with.
	ProjectFileExt = ".sdproj"
	// projectJSON is the project's description inside the zip.
	projectJSON = "project.json"
)

// ProjectFile is project.json.
type ProjectFile struct {
	Format   string `json:"format"`
	Version  int    `json:"version"`
	App      string `json:"app,omitempty"`
	Exported string `json:"exported"`

	Name        string `json:"name"`
	Description string `json:"description,omitempty"`

	// Parts are the library parts the BOM uses, with their datasheets:
	// what an import checks for and fetches.
	Parts        []FilePart         `json:"parts"`
	Rails        []FileRail         `json:"rails"`
	Entries      []FileEntry        `json:"entries"`
	Nets         []FileNet          `json:"nets"`
	Blocks       []FileBlock        `json:"blocks"`
	Connections  []FileConnection   `json:"connections"`
	Groups       []FileGroup        `json:"groups"`
	Assignments  []FileAssignment   `json:"assignments"`
	Positions    []library.Position `json:"positions"`
	Calculations []FileCalculation  `json:"calculations"`
	Notes        []FileNote         `json:"notes"`
	Models       []FileModel        `json:"models"`
	Tests        []FileTest         `json:"tests"`
}

// FilePart is a part the project uses and where its datasheet comes from.
type FilePart struct {
	MPN          string         `json:"mpn"`
	Manufacturer string         `json:"manufacturer,omitempty"`
	Description  string         `json:"description,omitempty"`
	Category     string         `json:"category,omitempty"`
	Package      string         `json:"package,omitempty"`
	Datasheet    *FileDatasheet `json:"datasheet,omitempty"`
}

// FileDatasheet identifies a part's datasheet: the file by its hash, and
// where it was downloaded from.
type FileDatasheet struct {
	SHA256    string `json:"sha256"`
	Filename  string `json:"filename,omitempty"`
	Title     string `json:"title,omitempty"`
	Revision  string `json:"revision,omitempty"`
	SourceURL string `json:"sourceUrl,omitempty"`
}

// FileRail is a supply rail.
type FileRail struct {
	Name         string   `json:"name"`
	VNom         *float64 `json:"vNom,omitempty"`
	VMin         *float64 `json:"vMin,omitempty"`
	VMax         *float64 `json:"vMax,omitempty"`
	Source       string   `json:"source,omitempty"`
	CurrentLimit *float64 `json:"currentLimit,omitempty"`
	OtherTyp     *float64 `json:"otherTyp,omitempty"`
	OtherMax     *float64 `json:"otherMax,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

// FileEntry is a BOM entry. Key names it everywhere else in the file.
type FileEntry struct {
	Key        string   `json:"key"`
	Part       string   `json:"part"`
	Designator string   `json:"designator,omitempty"`
	Qty        int      `json:"qty"`
	Role       string   `json:"role,omitempty"`
	SupplyRail string   `json:"supplyRail,omitempty"`
	OutputRail string   `json:"outputRail,omitempty"`
	ITyp       *float64 `json:"iTyp,omitempty"`
	IMax       *float64 `json:"iMax,omitempty"`
	Variant    string   `json:"variant,omitempty"`
	Notes      string   `json:"notes,omitempty"`
}

// FileNet is a net.
type FileNet struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind,omitempty"`
	Bus     string   `json:"bus,omitempty"`
	Voltage *float64 `json:"voltage,omitempty"`
	Notes   string   `json:"notes,omitempty"`
	Source  string   `json:"source,omitempty"`
}

// FileBlock is something off the BOM on the block diagram.
type FileBlock struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	Value  string `json:"value,omitempty"`
	Notes  string `json:"notes,omitempty"`
	Source string `json:"source,omitempty"`
}

// FileConnection is a pin on a net: an entry's (with the designator, for
// an entry with several) or a block's.
type FileConnection struct {
	Net    string `json:"net"`
	Entry  string `json:"entry,omitempty"`
	Block  string `json:"block,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Pin    string `json:"pin,omitempty"`
	Notes  string `json:"notes,omitempty"`
	Source string `json:"source,omitempty"`
}

// FileGroup is a functional group.
type FileGroup struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Below       string `json:"below,omitempty"`
	Source      string `json:"source,omitempty"`
}

// FileAssignment is where a node belongs. Node and Supports are file
// nodes: "part:<entry key>", "part:<entry key>/<designator>" or
// "block:<block name>".
type FileAssignment struct {
	Node     string `json:"node"`
	Group    string `json:"group,omitempty"`
	Supports string `json:"supports,omitempty"`
	Cluster  string `json:"cluster,omitempty"`
	Column   *int   `json:"column,omitempty"`
	Row      *int   `json:"row,omitempty"`
	Source   string `json:"source,omitempty"`
}

// FileCalculation is a saved calculator run.
type FileCalculation struct {
	Kind    string          `json:"kind"`
	Title   string          `json:"title,omitempty"`
	Part    string          `json:"part,omitempty"`
	Inputs  json.RawMessage `json:"inputs"`
	Outputs json.RawMessage `json:"outputs"`
	Created string          `json:"created,omitempty"`
}

// FileNote is a note on the project.
type FileNote struct {
	Body    string `json:"body"`
	Author  string `json:"author,omitempty"`
	Created string `json:"created,omitempty"`
}

// FileModel is a SPICE model of one of the project's parts.
type FileModel struct {
	Part    string `json:"part"`
	Name    string `json:"name"`
	Dialect string `json:"dialect,omitempty"`
	Text    string `json:"text"`
	Notes   string `json:"notes,omitempty"`
	Source  string `json:"source,omitempty"`
}

// FileTest is a simulation with its variations and runs.
type FileTest struct {
	Name         string                `json:"name"`
	Kind         string                `json:"kind,omitempty"`
	Purpose      string                `json:"purpose,omitempty"`
	Target       string                `json:"target,omitempty"`
	Presentation *library.Presentation `json:"presentation,omitempty"`
	Source       string                `json:"source,omitempty"`
	Created      string                `json:"created,omitempty"`
	Updated      string                `json:"updated,omitempty"`
	Variations   []FileVariation       `json:"variations"`
}

// FileVariation is a try of a test, with its runs.
type FileVariation struct {
	Name       string                 `json:"name"`
	Change     string                 `json:"change,omitempty"`
	Netlist    string                 `json:"netlist"`
	Models     []library.SimModel     `json:"models"`
	Conditions []library.SimCondition `json:"conditions"`
	Source     string                 `json:"source,omitempty"`
	Created    string                 `json:"created,omitempty"`
	Updated    string                 `json:"updated,omitempty"`
	Runs       []FileRun              `json:"runs"`
}

// FileRun is a run, oldest first. Files is its folder in the zip
// ("runs/3"), when it had one.
type FileRun struct {
	Condition string               `json:"condition"`
	State     string               `json:"state"`
	Ngspice   string               `json:"ngspice,omitempty"`
	DeckSHA   string               `json:"deckSha,omitempty"`
	Started   string               `json:"started,omitempty"`
	Finished  string               `json:"finished,omitempty"`
	TookMS    int64                `json:"tookMs,omitempty"`
	Error     string               `json:"error,omitempty"`
	Problems  []string             `json:"problems,omitempty"`
	Measures  []library.RunMeasure `json:"measures,omitempty"`
	Plots     []library.RunPlot    `json:"plots,omitempty"`
	Log       string               `json:"log,omitempty"`
	Files     string               `json:"files,omitempty"`
}

// ExportedProject is an exported project: the file's name and bytes.
type ExportedProject struct {
	Filename string `json:"filename"`
	Data     []byte `json:"data"`
}

// ExportProject writes a project to a project file.
func (s *Service) ExportProject(ctx context.Context, ref string) (ExportedProject, error) {
	pf, runDirs, err := s.projectFile(ctx, ref)
	if err != nil {
		return ExportedProject{}, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	js, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return ExportedProject{}, err
	}
	w, err := zw.Create(projectJSON)
	if err != nil {
		return ExportedProject{}, err
	}
	if _, err := w.Write(append(js, '\n')); err != nil {
		return ExportedProject{}, err
	}
	for _, rd := range runDirs {
		if err := zipDir(zw, rd.dir, rd.files); err != nil {
			return ExportedProject{}, fmt.Errorf("run files: %w", err)
		}
	}
	if err := zw.Close(); err != nil {
		return ExportedProject{}, err
	}
	return ExportedProject{Filename: fileSafe(pf.Name) + ProjectFileExt, Data: buf.Bytes()}, nil
}

// ExportProjectTo writes a project file to a path: a folder takes the
// project's file name, a file must be a .sdproj that doesn't exist yet
// (nothing is overwritten). It returns where it was written.
func (s *Service) ExportProjectTo(ctx context.Context, ref, dest string) (string, int, error) {
	res, err := s.ExportProject(ctx, ref)
	if err != nil {
		return "", 0, err
	}
	dest = expandHome(strings.TrimSpace(dest))
	if dest == "" {
		return "", 0, errors.New("give a path to write the project file to")
	}
	if st, err := os.Stat(dest); err == nil && st.IsDir() {
		dest = filepath.Join(dest, res.Filename)
	}
	if !strings.EqualFold(filepath.Ext(dest), ProjectFileExt) {
		return "", 0, fmt.Errorf("a project file's name ends in %s", ProjectFileExt)
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) //nolint:gosec // a file to hand on, readable like the app's other exports
	if errors.Is(err, fs.ErrExist) {
		return "", 0, fmt.Errorf("%s exists: give another name (nothing is overwritten)", dest)
	}
	if err != nil {
		return "", 0, err
	}
	if _, err := f.Write(res.Data); err != nil {
		_ = f.Close()
		_ = os.Remove(dest)
		return "", 0, err
	}
	return dest, len(res.Data), f.Close()
}

// runDir is a run's folder on disk and where it goes in the zip.
type runDir struct {
	dir, files string
}

// projectFile describes a project as project.json does, with the run
// folders to put beside it.
func (s *Service) projectFile(ctx context.Context, ref string) (ProjectFile, []runDir, error) {
	p, err := s.Lib.Project(ctx, ref)
	if err != nil {
		return ProjectFile{}, nil, err
	}
	// Everything the project holds, read first.
	entries, err1 := s.Lib.ProjectParts(ctx, p.ID)
	rails, err2 := s.Lib.Rails(ctx, p.ID)
	nets, err3 := s.Lib.Nets(ctx, p.ID)
	blocks, err4 := s.Lib.Blocks(ctx, p.ID)
	conns, err5 := s.Lib.Connections(ctx, p.ID)
	groups, err6 := s.Lib.Groups(ctx, p.ID)
	assigns, err7 := s.Lib.Assignments(ctx, p.ID)
	layout, err8 := s.Lib.Layout(ctx, p.ID)
	calcs, err9 := s.Lib.Calculations(ctx, p.ID)
	notes, err10 := s.Lib.Notes(ctx, library.TargetProject, p.Name)
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8, err9, err10); err != nil {
		return ProjectFile{}, nil, err
	}
	// Entries and blocks by key and name, never by library ID.
	keys := entryKeys(entries)
	blockNames := map[int64]string{}
	for _, b := range blocks {
		blockNames[b.ID] = b.Name
	}
	node := func(n string) string { return fileNode(n, keys, blockNames) }

	pf := ProjectFile{
		Format: ProjectFileFormat, Version: ProjectFileVersion, App: s.Version,
		Exported: time.Now().UTC().Format(time.RFC3339),
		Name:     p.Name, Description: p.Description,
		Parts: []FilePart{}, Models: []FileModel{},
		Rails: mapList(rails, func(r library.Rail) FileRail {
			return FileRail{Name: r.Name, VNom: r.VNom, VMin: r.VMin, VMax: r.VMax, Source: r.Source, CurrentLimit: r.CurrentLimit, OtherTyp: r.OtherTyp, OtherMax: r.OtherMax, Notes: r.Notes}
		}),
		Entries: mapList(entries, func(e library.ProjectPart) FileEntry {
			return FileEntry{Key: keys[e.ID], Part: e.MPN, Designator: e.Designator, Qty: e.Qty, Role: e.Role, SupplyRail: e.SupplyRail, OutputRail: e.OutputRail, ITyp: e.ITyp, IMax: e.IMax, Variant: e.Variant, Notes: e.Notes}
		}),
		Nets: mapList(nets, func(n library.Net) FileNet {
			return FileNet{Name: n.Name, Kind: n.Kind, Bus: n.Bus, Voltage: n.Voltage, Notes: n.Notes, Source: n.Source}
		}),
		Blocks: mapList(blocks, func(b library.Block) FileBlock {
			return FileBlock{Name: b.Name, Kind: b.Kind, Value: b.Value, Notes: b.Notes, Source: b.Source}
		}),
		Connections: mapList(conns, func(c library.Connection) FileConnection {
			return FileConnection{Net: c.Net, Entry: keys[c.EntryID], Block: blockNames[c.BlockID], Ref: c.Ref, Pin: c.Pin, Notes: c.Notes, Source: c.Source}
		}),
		Groups: mapList(groups, func(g library.Group) FileGroup {
			return FileGroup{Name: g.Name, Description: g.Description, Notes: g.Notes, Below: g.Below, Source: g.Source}
		}),
		// A node naming something no longer there is left out.
		Assignments: mapList(assigns, func(a library.Assignment) FileAssignment {
			return FileAssignment{Node: node(a.Node), Group: a.Group, Supports: node(a.Supports), Cluster: a.Cluster, Column: a.Column, Row: a.Row, Source: a.Source}
		}, func(a FileAssignment) bool { return a.Node != "" }),
		Positions: mapList(layout, func(pos library.Position) library.Position {
			return library.Position{Node: node(pos.Node), X: pos.X, Y: pos.Y}
		}, func(pos library.Position) bool { return pos.Node != "" }),
		Calculations: mapList(calcs, func(c library.Calculation) FileCalculation {
			return FileCalculation{Kind: c.Kind, Title: c.Title, Part: s.partMPN(ctx, c.PartID), Inputs: c.Inputs, Outputs: c.Outputs, Created: stamp(c.CreatedAt)}
		}),
		Notes: mapList(notes, func(n library.Note) FileNote {
			return FileNote{Body: n.Body, Author: n.Author, Created: stamp(n.CreatedAt)}
		}),
	}
	// The library lists these in the order they were made; the file keeps
	// a stable order, so exports diff well.
	sort.SliceStable(pf.Connections, func(i, j int) bool { return connKey(pf.Connections[i]) < connKey(pf.Connections[j]) })
	sort.SliceStable(pf.Assignments, func(i, j int) bool { return pf.Assignments[i].Node < pf.Assignments[j].Node })

	tests, dirs, err := s.fileTests(ctx, p.ID)
	if err != nil {
		return ProjectFile{}, nil, err
	}
	pf.Tests = tests
	// The parts: the BOM's, then those whose models the tests include
	// though they aren't on the BOM (a MOSFET on a test bench).
	seen := map[int64]bool{}
	for _, e := range entries {
		if err := s.addFilePart(ctx, &pf, seen, e.PartID); err != nil {
			return ProjectFile{}, nil, err
		}
	}
	for _, ref := range modelParts(tests) {
		if res, err := s.Lib.Resolve(ctx, ref); err == nil {
			if err := s.addFilePart(ctx, &pf, seen, res.Part.ID); err != nil {
				return ProjectFile{}, nil, err
			}
		}
	}
	return pf, dirs, nil
}

// mapList converts a list, keeping only what every keep accepts. The
// result is never nil, so an empty list is [] in the JSON.
func mapList[T, U any](in []T, f func(T) U, keep ...func(U) bool) []U {
	out := make([]U, 0, len(in))
next:
	for _, v := range in {
		u := f(v)
		for _, k := range keep {
			if !k(u) {
				continue next
			}
		}
		out = append(out, u)
	}
	return out
}

// connKey orders connections: by net, then entry, block, designator, pin.
func connKey(c FileConnection) string {
	return strings.Join([]string{strings.ToUpper(c.Net), c.Entry, c.Block, c.Ref, c.Pin}, "\x00")
}

// partMPN is a part's number, or "" for none.
func (s *Service) partMPN(ctx context.Context, id int64) string {
	if id == 0 {
		return ""
	}
	part, err := s.Lib.PartByID(ctx, id)
	if err != nil {
		return ""
	}
	return part.MPN
}

// modelParts lists the parts whose models the tests include.
func modelParts(tests []FileTest) []string {
	var out []string
	for _, t := range tests {
		for _, v := range t.Variations {
			for _, m := range v.Models {
				out = append(out, m.Part)
			}
		}
	}
	return out
}

// addFilePart adds a library part to the file once, with its datasheet
// and its SPICE models.
func (s *Service) addFilePart(ctx context.Context, pf *ProjectFile, seen map[int64]bool, id int64) error {
	if seen[id] {
		return nil
	}
	seen[id] = true
	part, err := s.Lib.PartByID(ctx, id)
	if err != nil {
		return err
	}
	models, err := s.Lib.SpiceModels(ctx, part.ID)
	if err != nil {
		return err
	}
	fp := FilePart{MPN: part.MPN, Manufacturer: part.Manufacturer, Description: part.Description, Category: part.Category, Package: part.Package}
	if part.DatasheetID != 0 {
		if d, err := s.Lib.Datasheet(ctx, part.DatasheetID); err == nil {
			fp.Datasheet = &FileDatasheet{SHA256: d.SHA256, Filename: d.Filename, Title: d.Title, Revision: d.Meta["revision"], SourceURL: d.SourceURL}
		}
	}
	pf.Parts = append(pf.Parts, fp)
	pf.Models = append(pf.Models, mapList(models, func(m library.SpiceModel) FileModel {
		return FileModel{Part: part.MPN, Name: m.Name, Dialect: m.Dialect, Text: m.Text, Notes: m.Notes, Source: m.Source}
	})...)
	return nil
}

// fileTests describes a project's tests, by name, with their variations
// and runs, and lists the runs' folders.
func (s *Service) fileTests(ctx context.Context, projectID int64) ([]FileTest, []runDir, error) {
	sims, err := s.Lib.Simulations(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	// By name: the library lists them by when they last changed, which an
	// import resets.
	sort.SliceStable(sims, func(i, j int) bool { return strings.ToLower(sims[i].Name) < strings.ToLower(sims[j].Name) })
	tests := make([]FileTest, 0, len(sims))
	var dirs []runDir
	for _, sim := range sims {
		vars, err1 := s.Lib.Variations(ctx, sim.ID)
		runs, err2 := s.Lib.Runs(ctx, sim.ID)
		if err := errors.Join(err1, err2); err != nil {
			return nil, nil, err
		}
		// Runs come newest first; the file keeps them in the order run.
		sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
		byVariation := map[int64][]FileRun{}
		for _, r := range runs {
			fr := FileRun{Condition: r.Condition, State: r.State, Ngspice: r.Ngspice, DeckSHA: r.DeckSHA, Started: r.Started, Finished: r.Finished, TookMS: r.TookMS, Error: r.Error, Problems: r.Problems, Measures: r.Measures, Plots: r.Plots, Log: r.Log}
			if dir := s.Lib.RunDir(r.ID); hasFiles(dir) {
				fr.Files = "runs/" + strconv.Itoa(len(dirs)+1)
				dirs = append(dirs, runDir{dir: dir, files: fr.Files})
			}
			byVariation[r.VariationID] = append(byVariation[r.VariationID], fr)
		}
		tests = append(tests, FileTest{
			Name: sim.Name, Kind: sim.Kind, Purpose: sim.Purpose, Target: sim.Target, Presentation: sim.Presentation, Source: sim.Source, Created: sim.Created, Updated: sim.Updated,
			Variations: mapList(vars, func(v library.Variation) FileVariation {
				return FileVariation{Name: v.Name, Change: v.Change, Netlist: v.Netlist, Models: v.Models, Conditions: v.Conditions, Source: v.Source, Created: v.Created, Updated: v.Updated,
					Runs: append([]FileRun{}, byVariation[v.ID]...)}
			}),
		})
	}
	return tests, dirs, nil
}

// entryKeys names each BOM entry for the file: its designators when no
// other entry has the same, else its part number, numbered when that
// repeats too ("TPS63900#2").
func entryKeys(entries []library.ProjectPart) map[int64]string {
	count := map[string]int{}
	for _, e := range entries {
		count[strings.ToUpper(e.Designator)]++
		count["mpn:"+strings.ToUpper(e.MPN)]++
	}
	keys := map[int64]string{}
	used := map[string]bool{}
	for _, e := range entries {
		k := e.Designator
		if k == "" || count[strings.ToUpper(k)] > 1 {
			k = e.MPN
			if count["mpn:"+strings.ToUpper(k)] > 1 {
				k += "#1"
			}
		}
		for n := 2; used[strings.ToUpper(k)]; n++ {
			k = e.MPN + "#" + strconv.Itoa(n)
		}
		used[strings.ToUpper(k)] = true
		keys[e.ID] = k
	}
	return keys
}

// fileNode turns a library node ("part:3", "part:3/R1", "block:2") into
// the file's ("part:R1,R2", "part:R1,R2/R1", "block:USB-C"), or "" when
// it names nothing that's left.
func fileNode(node string, keys map[int64]string, blocks map[int64]string) string {
	switch {
	case node == "":
		return ""
	case strings.HasPrefix(node, "part:"):
		rest := strings.TrimPrefix(node, "part:")
		id, ref, _ := strings.Cut(rest, "/")
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil || keys[n] == "" {
			return ""
		}
		if ref != "" {
			return "part:" + keys[n] + "/" + ref
		}
		return "part:" + keys[n]
	case strings.HasPrefix(node, "block:"):
		n, err := strconv.ParseInt(strings.TrimPrefix(node, "block:"), 10, 64)
		if err != nil || blocks[n] == "" {
			return ""
		}
		return "block:" + blocks[n]
	}
	return ""
}

// stamp writes a time as RFC 3339, or "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// hasFiles reports whether a folder exists and holds anything.
func hasFiles(dir string) bool {
	ents, err := os.ReadDir(dir)
	return err == nil && len(ents) > 0
}

// zipDir adds a folder's files to a zip under prefix: the files at its
// top (a run's deck, log and results), not what's in folders below it.
func zipDir(zw *zip.Writer, dir, prefix string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if !e.Type().IsRegular() {
			continue // folders, links and devices stay behind
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		w, err := zw.Create(path.Join(prefix, e.Name()))
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

// fileSafe makes a project name usable as a file name.
func fileSafe(name string) string {
	out := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	if out == "" {
		return "project"
	}
	return out
}
