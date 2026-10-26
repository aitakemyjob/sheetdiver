package layoutguide

import (
	"math"
	"strings"
	"testing"
)

func TestAdvice(t *testing.T) {
	b := Board{
		Parts: []Part{
			{Ref: "U1", MPN: "TPS62160", Category: "buck", Pins: []Pin{{Name: "VIN", Role: "power", Net: "VIN"}, {Name: "SW", Role: "output", Net: "SW"}, {Name: "FB", Role: "input", Net: "FB"}, {Name: "PAD", Role: "pad", Net: "GND"}}},
			{Ref: "U2", MPN: "ESP32-C3", Category: "mcu", Pins: []Pin{{Name: "VDD3P3", Role: "power", Net: "3V3"}, {Name: "VDD3P3_RTC", Role: "power", Net: "3V3"}, {Name: "XTAL_P", Role: "input"}}},
			{Ref: "D1", MPN: "TPD2E2U06", Category: "tvs"},
		},
		Nets:  []Net{{Name: "I2C_SDA", Bus: "I2C1"}, {Name: "USB_DP"}, {Name: "USB_DM"}},
		Rails: []Rail{{Name: "3V3", Voltage: 3.3, Current: 1}},
	}
	all := Default().Advise(b)
	topics := func(as []Advice) string {
		var s []string
		for _, a := range as {
			s = append(s, a.Topic)
		}
		return strings.Join(s, ",")
	}
	if got := topics(ForPart(all, "U1")); got != "Switching loop,Switching loop,Switching loop,Feedback,Exposed pad" {
		t.Errorf("U1: %s", got)
	}
	if got := topics(ForPart(all, "U2")); got != "Decoupling" {
		t.Errorf("U2: %s", got)
	}
	if got := topics(ForPart(all, "D1")); got != "Protection" {
		t.Errorf("D1: %s", got)
	}
	board := BoardWide(all)
	if got := topics(board); got != "Ground,Ground,Power traces,I2C,USB" {
		t.Errorf("board: %s", got)
	}
	if !strings.Contains(board[2].Text, "0.30 mm") {
		t.Errorf("1 A trace: %s", board[2].Text)
	}
}

func TestTraceWidth(t *testing.T) {
	// IPC-2221 calculators: 1 A, 10 °C, 1 oz outer ≈ 0.30 mm; 3 A ≈ 1.37 mm.
	for _, c := range []struct{ a, want float64 }{{1, 0.30}, {3, 1.37}} {
		if got := traceWidthMM(c.a, 10, 1); math.Abs(got-c.want) > 0.03 {
			t.Errorf("%g A: %.3f mm, want %.2f", c.a, got, c.want)
		}
	}
}
