package extract

import (
	"slices"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// synth builds documents for tests without PDFs: each page is a list of
// spans and drawings on a US-letter page.
type synth struct{ doc *pdf.Document }

// newSynth starts a document with n pages.
func newSynth(n int) *synth {
	d := &pdf.Document{Meta: map[string]string{}}
	for i := 0; i < n; i++ {
		d.Pages = append(d.Pages, pdf.Page{Index: i, Width: 612, Height: 792})
	}
	return &synth{d}
}

// text adds a span at (x, y) in a font size; characters are size/2 wide.
func (s *synth) text(page int, x, y, size float64, t string) *synth {
	s.doc.Pages[page].Spans = append(s.doc.Pages[page].Spans, tspan(x, y, size, t))
	return s
}

// bold adds a bold span.
func (s *synth) bold(page int, x, y, size float64, t string) *synth {
	sp := tspan(x, y, size, t)
	sp.Bold = true
	s.doc.Pages[page].Spans = append(s.doc.Pages[page].Spans, sp)
	return s
}

// drawing adds a vector drawing.
func (s *synth) drawing(page int, x0, y0, x1, y1 float64) *synth {
	s.doc.Pages[page].Graphics = append(s.doc.Pages[page].Graphics, pdf.Graphic{Box: pdf.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1}, Kind: "path"})
	return s
}

// header adds a running header to every page.
func (s *synth) header(t string) *synth {
	for i := range s.doc.Pages {
		s.text(i, 400, 40, 12, t)
	}
	return s
}

// chineseCharger is a TP4056-like Chinese datasheet: no outline, no
// figure captions, Chinese headings and table headers, a single rating
// column with "~" ranges, prose pins.
func chineseCharger() *pdf.Document {
	s := newSynth(3).header("TP9999 1A 锂电池充电器")
	s.text(0, 46, 90, 18, "概述").
		text(0, 46, 115, 10, "TP9999是一款单节锂离子电池恒流/恒压线性充电器，采用").
		text(0, 46, 128, 10, "SOP8封装，非常适合便携式设备应用。").
		text(0, 46, 160, 18, "特点").
		text(0, 46, 185, 10, " 最大充电电流：1A").
		text(0, 46, 198, 10, " 待机电流 40uA").
		text(0, 46, 240, 18, "典型应用电路").
		drawing(0, 100, 260, 500, 460).
		text(0, 120, 300, 7, "VCC").
		text(0, 400, 300, 7, "BAT")
	s.text(1, 46, 90, 18, "管脚").
		drawing(1, 200, 110, 400, 230).
		text(1, 46, 260, 18, "极限参数 （注 1）").
		text(1, 60, 285, 10, "符号").text(1, 160, 285, 10, "参数").text(1, 360, 285, 10, "额定值").text(1, 460, 285, 10, "单位").
		text(1, 60, 300, 10, "VCC").text(1, 160, 300, 10, "输入电源电压").text(1, 360, 300, 10, "-0.3~7").text(1, 460, 300, 10, "V").
		text(1, 60, 315, 10, "TSTG").text(1, 160, 315, 10, "储存温度").text(1, 360, 315, 10, "-65~125").text(1, 460, 315, 10, "℃").
		text(1, 46, 360, 18, "电气参数").
		text(1, 46, 385, 10, "符号").text(1, 100, 385, 10, "参数").text(1, 220, 385, 10, "测试条件").
		text(1, 390, 385, 10, "最小").text(1, 440, 385, 10, "典型").text(1, 490, 385, 10, "最大").text(1, 540, 385, 10, "单位").
		text(1, 46, 400, 10, "ICC").text(1, 100, 400, 10, "输入电源电流").text(1, 220, 400, 10, "充电模式").text(1, 445, 400, 10, "130").text(1, 495, 400, 10, "300").text(1, 540, 400, 10, "µA").
		text(1, 220, 415, 10, "待机模式").text(1, 445, 415, 10, "50").text(1, 495, 415, 10, "100").text(1, 540, 415, 10, "µA").
		text(1, 220, 430, 10, "停机模式").text(1, 445, 430, 10, "40").text(1, 495, 430, 10, "80").text(1, 540, 430, 10, "µA").
		text(1, 46, 445, 10, "TSS").text(1, 100, 445, 10, "软启动时间").text(1, 445, 445, 10, "20").text(1, 540, 445, 10, "uS")
	s.text(2, 46, 90, 18, "引脚功能").
		text(2, 46, 115, 14, "TEMP(PIN1)：电池温度检测输入端").
		text(2, 46, 135, 10, "将TEMP管脚接电池的NTC传感器的输出端。").
		text(2, 46, 160, 14, "PROG(PIN2)：恒流充电电流设置端").
		text(2, 46, 180, 10, "从PROG管脚连接一个电阻到GND可以对充电电流进行设定。").
		text(2, 46, 205, 14, "V").text(2, 53, 209, 9, "CC").text(2, 66, 205, 14, "(PIN4)：输入电压正端").
		text(2, 46, 225, 10, "此管脚的电压为内部电路的工作电源。")
	return s.doc
}

