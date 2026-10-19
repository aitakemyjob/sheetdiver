package calc

import (
	"errors"
	"fmt"
	"math"
)

// BuckInput describes a synchronous or asynchronous step-down stage. All
// values are SI (volts, amps, hertz, seconds, henries, farads, ohms).
// Zero means "not given"; device limits are optional and enable checks.
type BuckInput struct {
	VinMin float64 `json:"vinMin" jsonschema:"minimum input voltage (V)"`
	VinMax float64 `json:"vinMax" jsonschema:"maximum input voltage (V)"`
	Vout   float64 `json:"vout" jsonschema:"output voltage (V)"`
	Iout   float64 `json:"iout" jsonschema:"maximum load current (A)"`
	Fsw    float64 `json:"fsw,omitempty" jsonschema:"switching frequency (Hz)"`
	// RippleRatio is the inductor ripple as a fraction of Iout (0.2–0.4).
	RippleRatio float64 `json:"rippleRatio,omitempty" jsonschema:"inductor ripple current / Iout, default 0.3"`
	Efficiency  float64 `json:"efficiency,omitempty" jsonschema:"expected efficiency 0..1, default 0.9"`
	VoutRipple  float64 `json:"voutRipple,omitempty" jsonschema:"allowed output ripple, peak-peak (V), default 1% of Vout"`
	VinRipple   float64 `json:"vinRipple,omitempty" jsonschema:"allowed input ripple, peak-peak (V), default 2% of VinMin"`
	L           float64 `json:"l,omitempty" jsonschema:"chosen inductance (H); default: next E12 value above the calculated one"`
	Vref        float64 `json:"vref,omitempty" jsonschema:"feedback reference voltage (V) for the divider"`
	RBottom     float64 `json:"rBottom,omitempty" jsonschema:"feedback bottom resistor (Ω), default 10 kΩ"`
	// Device limits from the datasheet.
	VinLimit  float64 `json:"vinLimit,omitempty" jsonschema:"device maximum operating input voltage (V)"`
	VinMinDev float64 `json:"vinMinDevice,omitempty" jsonschema:"device minimum operating input voltage (V)"`
	IoutLimit float64 `json:"ioutLimit,omitempty" jsonschema:"device rated output current (A)"`
	Ilim      float64 `json:"ilim,omitempty" jsonschema:"device minimum current limit (A)"`
	TonMin    float64 `json:"tonMin,omitempty" jsonschema:"device minimum on-time (s)"`
	ToffMin   float64 `json:"toffMin,omitempty" jsonschema:"device minimum off-time (s)"`
	LMin      float64 `json:"lMin,omitempty" jsonschema:"datasheet minimum recommended inductance (H)"`
	LMax      float64 `json:"lMax,omitempty" jsonschema:"datasheet maximum recommended inductance (H)"`
}

