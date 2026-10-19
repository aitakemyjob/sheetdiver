package calc

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// GateDriveInput describes driving a MOSFET gate (SI units; zero = not
// given).
type GateDriveInput struct {
	// Channel is "n" or "p".
	Channel string  `json:"channel,omitempty" jsonschema:"n or p, default n"`
	Vdrive  float64 `json:"vdrive" jsonschema:"gate drive voltage swing (V), e.g. 3.3 from an MCU or 12 from a driver"`
	Rdriver float64 `json:"rdriver,omitempty" jsonschema:"driver output resistance (Ω); MCU pins are roughly 25–50 Ω"`
	Idrive  float64 `json:"idrive,omitempty" jsonschema:"driver peak current (A), used if rdriver is not given"`
	Rg      float64 `json:"rg,omitempty" jsonschema:"chosen external gate resistor (Ω); default: computed for tswitch"`
	Tswitch float64 `json:"tswitch,omitempty" jsonschema:"target switching (Miller) time (s), default 50 ns"`
	// Datasheet values.
	VgsTh    float64 `json:"vgsTh,omitempty" jsonschema:"gate threshold voltage, max (V)"`
	VgsMax   float64 `json:"vgsMax,omitempty" jsonschema:"gate-source voltage rating (V)"`
	VgsRated float64 `json:"vgsRated,omitempty" jsonschema:"Vgs at which RDS(on) is specified (V), e.g. 4.5 or 10"`
	Vplateau float64 `json:"vplateau,omitempty" jsonschema:"Miller plateau voltage (V); default VgsTh + 1 V"`
	Qg       float64 `json:"qg,omitempty" jsonschema:"total gate charge (C)"`
	Qgd      float64 `json:"qgd,omitempty" jsonschema:"gate-drain charge (C)"`
	Qgs      float64 `json:"qgs,omitempty" jsonschema:"gate-source charge (C)"`
	Ciss     float64 `json:"ciss,omitempty" jsonschema:"input capacitance (F)"`
	RgInt    float64 `json:"rgInternal,omitempty" jsonschema:"internal gate resistance (Ω)"`
	Rdson    float64 `json:"rdson,omitempty" jsonschema:"on-resistance at the drive voltage (Ω)"`
	Vds      float64 `json:"vds,omitempty" jsonschema:"drain-source voltage when off (V)"`
	VdsMax   float64 `json:"vdsMax,omitempty" jsonschema:"drain-source rating (V)"`
	Iload    float64 `json:"iload,omitempty" jsonschema:"load current (A)"`
	IdMax    float64 `json:"idMax,omitempty" jsonschema:"continuous drain current rating (A)"`
	Fsw      float64 `json:"fsw,omitempty" jsonschema:"switching frequency (Hz); 0 for a static switch"`
	RthJA    float64 `json:"rthJA,omitempty" jsonschema:"junction-to-ambient thermal resistance (°C/W)"`
	Ta       float64 `json:"ta,omitempty" jsonschema:"ambient (°C), default 25"`
	TjMax    float64 `json:"tjMax,omitempty" jsonschema:"maximum junction temperature (°C), default 150"`
}

