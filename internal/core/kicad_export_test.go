package core

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/kicad"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

func TestExportKiCad(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	buildCircuit(t, s, circuit("USB-C to 3V3"))
	dir := t.TempDir()
	out, err := s.ExportKiCad(ctx, "USB-C to 3V3", dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"USB-C_to_3V3.kicad_pro", "USB-C_to_3V3.kicad_sch", "sheetdiver.kicad_sym", "sym-lib-table", "USB-C_to_3V3-layout-guide.pdf"}
	for _, f := range want {
		if !slices.Contains(out.Files, f) {
			t.Errorf("files %q lack %s", out.Files, f)
		}
	}
	sch, _ := os.ReadFile(filepath.Join(dir, "USB-C_to_3V3.kicad_sch"))
	// Blocks named "U2 TPS63900" are U2 with value TPS63900; each passive
	// of a pair is its own symbol.
	for _, w := range []string{`(property "Reference" "U2"`, `(property "Value" "TPS63900"`, `(property "Reference" "C3"`, `(property "Reference" "C4"`, `(property "Reference" "Rd1"`} {
		if !strings.Contains(string(sch), w) {
			t.Errorf("schematic lacks %s", w)
		}
	}
	// With KiCad's footprints installed, a board comes too.
	if kicad.FindFootprints() != nil {
		if out.Board != "USB-C_to_3V3.kicad_pcb" || !slices.Contains(out.Files, out.Board) {
			t.Errorf("board %q in %q", out.Board, out.Files)
		}
	}
	if out.Components != 15 {
		t.Errorf("components = %d", out.Components)
	}

	// The guide reads, with the board's and each part's advice.
	guide, _ := os.ReadFile(filepath.Join(dir, out.Guide))
	e := pdf.NewEngine(pdf.DefaultCacheDir())
	defer e.Close()
	doc, err := e.Extract(ctx, guide, nil)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, p := range doc.Pages {
		for _, sp := range p.Spans {
			text.WriteString(sp.Text + " ")
		}
	}
	for _, w := range []string{"PCB layout guide", "whole board", "ground plane", "U2", "Switching loop", "Footprints", "No datasheet in the library"} {
		if !strings.Contains(text.String(), w) {
			t.Errorf("guide lacks %q", w)
		}
	}

	// KiCad reads the schematic with every connection on its net.
	cli, err := exec.LookPath("kicad-cli")
	if err != nil {
		return
	}
	netlist := filepath.Join(dir, "board.net")
	cmd := exec.Command(cli, "sch", "export", "netlist", "-o", netlist, filepath.Join(dir, "USB-C_to_3V3.kicad_sch"))
	cmd.Env = append(os.Environ(), "HOME="+dir)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("kicad-cli: %v\n%s", err, b)
	}
	b, _ := os.ReadFile(netlist)
	nets := map[string][]string{}
	netRe := regexp.MustCompile(`\(net \(code "?\d+"?\) \(name "([^"]*)"\)((?:\s*\(node [^\n]*\))*)`)
	nodeRe := regexp.MustCompile(`\(node \(ref "([^"]+)"\) \(pin "([^"]+)"\)`)
	for _, m := range netRe.FindAllStringSubmatch(string(b), -1) {
		for _, n := range nodeRe.FindAllStringSubmatch(m[2], -1) {
			nets[m[1]] = append(nets[m[1]], n[1]+"."+n[2])
		}
		slices.Sort(nets[m[1]])
	}
	// Each net has its pins, whatever KiCad names it: a net wired inside
	// a part's cluster has no label.
	netOf := map[string]string{}
	for net, pins := range nets {
		for _, p := range pins {
			netOf[p] = net
		}
	}
	for _, pins := range [][]string{
		{"L1.1", "U2.LX1"},
		{"R4.1", "U2.CFG1"},
		{"C3.1", "C4.1", "U2.VOUT"},
		{"J1.CC1", "Rd1.1"},
	} {
		if net := netOf[pins[0]]; !slices.Equal(nets[net], pins) {
			t.Errorf("%s is on %q with %q, want %q", pins[0], net, nets[net], pins)
		}
	}
	for _, p := range []string{"C3.2", "R4.2", "U2.GND", "J1.GND", "U1.GND"} {
		if !slices.Contains(nets["GND"], p) {
			t.Errorf("GND %q lacks %s", nets["GND"], p)
		}
	}
}

func TestSchematicPlacement(t *testing.T) {
	col := func(n int) *int { return &n }
	parts := []DiagramPart{
		{ID: "part:1", Column: 0, Group: "Power"}, {ID: "part:2", Column: 1, Group: "Power"},
		{ID: "part:3", Column: 1, Group: "Power"}, {ID: "part:4", Column: 0, Want: &Cell{Column: col(2)}},
	}
	// Where the app drew a part wins; else where it's asked to be, else
	// its power-flow column, down the column in diagram order.
	cells := partCells(parts, map[string]kicad.Cell{"part:1": {Column: 3, Row: 1}})
	want := map[string]kicad.Cell{"part:1": {Column: 3, Row: 1}, "part:2": {Column: 1}, "part:3": {Column: 1, Row: 1}, "part:4": {Column: 2}}
	for id, c := range want {
		if cells[id] != c {
			t.Errorf("%s: %+v, want %+v", id, cells[id], c)
		}
	}
	// Supplies go on the left, first; a configuration pin from the bottom
	// goes on the left, last; ground stays at the bottom.
	pins := schematicSides([]kicad.Pin{
		{Name: "EN", Side: kicad.Left, Net: "EN"}, {Name: "VIN", Side: kicad.Top, Net: "VIN"},
		{Name: "CFG", Side: kicad.Bottom, Net: "CFG"}, {Name: "GND", Side: kicad.Bottom, Net: "GND"},
	}, func(net string) bool { return net == "GND" })
	var got []string
	for _, p := range pins {
		got = append(got, p.Name+":"+string(p.Side))
	}
	if want := []string{"VIN:left", "EN:left", "GND:bottom", "CFG:left"}; !slices.Equal(got, want) {
		t.Errorf("sides %q, want %q", got, want)
	}
}