// Buck sizes a step-down converter: duty cycle, inductor, ripple, peak and
// RMS currents, capacitors and the feedback divider, and checks the
// design against the device's on/off-time, current and voltage limits.
func Buck(in BuckInput) (Result, error) {
	r := Result{Kind: "buck"}
	if in.VinMin == 0 {
		in.VinMin = in.VinMax
	}
	if in.VinMax == 0 {
		in.VinMax = in.VinMin
	}
	switch {
	case in.VinMax <= 0 || in.Vout <= 0 || in.Iout <= 0:
		return r, errors.New("buck needs vinMin/vinMax, vout and iout")
	case in.Fsw <= 0:
		return r, errors.New("buck needs the switching frequency fsw (Hz); it is usually in the electrical characteristics")
	case in.Vout >= in.VinMin:
		return r, fmt.Errorf("vout %.3g V must be below vinMin %.3g V for a buck", in.Vout, in.VinMin)
	}
	if in.VinMax < in.VinMin {
		in.VinMin, in.VinMax = in.VinMax, in.VinMin
	}
	eff := orDefault(in.Efficiency, 0.9)
	kr := orDefault(in.RippleRatio, 0.3)
	dvOut := orDefault(in.VoutRipple, 0.01*in.Vout)
	dvIn := orDefault(in.VinRipple, 0.02*in.VinMin)
	dMax := in.Vout / (in.VinMin * eff)
	dMin := in.Vout / in.VinMax
	r.add("Duty cycle at VinMin", "Dmax", math.Min(dMax, 1)*100, "%", "Vout / (VinMin·η)")
	r.add("Duty cycle at VinMax", "Dmin", dMin*100, "%", "Vout / VinMax")

	lCalc := (in.VinMax - in.Vout) * in.Vout / (in.VinMax * in.Fsw * kr * in.Iout)
	r.add("Inductance for target ripple", "Lcalc", lCalc, "H", "(VinMax−Vout)·Vout / (VinMax·fsw·k·Iout)")
	l := in.L
	if l == 0 {
		l = NextUp(lCalc, "E12")
		if in.LMin > 0 && l < in.LMin {
			l = in.LMin
		}
	}
	r.add("Chosen inductance", "L", l, "H", "next E12 value at or above Lcalc")
	dI := (in.VinMax - in.Vout) * in.Vout / (in.VinMax * in.Fsw * l)
	r.add("Inductor ripple (at VinMax)", "ΔIL", dI, "A", "(VinMax−Vout)·Vout / (VinMax·fsw·L)")
	ipk := in.Iout + dI/2
	irms := math.Sqrt(in.Iout*in.Iout + dI*dI/12)
	r.add("Peak inductor/switch current", "Ipk", ipk, "A", "Iout + ΔIL/2")
	r.add("Inductor RMS current", "IL,rms", irms, "A", "√(Iout² + ΔIL²/12)")
	r.add("Inductor saturation rating (min)", "Isat", ipk*1.2, "A", "Ipk + 20% margin (use the current limit if the output can be shorted)")

	cOut := dI / (8 * in.Fsw * dvOut)
	r.add("Output capacitance for ripple", "Cout", cOut, "F", "ΔIL / (8·fsw·ΔVout)")
	r.add("Output capacitor ESR (max)", "ESRmax", dvOut/dI, "Ω", "ΔVout / ΔIL")
	// Input RMS current peaks at D = 0.5.
	dWorst := math.Min(math.Max(dMin, math.Min(0.5, dMax)), dMax)
	icinRms := in.Iout * math.Sqrt(dWorst*(1-dWorst))
	r.add("Input capacitor RMS current", "ICin,rms", icinRms, "A", "Iout·√(D·(1−D)), worst D in range")
	cIn := in.Iout * dWorst * (1 - dWorst) / (in.Fsw * dvIn)
	r.add("Input capacitance for ripple", "Cin", cIn, "F", "Iout·D·(1−D) / (fsw·ΔVin)")

	pout := in.Vout * in.Iout
	r.add("Output power", "Pout", pout, "W", "Vout·Iout")
	r.add("Estimated loss", "Ploss", pout/eff-pout, "W", "Pout/η − Pout")
	iin := pout / (in.VinMin * eff)
	r.add("Input current at VinMin", "Iin", iin, "A", "Pout / (VinMin·η)")

	if in.Vref > 0 {
		rb := orDefault(in.RBottom, 10e3)
		rt := rb * (in.Vout/in.Vref - 1)
		rtStd := Nearest(rt, "E96")
		r.add("Feedback top resistor (exact)", "Rtop", rt, "Ω", "Rbottom·(Vout/Vref − 1)")
		r.add("Feedback top resistor (E96)", "Rtop,E96", rtStd, "Ω", "nearest E96")
		r.add("Feedback bottom resistor", "Rbottom", rb, "Ω", "")
		r.add("Resulting output voltage", "Vout,actual", in.Vref*(1+rtStd/rb), "V", "Vref·(1 + Rtop/Rbottom)")
	}

	ton := dMin / in.Fsw
	toff := (1 - math.Min(dMax, 1)) / in.Fsw
	r.add("On-time at VinMax", "ton", ton, "s", "Dmin / fsw")
	r.add("Off-time at VinMin", "toff", toff, "s", "(1 − Dmax) / fsw")
	if in.TonMin > 0 {
		if ton < in.TonMin {
			fmax := dMin / in.TonMin
			r.check(Error, "on-time %s at VinMax is below the minimum on-time %s; lower fsw to ≤ %s or raise Vout", Eng(ton, "s"), Eng(in.TonMin, "s"), Eng(fmax, "Hz"))
		} else if ton < in.TonMin*1.25 {
			r.check(Warn, "on-time %s at VinMax is within 25%% of the minimum on-time %s", Eng(ton, "s"), Eng(in.TonMin, "s"))
		} else {
			r.check(OK, "on-time %s ≥ minimum %s", Eng(ton, "s"), Eng(in.TonMin, "s"))
		}
	}
	if in.ToffMin > 0 {
		if dMax >= 1 || toff < in.ToffMin {
			vinNeeded := in.Vout / (eff * (1 - in.ToffMin*in.Fsw))
			r.check(Error, "off-time at VinMin is below the minimum off-time %s (dropout); VinMin must be ≥ about %s", Eng(in.ToffMin, "s"), Eng(vinNeeded, "V"))
		} else {
			r.check(OK, "off-time %s ≥ minimum %s", Eng(toff, "s"), Eng(in.ToffMin, "s"))
		}
	}
	if in.Ilim > 0 {
		if ipk > in.Ilim {
			r.check(Error, "peak current %s exceeds the minimum current limit %s; use a larger inductor or a higher-current part", Eng(ipk, "A"), Eng(in.Ilim, "A"))
		} else if ipk > in.Ilim*0.85 {
			r.check(Warn, "peak current %s is within 15%% of the minimum current limit %s", Eng(ipk, "A"), Eng(in.Ilim, "A"))
		} else {
			r.check(OK, "peak current %s is below the current limit %s", Eng(ipk, "A"), Eng(in.Ilim, "A"))
		}
	}
	if in.IoutLimit > 0 && in.Iout > in.IoutLimit {
		r.check(Error, "load %s exceeds the device rating %s", Eng(in.Iout, "A"), Eng(in.IoutLimit, "A"))
	}
	if in.VinLimit > 0 && in.VinMax > in.VinLimit {
		r.check(Error, "VinMax %s exceeds the device's maximum operating input %s", Eng(in.VinMax, "V"), Eng(in.VinLimit, "V"))
	}
	if in.VinMinDev > 0 && in.VinMin < in.VinMinDev {
		r.check(Error, "VinMin %s is below the device's minimum operating input %s", Eng(in.VinMin, "V"), Eng(in.VinMinDev, "V"))
	}
	if in.LMin > 0 && l < in.LMin || in.LMax > 0 && l > in.LMax {
		r.check(Warn, "L %s is outside the datasheet's recommended range %s–%s (loop stability)", Eng(l, "H"), Eng(in.LMin, "H"), Eng(in.LMax, "H"))
	}
	r.suggest("Inductor: %s, Isat ≥ %s, Irms ≥ %s, low DCR (losses ≈ Irms²·DCR)", Eng(l, "H"), Eng(ipk*1.2, "A"), Eng(irms, "A"))
	r.suggest("Output: ≥ %s ceramic (X5R/X7R, account for DC-bias derating, often 50%% at rated voltage)", Eng(cOut*2, "F"))
	r.suggest("Input: ≥ %s ceramic close to VIN/GND, rated for %s RMS", Eng(math.Max(cIn, 4.7e-6), "F"), Eng(icinRms, "A"))
	sortChecks(r.Checks)
	r.Summary = fmt.Sprintf("Buck %s–%s → %s @ %s, fsw %s: L = %s, ΔIL = %s, Ipk = %s, Cout ≥ %s",
		Eng(in.VinMin, "V"), Eng(in.VinMax, "V"), Eng(in.Vout, "V"), Eng(in.Iout, "A"), Eng(in.Fsw, "Hz"), Eng(l, "H"), Eng(dI, "A"), Eng(ipk, "A"), Eng(cOut, "F"))
	return r, nil
}