// GateDrive sizes the gate resistor and estimates switching times and
// losses, and checks that the drive voltage fully enhances the MOSFET.
func GateDrive(in GateDriveInput) (Result, error) {
	r := Result{Kind: "gate_drive"}
	if in.Vdrive <= 0 {
		return r, errors.New("gate_drive needs vdrive (the gate drive voltage)")
	}
	vpl := in.Vplateau
	if vpl == 0 && in.VgsTh > 0 {
		vpl = in.VgsTh + 1
	}
	if in.VgsRated > 0 {
		if in.Vdrive < in.VgsRated {
			r.check(Warn, "drive %s is below the %s at which RDS(on) is specified; on-resistance will be higher than the datasheet value (check the RDS(on) vs VGS graph)", Eng(in.Vdrive, "V"), Eng(in.VgsRated, "V"))
		} else {
			r.check(OK, "drive %s ≥ the RDS(on) rating voltage %s", Eng(in.Vdrive, "V"), Eng(in.VgsRated, "V"))
		}
	}
	if in.VgsTh > 0 && in.Vdrive < 2*in.VgsTh {
		r.check(Warn, "drive %s is less than twice the maximum threshold %s: the FET may be only partly on", Eng(in.Vdrive, "V"), Eng(in.VgsTh, "V"))
	}
	if in.VgsMax > 0 {
		if in.Vdrive > in.VgsMax {
			r.check(Error, "drive %s exceeds the gate rating ±%s: add a divider or zener clamp", Eng(in.Vdrive, "V"), Eng(in.VgsMax, "V"))
		} else if in.Vdrive > 0.8*in.VgsMax {
			r.check(Warn, "drive %s is above 80%% of the gate rating %s", Eng(in.Vdrive, "V"), Eng(in.VgsMax, "V"))
		}
	}
	rdrv := in.Rdriver
	if rdrv == 0 && in.Idrive > 0 {
		rdrv = in.Vdrive / in.Idrive
	}
	if rdrv == 0 {
		rdrv = 2
	}
	tsw := orDefault(in.Tswitch, 50e-9)
	rg := in.Rg
	if rg == 0 && in.Qgd > 0 && vpl > 0 && in.Vdrive > vpl {
		ig := in.Qgd / tsw
		rgCalc := (in.Vdrive-vpl)/ig - rdrv - in.RgInt
		r.add("Gate current for target Miller time", "Ig", ig, "A", "Qgd / tsw")
		if rgCalc < 0 {
			r.check(Warn, "the driver alone can't reach %s Miller time (needs %s); switching will be slower", Eng(tsw, "s"), Eng(ig, "A"))
			rgCalc = 0
		}
		rg = Nearest(math.Max(rgCalc, 1), "E24")
		r.add("Gate resistor (E24)", "Rg", rg, "Ω", "(Vdrive − Vplateau)/Ig − Rdriver − Rg,int")
	}
	rtot := rg + rdrv + in.RgInt
	r.add("Total gate loop resistance", "Rtotal", rtot, "Ω", "Rg + Rdriver + Rg,int")
	var tr, tf float64
	if in.Qgd > 0 && vpl > 0 && in.Vdrive > vpl {
		tr = in.Qgd * rtot / (in.Vdrive - vpl)
		tf = in.Qgd * rtot / vpl
		r.add("Turn-on Miller time", "tr", tr, "s", "Qgd·Rtotal / (Vdrive − Vplateau)")
		r.add("Turn-off Miller time", "tf", tf, "s", "Qgd·Rtotal / Vplateau")
	}
	if in.Qg > 0 {
		r.add("Gate drive power", "Pgate", in.Qg*in.Vdrive*in.Fsw, "W", "Qg·Vdrive·fsw")
		r.add("Average gate current", "Ig,avg", in.Qg*in.Fsw, "A", "Qg·fsw")
		r.add("Total gate charge time", "tQg", in.Qg*rtot/in.Vdrive, "s", "≈ Qg·Rtotal / Vdrive")
	} else if in.Ciss > 0 {
		r.add("Gate RC time constant", "τ", in.Ciss*rtot, "s", "Ciss·Rtotal (no Qg given)")
	}
	r.add("Gate pull-down resistor (suggested)", "Rpd", 100e3, "Ω", "10–100 kΩ gate–source keeps the FET off while the driver is high-impedance (reset, boot)")
	if in.Rdson > 0 && in.Iload > 0 {
		pc := in.Iload * in.Iload * in.Rdson
		r.add("Conduction loss", "Pcond", pc, "W", "Iload²·RDS(on) (×1.5 hot is typical)")
		r.add("Conduction loss, hot", "Pcond,hot", pc*1.5, "W", "×1.5 for RDS(on) at ~125 °C")
		ps := 0.0
		if in.Fsw > 0 && in.Vds > 0 && tr+tf > 0 {
			ps = 0.5 * in.Vds * in.Iload * (tr + tf) * in.Fsw
			r.add("Switching loss", "Psw", ps, "W", "½·Vds·Iload·(tr+tf)·fsw")
		}
		total := pc*1.5 + ps
		r.add("Total loss", "Ptotal", total, "W", "Pcond,hot + Psw")
		if in.RthJA > 0 {
			ta := orDefault(in.Ta, 25)
			tj := ta + total*in.RthJA
			tjMax := orDefault(in.TjMax, 150)
			r.add("Junction temperature", "Tj", tj, "°C", "Ta + Ptotal·RθJA")
			switch {
			case tj > tjMax:
				r.check(Error, "junction reaches %.0f °C, above %.0f °C", tj, tjMax)
			case tj > tjMax-25:
				r.check(Warn, "junction reaches %.0f °C (RθJA depends heavily on copper area)", tj)
			default:
				r.check(OK, "junction %.0f °C", tj)
			}
		}
	}
	if in.IdMax > 0 && in.Iload > in.IdMax {
		r.check(Error, "load %s exceeds the continuous drain rating %s", Eng(in.Iload, "A"), Eng(in.IdMax, "A"))
	}
	if in.VdsMax > 0 && in.Vds > 0.8*in.VdsMax {
		r.check(Warn, "Vds %s is above 80%% of the %s rating; leave margin for ringing", Eng(in.Vds, "V"), Eng(in.VdsMax, "V"))
	}
	sortChecks(r.Checks)
	r.Summary = fmt.Sprintf("Gate drive %s: Rg = %s, tr ≈ %s, tf ≈ %s", Eng(in.Vdrive, "V"), Eng(rg, "Ω"), Eng(tr, "s"), Eng(tf, "s"))
	return r, nil
}

