package calc

import (
	"math"
	"strings"
	"testing"
)

// near reports whether a and b agree within rel.
func near(a, b, rel float64) bool { return math.Abs(a-b) <= rel*math.Abs(b) }

func TestBuckMatchesHandCalculation(t *testing.T) {
	// TPS563200 reference design: 12 V → 1.05 V, 3 A, 650 kHz.
	r, err := Buck(BuckInput{VinMin: 4.5, VinMax: 12, Vout: 1.05, Iout: 3, Fsw: 650e3, RippleRatio: 0.3, Vref: 0.765, RBottom: 10e3})
	if err != nil {
		t.Fatal(err)
	}
	lcalc, _ := r.Get("Lcalc")
	want := (12 - 1.05) * 1.05 / (12 * 650e3 * 0.3 * 3)
	if !near(lcalc, want, 1e-9) {
		t.Fatalf("Lcalc = %g, want %g", lcalc, want)
	}
	l, _ := r.Get("L")
	if l != 1.8e-6 {
		t.Errorf("L = %g, want the next E12 value 1.8 µH", l)
	}
	rt, _ := r.Get("Rtop,E96")
	if !near(rt, 3740, 0.01) {
		t.Errorf("Rtop = %g, want 3.74 kΩ (the datasheet's value)", rt)
	}
}

func TestBuckFlagsMinimumOnTime(t *testing.T) {
	r, err := Buck(BuckInput{VinMin: 20, VinMax: 36, Vout: 1.0, Iout: 1, Fsw: 2.2e6, TonMin: 80e-9})
	if err != nil {
		t.Fatal(err)
	}
	if r.Worst() != Error || !strings.Contains(r.Checks[0].Message, "minimum on-time") {
		t.Fatalf("expected an on-time error, got %+v", r.Checks)
	}
}

func TestBuckRejectsVoutAboveVin(t *testing.T) {
	if _, err := Buck(BuckInput{VinMin: 3, VinMax: 5, Vout: 5, Iout: 1, Fsw: 1e6}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestBoostSwitchLimit(t *testing.T) {
	// TPS61023-like: 1 cell to 5 V, 3.7 A switch limit.
	r, err := Boost(BoostInput{VinMin: 3.0, VinMax: 4.2, Vout: 5, Iout: 1.5, Fsw: 1e6, IlimSwitch: 3.7})
	if err != nil {
		t.Fatal(err)
	}
	ipk, _ := r.Get("Ipk")
	if ipk < 2.9 || ipk > 3.4 {
		t.Errorf("Ipk = %g, want about 3.1 A", ipk)
	}
	if r.Worst() == Error {
		t.Errorf("unexpected error: %+v", r.Checks)
	}
	r2, _ := Boost(BoostInput{VinMin: 1.0, VinMax: 1.5, Vout: 5, Iout: 1, Fsw: 1e6, IlimSwitch: 3.7})
	if r2.Worst() != Error {
		t.Errorf("1 V → 5 V at 1 A should exceed a 3.7 A switch: %+v", r2.Checks)
	}
}

func TestGateDividerWindow(t *testing.T) {
	// P-FET high-side switch on 9–24 V, needs 4.5 V, rated ±20 V.
	r, err := GateDivider(GateDividerInput{VinMin: 9, VinMax: 24, VgsOn: 4.5, VgsMax: 20})
	if err != nil {
		t.Fatal(err)
	}
	lo, _ := r.Get("Vgs,min")
	hi, _ := r.Get("Vgs,max")
	if lo < 4.5 || hi > 16 {
		t.Fatalf("Vgs range %g–%g outside 4.5–16 V", lo, hi)
	}
	// 5–60 V can't be done with resistors alone.
	r, _ = GateDivider(GateDividerInput{VinMin: 5, VinMax: 60, VgsOn: 4.5, VgsMax: 20})
	if r.Worst() != Error || !strings.Contains(r.Summary, "zener") {
		t.Fatalf("expected the zener recommendation, got %q %+v", r.Summary, r.Checks)
	}
}

func TestDividerFindsExactRatio(t *testing.T) {
	r, err := Divider(DividerInput{Vin: 5, Vout: 2.5})
	if err != nil {
		t.Fatal(err)
	}
	v, _ := r.Get("Vtap")
	if !near(v, 2.5, 1e-6) {
		t.Fatalf("Vtap = %g", v)
	}
}

func TestLDOThermals(t *testing.T) {
	r, err := LDO(LDOInput{VinMin: 5, VinMax: 5, Vout: 3.3, Iout: 0.5, RthJA: 180, Dropout: 0.3})
	if err != nil {
		t.Fatal(err)
	}
	tj, _ := r.Get("Tj")
	if !near(tj, 25+1.7*0.5*180, 1e-9) {
		t.Fatalf("Tj = %g", tj)
	}
	if r.Worst() != Error {
		t.Fatalf("178 °C junction should be an error: %+v", r.Checks)
	}
}

func TestSeriesAndEng(t *testing.T) {
	if v := NextUp(2.1e-6, "E12"); v != 2.2e-6 {
		t.Errorf("NextUp = %g", v)
	}
	if v := Nearest(3730, "E96"); v != 3740 {
		t.Errorf("Nearest = %g", v)
	}
	if s := Eng(0.0000047, "H"); s != "4.7 µH" {
		t.Errorf("Eng = %q", s)
	}
	if s := Eng(650e3, "Hz"); s != "650 kHz" {
		t.Errorf("Eng = %q", s)
	}
}