// BoostInput describes a step-up stage (SI units; zero = not given).
type BoostInput struct {
	VinMin      float64 `json:"vinMin" jsonschema:"minimum input voltage (V)"`
	VinMax      float64 `json:"vinMax" jsonschema:"maximum input voltage (V)"`
	Vout        float64 `json:"vout" jsonschema:"output voltage (V)"`
	Iout        float64 `json:"iout" jsonschema:"maximum load current (A)"`
	Fsw         float64 `json:"fsw,omitempty" jsonschema:"switching frequency (Hz)"`
	RippleRatio float64 `json:"rippleRatio,omitempty" jsonschema:"inductor ripple / average inductor current, default 0.3"`
	Efficiency  float64 `json:"efficiency,omitempty" jsonschema:"expected efficiency 0..1, default 0.85"`
	VoutRipple  float64 `json:"voutRipple,omitempty" jsonschema:"allowed output ripple, peak-peak (V), default 1% of Vout"`
	L           float64 `json:"l,omitempty" jsonschema:"chosen inductance (H)"`
	Vref        float64 `json:"vref,omitempty" jsonschema:"feedback reference voltage (V)"`
	RBottom     float64 `json:"rBottom,omitempty" jsonschema:"feedback bottom resistor (Ω), default 10 kΩ"`
	IlimSwitch  float64 `json:"ilimSwitch,omitempty" jsonschema:"device minimum switch current limit (A)"`
	VoutLimit   float64 `json:"voutLimit,omitempty" jsonschema:"device maximum output voltage (V)"`
	VinMinDev   float64 `json:"vinMinDevice,omitempty" jsonschema:"device minimum input voltage (V)"`
	Dmax        float64 `json:"dmax,omitempty" jsonschema:"device maximum duty cycle (0..1)"`
}