// GateDividerInput describes biasing a gate from a supply that may exceed
// the gate rating: a high-side P-channel switch (gate pulled toward
// ground through R1, R2 from gate to source), or an N-channel gate fed
// from a higher rail.
type GateDividerInput struct {
	VinMin    float64 `json:"vinMin" jsonschema:"minimum supply across the divider (V)"`
	VinMax    float64 `json:"vinMax" jsonschema:"maximum supply across the divider (V)"`
	VgsOn     float64 `json:"vgsOn" jsonschema:"gate voltage needed for full enhancement (V), e.g. the RDS(on) rating voltage"`
	VgsMax    float64 `json:"vgsMax" jsonschema:"gate-source rating (V)"`
	Margin    float64 `json:"margin,omitempty" jsonschema:"fraction of VgsMax to stay under, default 0.8"`
	Rtotal    float64 `json:"rtotal,omitempty" jsonschema:"target R1+R2 (Ω), default 47 kΩ (sets divider current and turn-off speed)"`
	Ciss      float64 `json:"ciss,omitempty" jsonschema:"MOSFET input capacitance (F) for timing"`
	Series    string  `json:"series,omitempty" jsonschema:"resistor series, default E24"`
	Vpulldown float64 `json:"vpulldown,omitempty" jsonschema:"voltage lost in the pull-down transistor (V), default 0.2"`
}

