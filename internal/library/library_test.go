package library

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// openTemp opens an empty library in a temp dir.
func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// seed stores a small two-part datasheet.
func seed(t *testing.T, s *Store) int64 {
	t.Helper()
	v := 0.6
	id, err := s.SaveImport(context.Background(), &Import{
		Datasheet: Datasheet{SHA256: "abc", File: "x.pdf", Filename: "x.pdf", Title: "XYZ12x LDO", Manufacturer: "Acme", Pages: 2, Families: []string{"XYZ12x"}},
		Parts:     []Part{{MPN: "XYZ120", Category: "ldo"}, {MPN: "XYZ121", Category: "ldo"}},
		Variants:  []ImportVariant{{MPN: "XYZ120-33TR", Base: "XYZ120"}},
		Pages:     []Page{{Page: 0, Width: 612, Height: 792, Text: "Low dropout regulator with power-good"}, {Page: 1, Width: 612, Height: 792, Text: "Dropout voltage 300 mV"}},
		Sections:  []Section{{Title: "Electrical Characteristics", Level: 1, Kind: "electrical", Page: 1}},
		Figures:   []Figure{{Page: 1, Number: "3", Caption: "Dropout vs Load", Kind: "graph"}},
		Params:    []Parameter{{Key: "vdropout", Name: "Dropout voltage", Max: &v, Unit: "V", Section: "electrical", Page: 1, AppliesTo: []string{"XYZ121"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestResolve(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	ctx := context.Background()
	for ref, via := range map[string]string{"XYZ120": "mpn", "xyz120-33tr": "variant", "XYZ121-18TR": "prefix", "XYZ12x": "family"} {
		r, err := s.Resolve(ctx, ref)
		if err != nil || r.Via != via {
			t.Errorf("%s: %+v %v", ref, r, err)
		}
	}
	_, err := s.Resolve(ctx, "XYZ999")
	if err == nil || !strings.Contains(err.Error(), "similar: XYZ120") {
		t.Errorf("not-found error should suggest similar parts: %v", err)
	}
}

func TestSearchAndScoping(t *testing.T) {
	s := openTemp(t)
	ds := seed(t, s)
	ctx := context.Background()
	hits, err := s.Search(ctx, `power-good "regulator`, SearchOptions{})
	if err != nil || len(hits) == 0 || hits[0].Kind != "page" {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
	if hits, _ := s.Search(ctx, "XYZ120", SearchOptions{}); len(hits) == 0 || hits[0].Kind != "part" {
		t.Errorf("part hits come first: %+v", hits)
	}
	if ps, _ := s.Parameters(ctx, ds, ParamFilter{MPN: "XYZ120"}); len(ps) != 0 {
		t.Error("a parameter limited to XYZ121 must not apply to XYZ120")
	}
	n, err := s.AddNote(ctx, Note{TargetKind: TargetPart, TargetID: "XYZ120", Body: "Use the 3.3 V option; errata ES-7"}, ds)
	if err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.Search(ctx, "errata", SearchOptions{DatasheetID: ds}); len(hits) != 1 || hits[0].Kind != "note" {
		t.Errorf("notes are searchable: %+v", hits)
	}
	if err := s.DeleteNote(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if hits, _ := s.Search(ctx, "errata", SearchOptions{}); len(hits) != 0 {
		t.Error("deleted notes leave the index")
	}
}

func TestProjects(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	ctx := context.Background()
	p, err := s.CreateProject(ctx, "Sensor", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProject(ctx, "sensor", ""); err == nil {
		t.Error("names are unique ignoring case")
	}
	lo, hi := 4.5, 5.5
	r, err := s.SetRail(ctx, Rail{ProjectID: p.ID, Name: "5V", VMin: &lo, VMax: &hi})
	if err != nil || r.VNom == nil || *r.VNom != 5 {
		t.Fatalf("rail %+v %v", r, err)
	}
	part, _ := s.Resolve(ctx, "XYZ120")
	pp, err := s.AddProjectPart(ctx, ProjectPart{ProjectID: p.ID, PartID: part.Part.ID, Designator: "U1", SupplyRail: "5V"})
	if err != nil {
		t.Fatal(err)
	}
	// Adding the same designator again updates instead of duplicating.
	pp.Role = "3V3 LDO"
	if _, err := s.AddProjectPart(ctx, pp); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ProjectParts(ctx, p.ID)
	if len(list) != 1 || list[0].Role != "3V3 LDO" || list[0].MPN != "XYZ120" {
		t.Errorf("BOM = %+v", list)
	}
	ps, _ := s.Projects(ctx)
	if len(ps) != 1 || ps[0].Parts != 1 || ps[0].Rails != 1 {
		t.Errorf("projects = %+v", ps)
	}
	if _, err := s.Project(ctx, "nope"); err == nil || !strings.Contains(err.Error(), "Sensor") {
		t.Errorf("unknown project lists the others: %v", err)
	}
}

func TestDeleteDatasheetKeepsParts(t *testing.T) {
	s := openTemp(t)
	ds := seed(t, s)
	ctx := context.Background()
	if _, err := s.DeleteDatasheet(ctx, ds); err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve(ctx, "XYZ120")
	if err != nil || r.Part.DatasheetID != 0 {
		t.Errorf("part after delete: %+v %v", r, err)
	}
	if hits, _ := s.Search(ctx, "dropout", SearchOptions{}); len(hits) != 0 {
		t.Error("the datasheet's index entries are gone")
	}
}

func TestFavoritesAndStatus(t *testing.T) {
	s := openTemp(t)
	seed(t, s)
	ctx := context.Background()
	r, _ := s.Resolve(ctx, "XYZ121")
	yes, preferred := true, "Preferred"
	p, err := s.UpdatePart(ctx, r.Part.ID, PartUpdate{Favorite: &yes, Status: &preferred})
	if err != nil || !p.Favorite || p.Status != StatusPreferred {
		t.Fatalf("update = %+v %v", p, err)
	}
	bad := "great"
	if _, err := s.UpdatePart(ctx, r.Part.ID, PartUpdate{Status: &bad}); err == nil {
		t.Error("unknown statuses are rejected")
	}
	all, _ := s.Parts(ctx, PartFilter{})
	if len(all) != 2 || all[0].MPN != "XYZ121" {
		t.Errorf("favorites list first: %+v", all)
	}
	if fav, _ := s.Parts(ctx, PartFilter{Favorite: true}); len(fav) != 1 || fav[0].MPN != "XYZ121" {
		t.Errorf("favorite filter: %+v", fav)
	}
	if st, _ := s.Parts(ctx, PartFilter{Status: StatusPreferred}); len(st) != 1 {
		t.Errorf("status filter: %+v", st)
	}
	if !Discouraged(StatusEOL) || Discouraged(StatusApproved) {
		t.Error("discouraged statuses")
	}
}

func TestReextractReplacesUncaptionedFigures(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	in := func(figs ...Figure) *Import {
		return &Import{
			Datasheet: Datasheet{SHA256: "zh", File: "z.pdf", Filename: "z.pdf", Pages: 4},
			Parts:     []Part{{MPN: "TP4056"}},
			Figures:   figs,
		}
	}
	ds, err := s.SaveImport(ctx, in(
		Figure{Page: 0, Caption: "典型应用电路", Kind: "schematic"},
		Figure{Page: 1, Caption: "管脚", Kind: "pinout"},
		Figure{Page: 3, Caption: "内部框图", Kind: "block_diagram"},
	))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Figures(ctx, ds, FigureFilter{})
	// Re-extracted from the translation: same drawings, English captions,
	// one more found, one gone.
	if _, err := s.SaveImport(ctx, in(
		Figure{Page: 0, Caption: "Typical application circuit", Kind: "schematic"},
		Figure{Page: 1, Caption: "Pin configuration", Kind: "pinout"},
		Figure{Page: 6, Caption: "Package dimensions", Kind: "package"},
	)); err != nil {
		t.Fatal(err)
	}
	after, _ := s.Figures(ctx, ds, FigureFilter{})
	if len(after) != 3 {
		t.Fatalf("figures = %d, want 3: %+v", len(after), after)
	}
	ids := map[string]int64{}
	for _, f := range after {
		ids[f.Caption] = f.ID
	}
	if ids["Typical application circuit"] != before[0].ID || ids["Pin configuration"] != before[1].ID {
		t.Errorf("figures should keep their IDs: before %+v, after %+v", before, after)
	}
}

func TestTranslations(t *testing.T) {
	s := openTemp(t)
	ctx := context.Background()
	var items []Translation
	var sources []string
	for i := 0; i < 450; i++ {
		src := fmt.Sprintf("参数%d", i)
		items = append(items, Translation{Lang: "zh", Source: " " + src + " ", Text: fmt.Sprintf("Parameter %d", i)})
		sources = append(sources, src)
	}
	items = append(items, Translation{Lang: "zh", Source: "空", Text: " "}, Translation{Lang: "", Source: "x", Text: "y"})
	if err := s.SaveTranslations(ctx, items); err != nil {
		t.Fatal(err)
	}
	got, err := s.Translations(ctx, "zh", append(sources, "空", "未知"))
	if err != nil || len(got) != 450 || got["参数449"] != "Parameter 449" {
		t.Fatalf("got %d translations, %v", len(got), err)
	}
	// A later translation replaces the earlier one; languages are separate.
	if err := s.SaveTranslations(ctx, []Translation{{Lang: "zh", Source: "参数0", Text: "Better"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Translations(ctx, "zh", []string{"参数0"}); got["参数0"] != "Better" {
		t.Errorf("replace: %v", got)
	}
	if got, _ := s.Translations(ctx, "ja", []string{"参数0"}); len(got) != 0 {
		t.Errorf("other language: %v", got)
	}
}