// Boost sizes a step-up converter: duty cycle, inductor, peak switch
// current (checked against the switch current limit, which sets the
// maximum load at low input), output capacitance and feedback divider.
func Boost(in BoostInput) (Result, error) {
	r := Result{Kind: "boost"}
	if in.VinMin == 0 {
		in.VinMin = in.VinMax
	}
	if in.VinMax == 0 {
		in.VinMax = in.VinMin
	}
	switch {
	case in.VinMin <= 0 || in.Vout <= 0 || in.Iout <= 0:
		return r, errors.New("boost needs vinMin/vinMax, vout and iout")
	case in.Fsw <= 0:
		return r, errors.New("boost needs the switching frequency fsw (Hz)")
	case in.Vout <= in.VinMax:
		return r, fmt.Errorf("vout %.3g V must be above vinMax %.3g V for a boost", in.Vout, in.VinMax)
	}
	eff := orDefault(in.Efficiency, 0.85)
	kr := orDefault(in.RippleRatio, 0.3)
	dvOut := orDefault(in.VoutRipple, 0.01*in.Vout)
	dMax := 1 - in.VinMin*eff/in.Vout
	dMin := 1 - in.VinMax*eff/in.Vout
	r.add("Duty cycle at VinMin", "Dmax", dMax*100, "%", "1 − VinMin·η/Vout")
	r.add("Duty cycle at VinMax", "Dmin", dMin*100, "%", "1 − VinMax·η/Vout")
	iin := in.Vout * in.Iout / (in.VinMin * eff)
	r.add("Average inductor (input) current at VinMin", "IL,avg", iin, "A", "Vout·Iout / (VinMin·η)")
	dIt := kr * iin
	lCalc := in.VinMin * dMax / (in.Fsw * dIt)
	r.add("Inductance for target ripple", "Lcalc", lCalc, "H", "VinMin·Dmax / (fsw·k·IL,avg)")
	l := in.L
	if l == 0 {
		l = NextUp(lCalc, "E12")
	}
	r.add("Chosen inductance", "L", l, "H", "next E12 value at or above Lcalc")
	dI := in.VinMin * dMax / (in.Fsw * l)
	r.add("Inductor ripple at VinMin", "ΔIL", dI, "A", "VinMin·Dmax / (fsw·L)")
	ipk := iin + dI/2
	r.add("Peak switch/inductor current", "Ipk", ipk, "A", "IL,avg + ΔIL/2")
	r.add("Inductor saturation rating (min)", "Isat", ipk*1.2, "A", "Ipk + 20%")
	cOut := in.Iout * dMax / (in.Fsw * dvOut)
	r.add("Output capacitance for ripple", "Cout", cOut, "F", "Iout·Dmax / (fsw·ΔVout)")
	r.add("Output capacitor RMS current", "ICout,rms", in.Iout*math.Sqrt(dMax/(1-dMax)), "A", "Iout·√(D/(1−D))")
	rhpz := in.Vout * (1 - dMax) * (1 - dMax) / (2 * math.Pi * l * in.Iout)
	r.add("Right-half-plane zero (min)", "fRHPZ", rhpz, "Hz", "Vout·(1−D)² / (2π·L·Iout); keep crossover below ~1/5 of this")
	r.add("Diode/rectifier average current", "ID", in.Iout, "A", "Iout")
	if in.IlimSwitch > 0 {
		ioutMax := (in.IlimSwitch - dI/2) * in.VinMin * eff / in.Vout
		r.add("Maximum load at VinMin (switch limit)", "Iout,max", ioutMax, "A", "(Ilim − ΔIL/2)·VinMin·η / Vout")
		switch {
		case ipk > in.IlimSwitch:
			r.check(Error, "peak switch current %s exceeds the switch current limit %s: maximum load at VinMin is about %s", Eng(ipk, "A"), Eng(in.IlimSwitch, "A"), Eng(ioutMax, "A"))
		case ipk > in.IlimSwitch*0.85:
			r.check(Warn, "peak switch current %s is within 15%% of the switch current limit %s", Eng(ipk, "A"), Eng(in.IlimSwitch, "A"))
		default:
			r.check(OK, "peak switch current %s is below the limit %s", Eng(ipk, "A"), Eng(in.IlimSwitch, "A"))
		}
	}
	if in.Dmax > 0 && dMax > in.Dmax {
		r.check(Error, "required duty %.0f%% exceeds the device's maximum duty %.0f%%", dMax*100, in.Dmax*100)
	}
	if in.VoutLimit > 0 && in.Vout > in.VoutLimit {
		r.check(Error, "Vout %s exceeds the device's maximum %s", Eng(in.Vout, "V"), Eng(in.VoutLimit, "V"))
	}
	if in.VinMinDev > 0 && in.VinMin < in.VinMinDev {
		r.check(Error, "VinMin %s is below the device's minimum input %s", Eng(in.VinMin, "V"), Eng(in.VinMinDev, "V"))
	}
	if in.Vref > 0 {
		rb := orDefault(in.RBottom, 10e3)
		rt := rb * (in.Vout/in.Vref - 1)
		rtStd := Nearest(rt, "E96")
		r.add("Feedback top resistor (E96)", "Rtop,E96", rtStd, "Ω", "Rbottom·(Vout/Vref − 1), nearest E96")
		r.add("Feedback bottom resistor", "Rbottom", rb, "Ω", "")
		r.add("Resulting output voltage", "Vout,actual", in.Vref*(1+rtStd/rb), "V", "Vref·(1 + Rtop/Rbottom)")
	}
	r.suggest("Inductor: %s, Isat ≥ %s (≥ the switch current limit if overload is possible)", Eng(l, "H"), Eng(math.Max(ipk*1.2, in.IlimSwitch), "A"))
	r.suggest("Output: ≥ %s effective ceramic after DC-bias derating at %s", Eng(cOut, "F"), Eng(in.Vout, "V"))
	sortChecks(r.Checks)
	r.Summary = fmt.Sprintf("Boost %s–%s → %s @ %s, fsw %s: L = %s, Ipk = %s, Cout ≥ %s",
		Eng(in.VinMin, "V"), Eng(in.VinMax, "V"), Eng(in.Vout, "V"), Eng(in.Iout, "A"), Eng(in.Fsw, "Hz"), Eng(l, "H"), Eng(ipk, "A"), Eng(cOut, "F"))
	return r, nil
}