func TestChineseDatasheetWithoutTranslation(t *testing.T) {
	r := Analyze(chineseCharger(), "tp9999.pdf")
	if r.Language != "zh" || r.Category != "charger" || r.Title != "TP9999 1A 锂电池充电器" {
		t.Errorf("language %q, category %q, title %q", r.Language, r.Category, r.Title)
	}
	kinds := map[string]bool{}
	for _, s := range r.Sections {
		kinds[s.Kind] = true
	}
	for _, k := range []string{KindDescription, KindFeatures, KindTypicalApp, KindPins, KindAbsMax, KindElectrical} {
		if !kinds[k] {
			t.Errorf("no %s section: %+v", k, r.Sections)
		}
	}
	// Uncaptioned drawings under their headings.
	figs := map[string]string{}
	for _, f := range r.Figures {
		figs[f.Caption] = f.Kind
	}
	if figs["典型应用电路"] != FigSchematic || figs["管脚"] != FigPinout {
		t.Errorf("figures = %+v", r.Figures)
	}
	byKey := map[string]Param{}
	var ratings []Param
	for _, p := range r.Params {
		if p.Section == KindAbsMax {
			ratings = append(ratings, p)
		}
		if p.Key != "" {
			if _, dup := byKey[p.Key]; !dup {
				byKey[p.Key] = p
			}
		}
	}
	// "Rating" column ranges.
	if len(ratings) != 2 || ratings[0].Min == nil || *ratings[0].Min != -0.3 || *ratings[0].Max != 7 || *ratings[1].Min != -65 {
		t.Errorf("ratings = %+v", ratings)
	}
	// Standby isn't the quiescent current; shutdown is the shutdown current.
	if p := byKey["iq"]; p.Max == nil || *p.Max != 300 {
		t.Errorf("iq = %+v", p)
	}
	if p := byKey["isd"]; p.Max == nil || *p.Max != 80 {
		t.Errorf("isd = %+v", p)
	}
	if p := byKey["tss"]; p.Unit != "µs" {
		t.Errorf("tss = %+v (uS is µs on a time row)", p)
	}
	// Pins described in prose, subscripts joined.
	var names []string
	for _, p := range r.Pins {
		names = append(names, p.Number+" "+p.Name)
	}
	if !slices.Equal(names, []string{"1 TEMP", "2 PROG", "4 VCC"}) {
		t.Errorf("pins = %v", names)
	}
	// Section text: bullets, running header left out, tables as tables.
	for _, s := range r.Sections {
		if strings.Contains(s.Text, "锂电池充电器") {
			t.Errorf("%s has the running header: %q", s.Title, s.Text)
		}
		switch s.Kind {
		case KindFeatures:
			if s.Text != "• 最大充电电流：1A\n• 待机电流 40uA" {
				t.Errorf("features text = %q", s.Text)
			}
		case KindAbsMax:
			if !strings.Contains(s.Text, "| Symbol | Parameter | Value | Unit |") || !strings.Contains(s.Text, "| VCC | 输入电源电压 | -0.3~7 | V |") {
				t.Errorf("ratings text = %q", s.Text)
			}
		case KindTypicalApp:
			if s.Text != "[Figure: 典型应用电路]" {
				t.Errorf("figure text = %q", s.Text)
			}
		}
	}
}

