package core

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Calculator kinds.
var CalcKinds = map[string]string{
	"buck":         "Step-down converter: duty, inductor, ripple, peak/RMS currents, capacitors, feedback divider, on/off-time and current-limit checks",
	"boost":        "Step-up converter: duty, inductor, peak switch current vs limit, output capacitance, RHP zero, feedback divider",
	"ldo":          "Linear regulator: headroom vs dropout, dissipation and junction temperature",
	"gate_drive":   "MOSFET gate drive: gate resistor, Miller times, gate power, conduction/switching loss, junction temperature",
	"gate_divider": "Gate bias divider for a high-side P-FET (or N-FET from a higher rail): R1/R2 keeping Vgs between the on voltage and a safe fraction of VGS max, or a zener recommendation",
	"divider":      "Resistor divider from standard values (E24/E48/E96)",
}

// CalcRequest runs a calculator. Inputs are the calculator's JSON fields
// in SI units; anything not given is filled from the part's datasheet
// (and, with a project, from the part's rails).
type CalcRequest struct {
	Kind    string          `json:"kind"`
	Part    string          `json:"part,omitempty"`
	Project string          `json:"project,omitempty"`
	Inputs  json.RawMessage `json:"inputs,omitempty"`
	Save    bool            `json:"save,omitempty"`
	Title   string          `json:"title,omitempty"`
}