// LDOInput describes a linear regulator's operating point.
type LDOInput struct {
	VinMin   float64 `json:"vinMin" jsonschema:"minimum input voltage (V)"`
	VinMax   float64 `json:"vinMax" jsonschema:"maximum input voltage (V)"`
	Vout     float64 `json:"vout" jsonschema:"output voltage (V)"`
	Iout     float64 `json:"iout" jsonschema:"load current (A)"`
	Iq       float64 `json:"iq,omitempty" jsonschema:"quiescent/ground current (A)"`
	Dropout  float64 `json:"dropout,omitempty" jsonschema:"dropout voltage at this load (V)"`
	RthJA    float64 `json:"rthJA,omitempty" jsonschema:"junction-to-ambient thermal resistance (°C/W)"`
	Ta       float64 `json:"ta,omitempty" jsonschema:"ambient temperature (°C), default 25"`
	TjMax    float64 `json:"tjMax,omitempty" jsonschema:"maximum junction temperature (°C), default 125"`
	IoutMax  float64 `json:"ioutMax,omitempty" jsonschema:"device rated output current (A)"`
	VinLimit float64 `json:"vinLimit,omitempty" jsonschema:"device maximum input voltage (V)"`
}

// LDO checks a linear regulator's headroom and thermals.
func LDO(in LDOInput) (Result, error) {
	r := Result{Kind: "ldo"}
	if in.VinMax == 0 {
		in.VinMax = in.VinMin
	}
	if in.VinMin == 0 {
		in.VinMin = in.VinMax
	}
	if in.VinMax <= 0 || in.Vout <= 0 || in.Iout <= 0 {
		return r, errors.New("ldo needs vinMin/vinMax, vout and iout")
	}
	ta := orDefault(in.Ta, 25)
	tjMax := orDefault(in.TjMax, 125)
	p := (in.VinMax-in.Vout)*in.Iout + in.VinMax*in.Iq
	r.add("Dissipation at VinMax", "Pd", p, "W", "(VinMax−Vout)·Iout + VinMax·Iq")
	r.add("Efficiency at VinMin", "η", in.Vout*in.Iout/(in.VinMin*(in.Iout+in.Iq))*100, "%", "Vout·Iout / (Vin·(Iout+Iq))")
	head := in.VinMin - in.Vout
	r.add("Headroom at VinMin", "Vhead", head, "V", "VinMin − Vout")
	if in.Dropout > 0 {
		if head < in.Dropout {
			r.check(Error, "headroom %s at VinMin is below the dropout %s: the output will sag", Eng(head, "V"), Eng(in.Dropout, "V"))
		} else if head < in.Dropout*1.3 {
			r.check(Warn, "headroom %s is close to the dropout %s (PSRR is poor near dropout)", Eng(head, "V"), Eng(in.Dropout, "V"))
		} else {
			r.check(OK, "headroom %s > dropout %s", Eng(head, "V"), Eng(in.Dropout, "V"))
		}
	}
	if in.RthJA > 0 {
		tj := ta + p*in.RthJA
		r.add("Junction temperature", "Tj", tj, "°C", "Ta + Pd·RθJA")
		r.add("Maximum dissipation", "Pd,max", (tjMax-ta)/in.RthJA, "W", "(TjMax − Ta) / RθJA")
		switch {
		case tj > tjMax:
			r.check(Error, "junction reaches %.0f °C, above %.0f °C: use a switcher, a bigger package or a series resistor to share the drop", tj, tjMax)
		case tj > tjMax-20:
			r.check(Warn, "junction reaches %.0f °C, within 20 °C of the %.0f °C limit", tj, tjMax)
		default:
			r.check(OK, "junction %.0f °C at %.0f °C ambient", tj, ta)
		}
	}
	if in.IoutMax > 0 && in.Iout > in.IoutMax {
		r.check(Error, "load %s exceeds the device rating %s", Eng(in.Iout, "A"), Eng(in.IoutMax, "A"))
	}
	if in.VinLimit > 0 && in.VinMax > in.VinLimit {
		r.check(Error, "VinMax %s exceeds the device's maximum input %s", Eng(in.VinMax, "V"), Eng(in.VinLimit, "V"))
	}
	sortChecks(r.Checks)
	r.Summary = fmt.Sprintf("LDO %s → %s @ %s: Pd = %s", Eng(in.VinMax, "V"), Eng(in.Vout, "V"), Eng(in.Iout, "A"), Eng(p, "W"))
	return r, nil
}