// GateDivider picks R1 (gate to driver/ground) and R2 (gate to source) so
// Vgs = Vin·R2/(R1+R2) stays above VgsOn at VinMin and under the safe
// fraction of VgsMax at VinMax, preferring the middle of the window.
func GateDivider(in GateDividerInput) (Result, error) {
	r := Result{Kind: "gate_divider"}
	if in.VinMax == 0 {
		in.VinMax = in.VinMin
	}
	if in.VinMin == 0 {
		in.VinMin = in.VinMax
	}
	if in.VinMin <= 0 || in.VgsOn <= 0 || in.VgsMax <= 0 {
		return r, errors.New("gate_divider needs vinMin/vinMax, vgsOn and vgsMax")
	}
	margin := orDefault(in.Margin, 0.8)
	vpd := in.Vpulldown
	if vpd == 0 {
		vpd = 0.2
	}
	vMin, vMax := in.VinMin-vpd, in.VinMax
	lo := in.VgsOn / vMin           // smallest acceptable ratio R2/(R1+R2)
	hi := margin * in.VgsMax / vMax // largest
	r.add("Minimum divider ratio", "kmin", lo, "", "VgsOn / (VinMin − Vpd)")
	r.add("Maximum divider ratio", "kmax", hi, "", "margin·VgsMax / VinMax")
	if in.VinMax <= margin*in.VgsMax {
		r.check(OK, "VinMax %s is within the gate rating: R2 can be a plain pull-up and R1 just limits current", Eng(in.VinMax, "V"))
	}
	if lo > hi {
		z := in.VgsOn + (margin*in.VgsMax-in.VgsOn)/2
		r.check(Error, "no divider works across %s–%s: the window is too wide for VgsOn %s and %.0f%% of VgsMax %s. Use a zener (≈%s) from gate to source with a series resistor instead",
			Eng(in.VinMin, "V"), Eng(in.VinMax, "V"), Eng(in.VgsOn, "V"), margin*100, Eng(in.VgsMax, "V"), Eng(z, "V"))
		r.suggest("Zener clamp: %s zener gate–source, series resistor sized for ~1 mA at VinMax: %s", Eng(z, "V"), Eng(Nearest((in.VinMax-z)/1e-3, "E24"), "Ω"))
		sortChecks(r.Checks)
		r.Summary = "No resistor divider satisfies both limits; use a zener clamp."
		return r, nil
	}
	target := math.Min(1, (lo+hi)/2)
	rt := orDefault(in.Rtotal, 47e3)
	series := in.Series
	if series == "" {
		series = "E24"
	}
	type pair struct{ r1, r2, k, score float64 }
	var cands []pair
	d := int(math.Floor(math.Log10(rt)))
	vals := seriesValues(series, d-2, d+1)
	for _, r2 := range vals {
		for _, r1 := range vals {
			k := r2 / (r1 + r2)
			if k < lo || k > hi {
				continue
			}
			// Prefer the window's middle and a total near the target.
			score := math.Abs(k-target)/(hi-lo+1e-9) + 0.3*math.Abs(math.Log((r1+r2)/rt))
			cands = append(cands, pair{r1, r2, k, score})
		}
	}
	if target >= 1 || len(cands) == 0 {
		r.check(OK, "the gate can be pulled fully to the supply: use R2 = %s gate–source and R1 ≈ 1 kΩ in series with the pull-down", Eng(rt, "Ω"))
		sortChecks(r.Checks)
		r.Summary = "No divider needed."
		return r, nil
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].score < cands[j].score })
	best := cands[0]
	r.add("R1 (gate to pull-down)", "R1", best.r1, "Ω", series)
	r.add("R2 (gate to source)", "R2", best.r2, "Ω", series)
	r.add("Divider ratio", "k", best.k, "", "R2 / (R1 + R2)")
	r.add("Vgs at VinMin", "Vgs,min", best.k*vMin, "V", "k·(VinMin − Vpd)")
	r.add("Vgs at VinMax", "Vgs,max", best.k*vMax, "V", "k·VinMax")
	r.add("Divider current at VinMax", "Idiv", vMax/(best.r1+best.r2), "A", "VinMax / (R1+R2)")
	r.add("Divider power at VinMax", "Pdiv", vMax*vMax/(best.r1+best.r2), "W", "VinMax² / (R1+R2)")
	if in.Ciss > 0 {
		par := best.r1 * best.r2 / (best.r1 + best.r2)
		r.add("Turn-on time constant", "τon", in.Ciss*par, "s", "Ciss·(R1‖R2)")
		r.add("Turn-off time constant", "τoff", in.Ciss*best.r2, "s", "Ciss·R2 (gate discharges through R2 only)")
		if in.Ciss*best.r2*5 > 1e-3 {
			r.check(Warn, "turn-off takes about %s (5τ); slow edges heat the FET in linear region; lower R2 if it switches often", Eng(5*in.Ciss*best.r2, "s"))
		}
	}
	r.check(OK, "Vgs stays between %s and %s across the supply range", Eng(best.k*vMin, "V"), Eng(best.k*vMax, "V"))
	for i := 1; i < len(cands) && i < 4; i++ {
		c := cands[i]
		r.suggest("Alternative: R1 = %s, R2 = %s (Vgs %s–%s)", Eng(c.r1, "Ω"), Eng(c.r2, "Ω"), Eng(c.k*vMin, "V"), Eng(c.k*vMax, "V"))
	}
	sortChecks(r.Checks)
	r.Summary = fmt.Sprintf("Gate divider R1 = %s, R2 = %s: Vgs %s–%s over %s–%s", Eng(best.r1, "Ω"), Eng(best.r2, "Ω"), Eng(best.k*vMin, "V"), Eng(best.k*vMax, "V"), Eng(in.VinMin, "V"), Eng(in.VinMax, "V"))
	return r, nil
}