func TestStaleOutline(t *testing.T) {
	s := newSynth(3)
	s.doc.Meta["Title"] = "FSXXXX Datasheet"
	s.bold(0, 46, 80, 18, "DW99A").
		bold(0, 46, 120, 16, "General Description").
		text(0, 46, 145, 10, "The DW99A battery protection IC protects lithium-ion batteries from overcharge.").
		bold(0, 46, 200, 16, "Features").
		text(0, 46, 225, 10, "z Low quiescent current").
		text(0, 46, 240, 10, "z Precision overcharge detection").
		text(0, 64, 252, 10, "4.3V ± 50mV").
		text(0, 46, 270, 10, "z Two overcurrent levels")
	s.bold(1, 46, 120, 16, "Absolute Maximum Ratings").
		bold(1, 46, 400, 16, "Timing Diagram").
		text(1, 79, 430, 12, "1. Overcharge Condition ÆLoad Discharging Æ Normal Condition").
		drawing(1, 100, 450, 500, 650)
	s.bold(2, 46, 120, 16, "Electrical Characteristics")
	// The outline is from an older revision: one bookmark is on the wrong
	// page, one has no destination, one heading is missing, and a child's
	// arrows are garbled.
	s.doc.Bookmarks = []pdf.Bookmark{
		{Title: "General Description", Page: 0, Y: 113, HasY: true},
		{Title: "Features", Page: 0, Y: 190, HasY: true},
		{Title: "Description of Operation", Page: -1},
		{Title: "Timing Diagram", Page: 2, Y: 113, HasY: true, Children: []pdf.Bookmark{
			{Title: "Overcharge Condition (Load Discharging ( Normal Condition", Page: 1, Y: 425, HasY: true},
		}},
		{Title: "Electrical Characteristics", Page: 2, Y: 113, HasY: true},
	}
	r := Analyze(s.doc, "dw99a.pdf")
	if r.Title != "DW99A" || r.Category != "battery-protection" {
		t.Errorf("title %q (template metadata title ignored), category %q", r.Title, r.Category)
	}
	got := map[string]Section{}
	for _, sec := range r.Sections {
		got[sec.Title] = sec
	}
	if _, ok := got["Description of Operation"]; ok {
		t.Error("a bookmark with no destination and no heading is dropped")
	}
	if sec, ok := got["Timing Diagram"]; !ok || sec.Page != 1 {
		t.Errorf("a misplaced bookmark is found on its real page: %+v", sec)
	}
	if _, ok := got["Absolute Maximum Ratings"]; !ok {
		t.Errorf("headings the stale outline lacks are added: %+v", r.Sections)
	}
	child, ok := got["Overcharge Condition → Load Discharging → Normal Condition"]
	if !ok || child.Kind != KindTiming {
		t.Errorf("the child is titled as printed and takes the diagram kind: %+v", r.Sections)
	}
	if len(r.Figures) != 1 || r.Figures[0].Caption != child.Title || r.Figures[0].Kind != FigTiming {
		t.Errorf("the timing diagram is named by its own sub-section: %+v", r.Figures)
	}
	if f := got["Features"].Text; f != "• Low quiescent current\n• Precision overcharge detection 4.3V ± 50mV\n• Two overcurrent levels" {
		t.Errorf("letter bullets and indented continuations: %q", f)
	}
}

func TestLinesSplitAtSizeJumps(t *testing.T) {
	p := pdf.Page{Width: 612, Height: 792, Spans: []pdf.Span{
		tspan(46, 250, 10, "body text running to the column edge"),
		tspan(240, 248, 18, "Applications"),
		tspan(120, 250, 10, "V"), // touching subscripts stay
	}}
	p.Spans[2] = pdf.Span{Text: "CC", Size: 6, Box: pdf.Rect{X0: 226, Y0: 248, X1: 233, Y1: 255}}
	var texts []string
	for _, l := range Lines(p) {
		texts = append(texts, l.Text)
	}
	if !slices.Contains(texts, "Applications") {
		t.Errorf("a heading level with body text in the next column is its own line: %q", texts)
	}
}

func TestValueCells(t *testing.T) {
	for in, want := range map[string][3]float64{
		"-0.3~7":     {-0.3, 7, 2},
		"−40 to 85":  {-40, 85, 2},
		"-65～150":    {-65, 150, 2},
		"0...5.5":    {0, 5.5, 2},
		"150":        {150, 150, 1},
		"VDD+0.3":    {0, 0, 0},
		"":           {0, 0, 0},
		"-0.3 to 6V": {-0.3, 6, 2},
	} {
		lo, hi, n := parseRange(in)
		if lo != want[0] || hi != want[1] || float64(n) != want[2] {
			t.Errorf("parseRange(%q) = %v %v %d, want %v", in, lo, hi, n, want)
		}
	}
	for in, want := range map[[2]string]string{
		{"uS", "TSS Soft-start time"}: "µs",
		{"mS", "tDLY Delay"}:          "ms",
		{"mS", "gm Transconductance"}: "mS",
		{"µA", "Soft-start time"}:     "µA",
	} {
		if got := timeUnit(in[0], in[1]); got != want {
			t.Errorf("timeUnit(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
	for in, want := range map[[2]string]string{
		{"iq", "Standby mode (charging done)"}: "",
		{"iq", "Shutdown mode, EN = 0"}:        "isd",
		{"iq", "停机模式"}:                         "isd",
		{"ichg", "Sleep mode, VCC = 0"}:        "",
		{"iq", "Charging, RPROG = 1.2k"}:       "iq",
		{"vin", "Standby mode"}:                "vin",
	} {
		if got := modeKey(in[0], in[1]); got != want {
			t.Errorf("modeKey(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestSymbolArrows(t *testing.T) {
	for in, want := range map[string]string{
		"1. Overcharge Condition ÆLoad Discharging Æ Normal Condition": "1. Overcharge Condition → Load Discharging → Normal Condition",
		"Ærøskøbing Æble": "Ærøskøbing Æble",
		"plain":           "plain",
	} {
		if got := symbolArrows(in); got != want {
			t.Errorf("symbolArrows(%q) = %q, want %q", in, got, want)
		}
	}
}