// Filled records an input taken from the datasheet or project.
type Filled struct {
	Field string  `json:"field"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	From  string  `json:"from"`
	Page  int     `json:"page,omitempty"`
	Param int64   `json:"paramId,omitempty"`
}

// CalcResponse is a calculator result with the inputs actually used.
type CalcResponse struct {
	Kind    string         `json:"kind"`
	Part    string         `json:"part,omitempty"`
	Project string         `json:"project,omitempty"`
	Inputs  map[string]any `json:"inputs"`
	Filled  []Filled       `json:"filled"`
	Missing []string       `json:"missing,omitempty"`
	Result  calc.Result    `json:"result"`
	SavedID int64          `json:"savedId,omitempty"`
}

// source picks one statistic of a keyed parameter for an input field.
type source struct {
	field    string
	key      string
	stats    []string // min, typ, max in order of preference
	sections []string // preferred sections, in order; empty = any
	unit     string   // SI unit the field expects
	scale    float64  // multiply after SI conversion (e.g. % → fraction)
}

// fills maps calculator inputs to datasheet parameters.
var fills = map[string][]source{
	"buck": {
		{field: "fsw", key: "fsw", stats: []string{"typ", "min", "max"}, unit: "Hz"},
		{field: "vref", key: "vref", stats: []string{"typ", "min", "max"}, sections: []string{extract.KindElectrical}, unit: "V"},
		{field: "vinLimit", key: "vin", stats: []string{"max"}, sections: []string{extract.KindRecommended, extract.KindElectrical}, unit: "V"},
		{field: "vinMinDevice", key: "vin", stats: []string{"min"}, sections: []string{extract.KindRecommended, extract.KindElectrical}, unit: "V"},
		{field: "ioutLimit", key: "iout", stats: []string{"max", "typ"}, sections: []string{extract.KindRecommended, extract.KindElectrical}, unit: "A"},
		{field: "ilim", key: "ilim", stats: []string{"min", "typ"}, unit: "A"},
		{field: "tonMin", key: "ton_min", stats: []string{"max", "typ"}, unit: "s"},
		{field: "toffMin", key: "toff_min", stats: []string{"max", "typ"}, unit: "s"},
	},
	"boost": {
		{field: "fsw", key: "fsw", stats: []string{"typ", "min", "max"}, unit: "Hz"},
		{field: "vref", key: "vref", stats: []string{"typ", "min", "max"}, sections: []string{extract.KindElectrical}, unit: "V"},
		{field: "ilimSwitch", key: "ilim", stats: []string{"min", "typ"}, unit: "A"},
		{field: "voutLimit", key: "vout", stats: []string{"max"}, sections: []string{extract.KindRecommended, extract.KindElectrical}, unit: "V"},
		{field: "vinMinDevice", key: "vin", stats: []string{"min"}, sections: []string{extract.KindRecommended, extract.KindElectrical}, unit: "V"},
		{field: "dmax", key: "dmax", stats: []string{"min", "typ"}, unit: "%", scale: 0.01},
	},
	"ldo": {
		{field: "iq", key: "iq", stats: []string{"typ", "max"}, unit: "A"},
		{field: "dropout", key: "vdropout", stats: []string{"max", "typ"}, unit: "V"},
		{field: "rthJA", key: "rth_ja", stats: []string{"typ", "max"}, unit: "°C/W"},
		{field: "ioutMax", key: "iout", stats: []string{"max", "typ"}, sections: []string{extract.KindRecommended, extract.KindElectrical}, unit: "A"},
		{field: "vinLimit", key: "vin", stats: []string{"max"}, sections: []string{extract.KindRecommended, extract.KindElectrical}, unit: "V"},
		{field: "tjMax", key: "tj", stats: []string{"max"}, sections: []string{extract.KindRecommended, extract.KindAbsMax}, unit: "°C"},
	},
	"gate_drive": {
		{field: "vgsTh", key: "vgs_th", stats: []string{"max", "typ"}, unit: "V"},
		{field: "vgsMax", key: "vgs", stats: []string{"max"}, sections: []string{extract.KindAbsMax}, unit: "V"},
		{field: "qg", key: "qg", stats: []string{"typ", "max"}, unit: "C"},
		{field: "qgd", key: "qgd", stats: []string{"typ", "max"}, unit: "C"},
		{field: "qgs", key: "qgs", stats: []string{"typ", "max"}, unit: "C"},
		{field: "ciss", key: "ciss", stats: []string{"typ", "max"}, unit: "F"},
		{field: "rgInternal", key: "rg", stats: []string{"typ", "max"}, unit: "Ω"},
		{field: "vplateau", key: "vplateau", stats: []string{"typ"}, unit: "V"},
		{field: "rdson", key: "rdson", stats: []string{"max", "typ"}, unit: "Ω"},
		{field: "vdsMax", key: "vds", stats: []string{"max", "min"}, sections: []string{extract.KindAbsMax, extract.KindElectrical}, unit: "V"},
		{field: "idMax", key: "id", stats: []string{"max", "typ"}, sections: []string{extract.KindAbsMax}, unit: "A"},
		{field: "rthJA", key: "rth_ja", stats: []string{"typ", "max"}, unit: "°C/W"},
		{field: "tjMax", key: "tj", stats: []string{"max"}, unit: "°C"},
	},
	"gate_divider": {
		{field: "vgsMax", key: "vgs", stats: []string{"max"}, sections: []string{extract.KindAbsMax}, unit: "V"},
		{field: "ciss", key: "ciss", stats: []string{"typ", "max"}, unit: "F"},
	},
}

// vgsCondRe finds "VGS = 4.5 V" in an RDS(on) row's conditions.
var vgsCondRe = regexp.MustCompile(`(?i)V\s*_?\(?GS\)?\s*=\s*[-–−]?\s*(\d+(?:\.\d+)?)\s*V`)

// Calc runs a calculator, filling missing inputs from the datasheet and
// project, and optionally saves the run.
func (s *Service) Calc(ctx context.Context, req CalcRequest) (CalcResponse, error) {
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	resp := CalcResponse{Kind: kind, Filled: []Filled{}}
	if _, ok := CalcKinds[kind]; !ok {
		return resp, fmt.Errorf("unknown calculator %q; one of: buck, boost, ldo, gate_drive, gate_divider, divider", req.Kind)
	}
	inputs := map[string]any{}
	if len(req.Inputs) > 0 && string(req.Inputs) != "null" {
		if err := json.Unmarshal(req.Inputs, &inputs); err != nil {
			return resp, fmt.Errorf("inputs must be a JSON object of numbers: %w", err)
		}
	}
	given := func(field string) bool {
		v, ok := inputs[field]
		if !ok {
			return false
		}
		f, isNum := v.(float64)
		return !isNum || f != 0
	}
	set := func(f Filled) {
		inputs[f.Field] = f.Value
		resp.Filled = append(resp.Filled, f)
	}
	var partID int64
	var mpn string
	if req.Part != "" {
		r, d, err := s.partDatasheet(ctx, req.Part)
		if err != nil {
			return resp, err
		}
		partID, mpn = r.Part.ID, r.Part.MPN
		resp.Part = mpn
		params, err := s.Lib.Parameters(ctx, d.ID, library.ParamFilter{MPN: mpn})
		if err != nil {
			return resp, err
		}
		for _, src := range fills[kind] {
			if given(src.field) {
				continue
			}
			if f, ok := pickParam(params, src, mpn); ok {
				set(f)
			} else {
				resp.Missing = append(resp.Missing, fmt.Sprintf("%s (parameter key %q)", src.field, src.key))
			}
		}
		if kind == "gate_drive" || kind == "gate_divider" {
			field := "vgsRated"
			if kind == "gate_divider" {
				field = "vgsOn"
			}
			if !given(field) {
				for _, p := range params {
					if p.Key == "rdson" {
						if m := vgsCondRe.FindStringSubmatch(p.Conditions + " " + p.Name); m != nil {
							v, _ := strconv.ParseFloat(m[1], 64)
							set(Filled{Field: field, Value: v, Unit: "V", From: fmt.Sprintf("%s p%d: RDS(on) specified at VGS = %s V", mpn, p.Page+1, m[1]), Page: p.Page, Param: p.ID})
							break
						}
					}
				}
			}
		}
	}
	if req.Project != "" && mpn != "" {
		if err := s.fillFromProject(ctx, req.Project, partID, kind, given, set); err != nil {
			return resp, err
		}
		if p, err := s.Lib.Project(ctx, req.Project); err == nil {
			resp.Project = p.Name
		}
	}
	raw, _ := json.Marshal(inputs)
	var (
		res calc.Result
		err error
	)
	switch kind {
	case "buck":
		var in calc.BuckInput
		err = decodeInputs(raw, &in)
		if err == nil {
			res, err = calc.Buck(in)
		}
	case "boost":
		var in calc.BoostInput
		err = decodeInputs(raw, &in)
		if err == nil {
			res, err = calc.Boost(in)
		}
	case "ldo":
		var in calc.LDOInput
		err = decodeInputs(raw, &in)
		if err == nil {
			res, err = calc.LDO(in)
		}
	case "gate_drive":
		var in calc.GateDriveInput
		err = decodeInputs(raw, &in)
		if err == nil {
			res, err = calc.GateDrive(in)
		}
	case "gate_divider":
		var in calc.GateDividerInput
		err = decodeInputs(raw, &in)
		if err == nil {
			res, err = calc.GateDivider(in)
		}
	case "divider":
		var in calc.DividerInput
		err = decodeInputs(raw, &in)
		if err == nil {
			res, err = calc.Divider(in)
		}
	}
	resp.Inputs = inputs
	if err != nil {
		if len(resp.Missing) > 0 {
			return resp, fmt.Errorf("%w (not found on the datasheet: %s; pass them in inputs or record them with record_parameters)", err, strings.Join(resp.Missing, ", "))
		}
		return resp, err
	}
	// Lists are [] for the UI and agents, never null.
	if res.Checks == nil {
		res.Checks = []calc.Check{}
	}
	if res.Outputs == nil {
		res.Outputs = []calc.Output{}
	}
	resp.Result = res
	if req.Save {
		var projID int64
		if req.Project != "" {
			p, err := s.Lib.Project(ctx, req.Project)
			if err != nil {
				return resp, err
			}
			projID = p.ID
		}
		title := firstNonEmpty(req.Title, strings.TrimSpace(mpn+" "+kind))
		out, _ := json.Marshal(resp)
		c, err := s.Lib.SaveCalculation(ctx, library.Calculation{ProjectID: projID, PartID: partID, Kind: kind, Title: title, Inputs: raw, Outputs: out})
		if err != nil {
			return resp, err
		}
		resp.SavedID = c.ID
		s.emit(EvProject, map[string]any{"project": req.Project, "action": "calculation"})
	}
	s.emit(EvCalc, resp)
	return resp, nil
}

// decodeInputs decodes JSON inputs strictly, so a misspelled field is an
// error rather than silently ignored.
func decodeInputs(raw []byte, dst any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("inputs: %w (fields: %s)", err, strings.Join(jsonFields(dst), ", "))
	}
	return nil
}

// jsonFields lists a struct's JSON field names.
func jsonFields(v any) []string {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var out []string
	for i := 0; i < t.NumField(); i++ {
		if n := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]; n != "" && n != "-" {
			out = append(out, n)
		}
	}
	return out
}

// CalcFields describes a calculator's inputs for the UI and agents.
func CalcFields(kind string) []map[string]string {
	var v any
	switch kind {
	case "buck":
		v = calc.BuckInput{}
	case "boost":
		v = calc.BoostInput{}
	case "ldo":
		v = calc.LDOInput{}
	case "gate_drive":
		v = calc.GateDriveInput{}
	case "gate_divider":
		v = calc.GateDividerInput{}
	case "divider":
		v = calc.DividerInput{}
	default:
		return nil
	}
	t := reflect.TypeOf(v)
	var out []map[string]string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		out = append(out, map[string]string{"name": name, "type": f.Type.Kind().String(), "description": f.Tag.Get("jsonschema"), "required": fmt.Sprint(!strings.Contains(f.Tag.Get("json"), "omitempty"))})
	}
	return out
}

// pickParam finds the best parameter value for an input.
func pickParam(params []library.Parameter, src source, mpn string) (Filled, bool) {
	try := func(sectionOK func(string) bool) (Filled, bool) {
		for _, p := range params {
			if p.Key != src.key || !sectionOK(p.Section) {
				continue
			}
			for _, st := range src.stats {
				var v *float64
				switch st {
				case "min":
					v = p.Min
				case "typ":
					v = p.Typ
				case "max":
					v = p.Max
				}
				if v == nil {
					continue
				}
				val, base, ok := extract.ToSI(*v, p.Unit)
				if p.Unit == "" {
					val, base, ok = *v, src.unit, true
				}
				if !ok || base != src.unit {
					continue
				}
				if src.scale != 0 {
					val *= src.scale
				}
				from := fmt.Sprintf("%s p%d: %s %s %s = %g %s", mpn, p.Page+1, strings.TrimSpace(p.Symbol+" "+p.Name), st, conditionsNote(p.Conditions), *v, p.Unit)
				if p.Source != library.SourceAuto {
					from += " (" + p.Source + ")"
				}
				return Filled{Field: src.field, Value: val, Unit: src.unit, From: strings.Join(strings.Fields(from), " "), Page: p.Page, Param: p.ID}, true
			}
		}
		return Filled{}, false
	}
	for _, sec := range src.sections {
		if f, ok := try(func(s string) bool { return s == sec }); ok {
			return f, true
		}
	}
	return try(func(s string) bool {
		return len(src.sections) == 0 || s != extract.KindAbsMax || contains(src.sections, extract.KindAbsMax)
	})
}

// conditionsNote shortens conditions for provenance text.
func conditionsNote(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return ""
	}
	if len(c) > 60 {
		c = c[:60] + "…"
	}
	return "(" + c + ")"
}

// contains reports whether list has s.
func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// fillFromProject takes input and output voltages and the load from the
// part's placement in a project: its supply rail gives vinMin/vinMax,
// its output rail gives vout and the load current on that rail.
func (s *Service) fillFromProject(ctx context.Context, ref string, partID int64, kind string, given func(string) bool, set func(Filled)) error {
	proj, err := s.Lib.Project(ctx, ref)
	if err != nil {
		return err
	}
	pps, err := s.Lib.ProjectParts(ctx, proj.ID)
	if err != nil {
		return err
	}
	rails, err := s.Lib.Rails(ctx, proj.ID)
	if err != nil {
		return err
	}
	rail := func(name string) *library.Rail {
		for i := range rails {
			if strings.EqualFold(rails[i].Name, name) {
				return &rails[i]
			}
		}
		return nil
	}
	var pp *library.ProjectPart
	for i := range pps {
		if pps[i].PartID == partID {
			pp = &pps[i]
			break
		}
	}
	if pp == nil {
		return nil
	}
	from := func(what string) string {
		return fmt.Sprintf("project %s: %s (%s)", proj.Name, what, firstNonEmpty(pp.Designator, pp.MPN))
	}
	if r := rail(pp.SupplyRail); r != nil {
		lo, hi := railRange(*r)
		if lo > 0 && !given("vinMin") && kind != "gate_drive" && kind != "divider" {
			set(Filled{Field: "vinMin", Value: lo, Unit: "V", From: from("supply rail " + r.Name + " minimum")})
		}
		if hi > 0 && !given("vinMax") && kind != "gate_drive" && kind != "divider" {
			set(Filled{Field: "vinMax", Value: hi, Unit: "V", From: from("supply rail " + r.Name + " maximum")})
		}
	}
	if r := rail(pp.OutputRail); r != nil {
		if r.VNom != nil && !given("vout") && (kind == "buck" || kind == "boost" || kind == "ldo") {
			set(Filled{Field: "vout", Value: *r.VNom, Unit: "V", From: from("output rail " + r.Name)})
		}
		if !given("iout") && (kind == "buck" || kind == "boost" || kind == "ldo") {
			// The same power-tree totals as the project view: direct loads
			// plus the input current of regulators fed from this rail.
			if d, err := s.ProjectDetail(ctx, proj.Name); err == nil {
				for _, rs := range d.Rails {
					if strings.EqualFold(rs.Name, r.Name) && rs.LoadMax > 0 {
						set(Filled{Field: "iout", Value: rs.LoadMax, Unit: "A", From: from(fmt.Sprintf("maximum load on %s, incl. downstream regulators", r.Name))})
					}
				}
			}
		}
	}
	return nil
}

// railRange returns a rail's minimum and maximum voltage, falling back to
// the nominal value.
func railRange(r library.Rail) (float64, float64) {
	var lo, hi float64
	if r.VNom != nil {
		lo, hi = *r.VNom, *r.VNom
	}
	if r.VMin != nil {
		lo = *r.VMin
	}
	if r.VMax != nil {
		hi = *r.VMax
	}
	return lo, hi
}