// DividerInput describes a resistor divider (feedback network, ADC
// scaling, threshold setting).
type DividerInput struct {
	Vin     float64 `json:"vin" jsonschema:"voltage across the divider (V)"`
	Vout    float64 `json:"vout" jsonschema:"wanted tap voltage (V)"`
	Rtotal  float64 `json:"rtotal,omitempty" jsonschema:"target Rtop+Rbottom (Ω), default 100 kΩ"`
	Rbottom float64 `json:"rbottom,omitempty" jsonschema:"fix the bottom resistor (Ω) instead of searching"`
	Series  string  `json:"series,omitempty" jsonschema:"E24, E48 or E96 (default E96)"`
}

// Divider finds standard-value resistor pairs whose ratio gives Vout.
func Divider(in DividerInput) (Result, error) {
	r := Result{Kind: "divider"}
	if in.Vin <= 0 || in.Vout <= 0 || in.Vout >= in.Vin {
		return r, errors.New("divider needs vin > vout > 0")
	}
	series := in.Series
	if series == "" {
		series = "E96"
	}
	rt := orDefault(in.Rtotal, 100e3)
	want := in.Vout / in.Vin
	type pair struct{ top, bot, v, err float64 }
	var cands []pair
	d := int(math.Floor(math.Log10(rt)))
	vals := seriesValues(series, d-2, d+1)
	bots := vals
	if in.Rbottom > 0 {
		bots = []float64{in.Rbottom}
	}
	for _, b := range bots {
		for _, t := range vals {
			v := in.Vin * b / (t + b)
			e := math.Abs(v-in.Vout) / in.Vout
			// Penalize totals far from the target.
			e += 0.002 * math.Abs(math.Log((t+b)/rt))
			cands = append(cands, pair{t, b, v, e})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].err < cands[j].err })
	if len(cands) == 0 {
		return r, errors.New("no divider found")
	}
	best := cands[0]
	r.add("Top resistor", "Rtop", best.top, "Ω", series)
	r.add("Bottom resistor", "Rbottom", best.bot, "Ω", series)
	r.add("Tap voltage", "Vtap", best.v, "V", "Vin·Rbottom / (Rtop + Rbottom)")
	r.add("Error", "err", (best.v-in.Vout)/in.Vout*100, "%", "")
	r.add("Divider current", "I", in.Vin/(best.top+best.bot), "A", "Vin / (Rtop + Rbottom)")
	r.add("Ideal ratio", "k", want, "", "Vout / Vin")
	for i := 1; i < len(cands) && i < 5; i++ {
		c := cands[i]
		r.suggest("Rtop = %s, Rbottom = %s → %s", Eng(c.top, "Ω"), Eng(c.bot, "Ω"), Eng(c.v, "V"))
	}
	r.Summary = fmt.Sprintf("Divider %s → %s: Rtop = %s, Rbottom = %s (%s)", Eng(in.Vin, "V"), Eng(in.Vout, "V"), Eng(best.top, "Ω"), Eng(best.bot, "Ω"), Eng(best.v, "V"))
	return r, nil
}
