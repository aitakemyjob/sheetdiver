package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// comparable strips what an export can't keep the same from one library
// to the next: when it was made, and when things were saved.
func comparable(pf ProjectFile) ProjectFile {
	pf.Exported, pf.App = "", ""
	for i := range pf.Calculations {
		pf.Calculations[i].Created = ""
	}
	for i := range pf.Notes {
		pf.Notes[i].Created = ""
	}
	for i := range pf.Tests {
		t := &pf.Tests[i]
		t.Created, t.Updated = "", ""
		if t.Presentation != nil {
			t.Presentation.Updated = ""
		}
		for j := range t.Variations {
			t.Variations[j].Created, t.Variations[j].Updated = "", ""
		}
	}
	return pf
}

// rezip rewrites a project file's project.json.
func rezip(t *testing.T, data []byte, edit func(*ProjectFile), extra map[string]string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		b, err := readZipFile(f, 1<<30)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == projectJSON && edit != nil {
			var pf ProjectFile
			if err := json.Unmarshal(b, &pf); err != nil {
				t.Fatal(err)
			}
			edit(&pf)
			b, _ = json.Marshal(pf)
		}
		w, _ := zw.Create(f.Name)
		w.Write(b)
	}
	for name, body := range extra {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func TestProjectRoundTrip(t *testing.T) {
	a := newService(t)
	exportFixture(t, a)
	ctx := context.Background()
	exp, err := a.ExportProject(ctx, "Driver board")
	if err != nil {
		t.Fatal(err)
	}
	// Another library, with SD1234 but not the test bench's SD9001, and
	// no fetching: SD9001 joins without a datasheet.
	b := newService(t)
	res, err := b.ImportProject(ctx, ProjectImportRequest{Data: exp.Data})
	if err != nil {
		t.Fatal(err)
	}
	if res.Project != "Driver board" || res.Tests != 1 || res.Runs != 1 {
		t.Errorf("result: %+v", res)
	}
	status := map[string]ProjectImportPart{}
	for _, p := range res.Parts {
		status[p.MPN] = p
	}
	if status["SD1234"].Status != PartInLibrary || status["SD9001"].Status != PartNoDatasheet || !strings.Contains(status["SD9001"].Note, "fetching was off") {
		t.Errorf("parts: %+v", res.Parts)
	}
	if len(res.Notes) != 0 {
		t.Errorf("notes: %q", res.Notes)
	}
	// Exported again from the new library, it's the same project: every
	// entry, connection, assignment, position, calculation, note, model,
	// test and run, the run's files too.
	again, err := b.ExportProject(ctx, "Driver board")
	if err != nil {
		t.Fatal(err)
	}
	pfA, filesA := unzipProject(t, exp.Data)
	pfB, filesB := unzipProject(t, again.Data)
	// SD9001 has no datasheet here: that's the one expected difference.
	for i := range pfA.Parts {
		if pfA.Parts[i].MPN == "SD9001" {
			pfA.Parts[i].Datasheet = nil
		}
	}
	ja, _ := json.MarshalIndent(comparable(pfA), "", " ")
	jb, _ := json.MarshalIndent(comparable(pfB), "", " ")
	if string(ja) != string(jb) {
		t.Errorf("the project changed on the way:\n--- exported\n%s\n--- imported and exported again\n%s", ja, jb)
	}
	if filesB["runs/1/results.raw"] != filesA["runs/1/results.raw"] {
		t.Errorf("run files: %v", keysOf(filesB))
	}
	// The diagram draws the same parts, with the same pins wired.
	da, _ := a.ProjectDetail(ctx, "Driver board")
	db, _ := b.ProjectDetail(ctx, "Driver board")
	if len(da.Connections.Diagram.Parts) != len(db.Connections.Diagram.Parts) || len(da.Connections.Connections) != len(db.Connections.Connections) || len(da.Connections.Pinouts) != len(db.Connections.Pinouts) {
		t.Errorf("diagram %d parts, %d connections, %d pinouts; imported %d, %d, %d", len(da.Connections.Diagram.Parts), len(da.Connections.Connections), len(da.Connections.Pinouts), len(db.Connections.Diagram.Parts), len(db.Connections.Connections), len(db.Connections.Pinouts))
	}

	// Into the library it came from: a new name, everything in the
	// library already.
	res, err = a.ImportProject(ctx, ProjectImportRequest{Data: exp.Data})
	if err != nil {
		t.Fatal(err)
	}
	if res.Project != "Driver board (2)" || res.Counts[PartInLibrary] != 2 {
		t.Errorf("same library: %+v", res)
	}
	// A name given.
	if res, err := a.ImportProject(ctx, ProjectImportRequest{Data: exp.Data, Name: "Driver rev B"}); err != nil || res.Project != "Driver rev B" {
		t.Errorf("named: %+v %v", res, err)
	}
}

func TestProjectImportFetchesAndChecks(t *testing.T) {
	a := newService(t)
	exportFixture(t, a)
	ctx := context.Background()
	exp, err := a.ExportProject(ctx, "Driver board")
	if err != nil {
		t.Fatal(err)
	}
	// SD9001 came from a URL; SD1234's datasheet here is another revision.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/datasheets/sd9001.pdf" {
			// A series datasheet that never prints SD9001 itself.
			w.Write(testutil.Mini("SD9000", "LDO"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	data := rezip(t, exp.Data, func(pf *ProjectFile) {
		for i := range pf.Parts {
			switch pf.Parts[i].MPN {
			case "SD9001":
				pf.Parts[i].Datasheet.SourceURL = srv.URL + "/datasheets/sd9001.pdf"
			case "SD1234":
				pf.Parts[i].Datasheet.SHA256 = strings.Repeat("0", 64)
				pf.Parts[i].Datasheet.Revision = "rev C"
			}
		}
		// The library's model of SD1234 will differ from the project's.
		pf.Models[0].Text = ".subckt SD1234 in out gnd\nR1 in out 2\n.ends\n"
	}, nil)
	b := newService(t)
	if _, err := b.Lib.SetSpiceModel(ctx, library.SpiceModel{PartID: mustPart(t, b, "SD1234").ID, Name: "SD1234", Text: ".subckt SD1234 in out gnd\nR1 in out 1\n.ends\n"}); err != nil {
		t.Fatal(err)
	}
	res, err := b.ImportProject(ctx, ProjectImportRequest{Data: data, Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]ProjectImportPart{}
	for _, p := range res.Parts {
		status[p.MPN] = p
	}
	if p := status["SD9001"]; p.Status != PartImported {
		t.Errorf("SD9001 from its source URL: %+v", p)
	}
	// The series' datasheet now gives SD9001, as the project knew it.
	if p := mustPart(t, b, "SD9001"); p.MPN != "SD9001" || p.DatasheetID == 0 || p.Category != "ldo" {
		t.Errorf("SD9001 attached to its datasheet: %+v", p)
	}

	if p := status["SD1234"]; p.Status != PartOtherVersion || !strings.Contains(p.Note, "rev C") {
		t.Errorf("SD1234, another revision: %+v", p)
	}
	if len(res.Notes) != 1 || !strings.Contains(res.Notes[0], "library's version was kept") {
		t.Errorf("notes: %q", res.Notes)
	}
	models, _ := b.Lib.SpiceModels(ctx, mustPart(t, b, "SD1234").ID)
	if len(models) != 1 || !strings.Contains(models[0].Text, "R1 in out 1") {
		t.Errorf("the library's model was replaced: %+v", models)
	}

	// A third library has the series' datasheet already, under SD9000:
	// SD9001 joins it with nothing to download, and the prefix match
	// SD9001 → SD9000 is never taken for the part.
	c := newService(t)
	if _, err := c.Import(ctx, ImportRequest{Data: testutil.Mini("SD9000", "LDO"), Filename: "sd9000.pdf"}); err != nil {
		t.Fatal(err)
	}
	series, _ := c.Lib.Resolve(ctx, "SD9000")
	d, _ := c.Lib.Datasheet(ctx, series.Part.DatasheetID)
	data = rezip(t, exp.Data, func(pf *ProjectFile) {
		for i := range pf.Parts {
			if pf.Parts[i].MPN == "SD9001" {
				pf.Parts[i].Datasheet.SHA256, pf.Parts[i].Datasheet.SourceURL = d.SHA256, "http://127.0.0.1:1/unreachable.pdf"
			}
		}
	}, nil)
	res, err = c.ImportProject(ctx, ProjectImportRequest{Data: data, Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Parts {
		if p.MPN == "SD9001" && (p.Status != PartInLibrary || p.Part != "") {
			t.Errorf("SD9001 by its datasheet's hash: %+v", p)
		}
	}
	if p := mustPart(t, c, "SD9001"); p.MPN != "SD9001" || p.DatasheetID != d.ID {
		t.Errorf("SD9001 in the third library: %+v", p)
	}
}

func TestProjectImportRefuses(t *testing.T) {
	a := newService(t)
	exportFixture(t, a)
	ctx := context.Background()
	exp, err := a.ExportProject(ctx, "Driver board")
	if err != nil {
		t.Fatal(err)
	}
	b := newService(t)
	for name, tc := range map[string]struct {
		data []byte
		want string
	}{
		"not a zip":        {[]byte("hello"), "not a project file"},
		"a file up a path": {rezip(t, exp.Data, nil, map[string]string{"runs/1/../../evil": "x"}), "unexpected file"},
		"a stray file":     {rezip(t, exp.Data, nil, map[string]string{"notes.txt": "x"}), "unexpected file"},
		"a newer version":  {rezip(t, exp.Data, func(pf *ProjectFile) { pf.Version = ProjectFileVersion + 1 }, nil), "newer SheetDiver"},
		"another format":   {rezip(t, exp.Data, func(pf *ProjectFile) { pf.Format = "kicad" }, nil), "not a SheetDiver project file"},
		// A run's files that don't belong to it fail the import half way:
		// the half-made project goes.
		"a run's files elsewhere": {rezip(t, exp.Data, func(pf *ProjectFile) { pf.Tests[0].Variations[0].Runs[0].Files = "runs/../x" }, nil), "bad run folder"},
	} {
		if _, err := b.ImportProject(ctx, ProjectImportRequest{Data: tc.data}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	if ps, _ := b.Lib.Projects(ctx); len(ps) != 0 {
		t.Errorf("a refused import left %d projects", len(ps))
	}
	// The run folders of a project removed half way are gone too.
	ents, _ := os.ReadDir(filepath.Join(b.Lib.Dir, "simulations"))
	if len(ents) != 0 {
		t.Errorf("run folders left: %d", len(ents))
	}
}

// mustPart resolves a part or fails.
func mustPart(t *testing.T, s *Service, ref string) library.Part {
	t.Helper()
	r, err := s.Lib.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	return r.Part
}
