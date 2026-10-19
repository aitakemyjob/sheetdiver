package core

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// ProjectDetail is a project with its BOM, rails, power budget, checks,
// saved calculations and notes.
type ProjectDetail struct {
	Project      library.Project       `json:"project"`
	Parts        []library.ProjectPart `json:"parts"`
	Rails        []RailStatus          `json:"rails"`
	Checks       []ProjectCheck        `json:"checks"`
	Calculations []library.Calculation `json:"calculations"`
	Notes        []library.Note        `json:"notes"`
	// InputPower is the power drawn from rails with no regulator in the
	// project (the board's inputs), at maximum load.
	InputPower float64 `json:"inputPower"`
}

// RailStatus is a rail with its computed load.
type RailStatus struct {
	library.Rail
	// LoadTyp and LoadMax are the rail's current in amps: direct loads
	// plus the input current of regulators fed from it.
	LoadTyp float64 `json:"loadTyp"`
	LoadMax float64 `json:"loadMax"`
	// Capacity is the rail's current limit or its regulator's rating.
	Capacity     float64  `json:"capacity,omitempty"`
	CapacityFrom string   `json:"capacityFrom,omitempty"`
	SourcePart   string   `json:"sourcePart,omitempty"`
	Consumers    []string `json:"consumers"`
	Power        float64  `json:"power"`
}

// ProjectCheck is a finding about a project.
type ProjectCheck struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Part    string `json:"part,omitempty"`
	Rail    string `json:"rail,omitempty"`
	Page    int    `json:"page,omitempty"`
}

// regulatorCats are categories whose input current derives from their
// output load.
var regulatorCats = map[string]float64{"buck": 0.9, "boost": 0.85, "buck-boost": 0.85, "power": 0.85, "ldo": 0, "charger": 0}

// ProjectDetail analyzes a project.
func (s *Service) ProjectDetail(ctx context.Context, ref string) (ProjectDetail, error) {
	p, err := s.Lib.Project(ctx, ref)
	if err != nil {
		return ProjectDetail{}, err
	}
	out := ProjectDetail{Project: p, Checks: []ProjectCheck{}}
	if out.Parts, err = s.Lib.ProjectParts(ctx, p.ID); err != nil {
		return out, err
	}
	rails, err := s.Lib.Rails(ctx, p.ID)
	if err != nil {
		return out, err
	}
	out.Calculations, _ = s.Lib.Calculations(ctx, p.ID)
	out.Notes, _ = s.Lib.Notes(ctx, library.TargetProject, p.Name)
	params := map[int64][]library.Parameter{}
	for _, pp := range out.Parts {
		if _, ok := params[pp.PartID]; ok {
			continue
		}
		part, err := s.Lib.PartByID(ctx, pp.PartID)
		if err != nil || part.DatasheetID == 0 {
			params[pp.PartID] = nil
			continue
		}
		params[pp.PartID], _ = s.Lib.Parameters(ctx, part.DatasheetID, library.ParamFilter{MPN: part.MPN})
	}
	out.Rails, out.InputPower = railStatus(rails, out.Parts, params)
	out.Checks = projectChecks(out.Rails, out.Parts, params)
	return out, nil
}

// railStatus computes each rail's load, propagating regulator input
// currents up the power tree.
func railStatus(rails []library.Rail, parts []library.ProjectPart, params map[int64][]library.Parameter) ([]RailStatus, float64) {
	st := make([]RailStatus, len(rails))
	idx := map[string]int{}
	for i, r := range rails {
		st[i] = RailStatus{Rail: r, Consumers: []string{}}
		idx[strings.ToLower(r.Name)] = i
	}
	name := func(pp library.ProjectPart) string { return firstNonEmpty(pp.Designator, pp.MPN) }
	for _, pp := range parts {
		if i, ok := idx[strings.ToLower(pp.OutputRail)]; ok && pp.OutputRail != "" {
			st[i].SourcePart = name(pp) + " " + pp.MPN
			if st[i].Source == "" {
				st[i].Source = name(pp)
			}
			if c, from := regulatorCapacity(params[pp.PartID]); c > 0 && st[i].CurrentLimit == nil {
				st[i].Capacity, st[i].CapacityFrom = c, pp.MPN+" "+from
			}
		}
		if i, ok := idx[strings.ToLower(pp.SupplyRail)]; ok && pp.SupplyRail != "" {
			st[i].Consumers = append(st[i].Consumers, name(pp))
		}
	}
	for i := range st {
		if st[i].CurrentLimit != nil {
			st[i].Capacity, st[i].CapacityFrom = *st[i].CurrentLimit, "rail current limit"
		}
	}
	// Iterate: loads on output rails determine regulator input currents.
	for iter := 0; iter < 12; iter++ {
		typ := make([]float64, len(st))
		mx := make([]float64, len(st))
		for _, pp := range parts {
			si, ok := idx[strings.ToLower(pp.SupplyRail)]
			if !ok || pp.SupplyRail == "" {
				continue
			}
			q := float64(pp.Qty)
			it, im := 0.0, 0.0
			if pp.ITyp != nil {
				it = *pp.ITyp
			}
			if pp.IMax != nil {
				im = *pp.IMax
			}
			if oi, ok := idx[strings.ToLower(pp.OutputRail)]; ok && pp.OutputRail != "" && pp.ITyp == nil && pp.IMax == nil {
				// A regulator: input current from its output load.
				eff, isReg := regulatorCats[pp.Category]
				if !isReg {
					eff = 0.85
				}
				vin := railNominal(st[si].Rail)
				vout := railNominal(st[oi].Rail)
				iq := quiescent(params[pp.PartID])
				if eff == 0 || vin == 0 || vout == 0 {
					it, im = st[oi].LoadTyp+iq, st[oi].LoadMax+iq
				} else {
					it = vout*st[oi].LoadTyp/(vin*eff) + iq
					im = vout*st[oi].LoadMax/(vin*eff) + iq
				}
			}
			if im == 0 {
				im = it
			}
			typ[si] += it * q
			mx[si] += im * q
		}
		changed := false
		for i := range st {
			if math.Abs(st[i].LoadTyp-typ[i]) > 1e-12 || math.Abs(st[i].LoadMax-mx[i]) > 1e-12 {
				changed = true
			}
			st[i].LoadTyp, st[i].LoadMax = typ[i], mx[i]
		}
		if !changed {
			break
		}
	}
	input := 0.0
	for i := range st {
		st[i].Power = railNominal(st[i].Rail) * st[i].LoadMax
		if st[i].SourcePart == "" {
			input += st[i].Power
		}
	}
	return st, input
}

// railNominal is a rail's nominal voltage (the midpoint if only a range
// is known).
func railNominal(r library.Rail) float64 {
	if r.VNom != nil {
		return *r.VNom
	}
	lo, hi := railRange(r)
	return (lo + hi) / 2
}

// paramSI returns the first value of a keyed parameter in SI units.
func paramSI(ps []library.Parameter, key string, sections []string, stats ...string) (float64, library.Parameter, bool) {
	for _, sec := range append(sections, "") {
		for _, p := range ps {
			if p.Key != key || (sec != "" && p.Section != sec) {
				continue
			}
			for _, st := range stats {
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
				if si, _, ok := extract.ToSI(*v, p.Unit); ok {
					return si, p, true
				}
			}
		}
	}
	return 0, library.Parameter{}, false
}

// regulatorCapacity is a regulator's output current rating, else its
// minimum current limit.
func regulatorCapacity(ps []library.Parameter) (float64, string) {
	if v, p, ok := paramSI(ps, "iout", []string{extract.KindRecommended, extract.KindElectrical}, "max", "typ"); ok {
		return v, fmt.Sprintf("rated output (p%d)", p.Page+1)
	}
	if v, p, ok := paramSI(ps, "ilim", nil, "min", "typ"); ok {
		return v, fmt.Sprintf("current limit, min (p%d)", p.Page+1)
	}
	return 0, ""
}

// quiescent is a part's typical quiescent current in amps (0 if unknown).
func quiescent(ps []library.Parameter) float64 {
	v, _, _ := paramSI(ps, "iq", nil, "typ", "max")
	return v
}

// projectChecks compares rails against the parts on them.
func projectChecks(rails []RailStatus, parts []library.ProjectPart, params map[int64][]library.Parameter) []ProjectCheck {
	var out []ProjectCheck
	add := func(level, part, rail string, page int, format string, a ...any) {
		out = append(out, ProjectCheck{Level: level, Part: part, Rail: rail, Page: page, Message: fmt.Sprintf(format, a...)})
	}
	byName := map[string]*RailStatus{}
	for i := range rails {
		byName[strings.ToLower(rails[i].Name)] = &rails[i]
	}
	for _, pp := range parts {
		label := firstNonEmpty(pp.Designator, pp.MPN)
		ps := params[pp.PartID]
		if library.Discouraged(pp.Status) {
			add(calc.Warn, label, "", 0, "%s (%s) is marked %s: %s", label, pp.MPN, strings.ToUpper(pp.Status), library.Statuses[pp.Status])
		}
		if !pp.HasDatasheet {
			add(calc.Warn, label, "", 0, "%s (%s) has no datasheet in the library, so its limits aren't checked; import one", label, pp.MPN)
		}
		if pp.SupplyRail == "" && pp.OutputRail == "" && !passive(pp.Category) {
			add(calc.Warn, label, "", 0, "%s (%s) is not connected to a rail; set its supply rail to check voltages and currents", label, pp.MPN)
		}
		if pp.SupplyRail != "" {
			r := byName[strings.ToLower(pp.SupplyRail)]
			if r == nil {
				add(calc.Warn, label, pp.SupplyRail, 0, "%s is supplied from %s, which is not defined; add the rail with its voltage range", label, pp.SupplyRail)
			} else {
				lo, hi := railRange(r.Rail)
				if hi > 0 {
					if v, p, ok := paramSI(ps, "vin", []string{extract.KindAbsMax}, "max"); ok && p.Section == extract.KindAbsMax {
						if hi > v {
							add(calc.Error, label, r.Name, p.Page, "%s: %s reaches %s, above the absolute maximum %s (%s p%d)", label, r.Name, calc.Eng(hi, "V"), calc.Eng(v, "V"), pp.MPN, p.Page+1)
						}
					}
					if v, p, ok := paramSI(ps, "vin", []string{extract.KindRecommended}, "max"); ok && p.Section == extract.KindRecommended {
						if hi > v {
							add(calc.Warn, label, r.Name, p.Page, "%s: %s reaches %s, above the recommended maximum %s (%s p%d)", label, r.Name, calc.Eng(hi, "V"), calc.Eng(v, "V"), pp.MPN, p.Page+1)
						} else {
							add(calc.OK, label, r.Name, p.Page, "%s: %s maximum %s is within the recommended %s", label, r.Name, calc.Eng(hi, "V"), calc.Eng(v, "V"))
						}
					}
				}
				if lo > 0 {
					if v, p, ok := paramSI(ps, "vin", []string{extract.KindRecommended, extract.KindElectrical}, "min"); ok && p.Section != extract.KindAbsMax && v > 0 {
						if lo < v {
							add(calc.Error, label, r.Name, p.Page, "%s: %s can drop to %s, below the minimum operating input %s (%s p%d)", label, r.Name, calc.Eng(lo, "V"), calc.Eng(v, "V"), pp.MPN, p.Page+1)
						}
					}
				}
				if pp.ITyp == nil && pp.IMax == nil && pp.OutputRail == "" && !passive(pp.Category) {
					if iq := quiescent(ps); iq > 0 {
						add(calc.Warn, label, r.Name, 0, "%s has no load current set; its quiescent current is %s, but set the actual operating current for an accurate budget", label, calc.Eng(iq, "A"))
					} else {
						add(calc.Warn, label, r.Name, 0, "%s has no load current set, so it adds nothing to %s's budget", label, r.Name)
					}
				}
			}
		}
		if pp.OutputRail != "" {
			r := byName[strings.ToLower(pp.OutputRail)]
			if r == nil {
				add(calc.Warn, label, pp.OutputRail, 0, "%s drives %s, which is not defined", label, pp.OutputRail)
			} else if vn := railNominal(r.Rail); vn > 0 {
				if v, p, ok := paramSI(ps, "vout", []string{extract.KindRecommended, extract.KindElectrical}, "max"); ok && vn > v*1.001 {
					add(calc.Error, label, r.Name, p.Page, "%s: %s at %s is above the output range maximum %s (%s p%d)", label, r.Name, calc.Eng(vn, "V"), calc.Eng(v, "V"), pp.MPN, p.Page+1)
				}
				if v, p, ok := paramSI(ps, "vout", []string{extract.KindRecommended, extract.KindElectrical}, "min"); ok && v > 0 && vn < v*0.999 {
					add(calc.Error, label, r.Name, p.Page, "%s: %s at %s is below the output range minimum %s (%s p%d)", label, r.Name, calc.Eng(vn, "V"), calc.Eng(v, "V"), pp.MPN, p.Page+1)
				}
				if supply := byName[strings.ToLower(pp.SupplyRail)]; supply != nil {
					lo, hi := railRange(supply.Rail)
					switch pp.Category {
					case "buck", "ldo":
						if lo > 0 && lo <= vn {
							add(calc.Error, label, r.Name, 0, "%s steps down %s → %s, but %s can be as low as %s", label, supply.Name, r.Name, supply.Name, calc.Eng(lo, "V"))
						}
					case "boost":
						if hi >= vn {
							add(calc.Error, label, r.Name, 0, "%s boosts %s → %s, but %s can reach %s", label, supply.Name, r.Name, supply.Name, calc.Eng(hi, "V"))
						}
					}
				}
			}
		}
	}
	for _, r := range rails {
		if r.Capacity > 0 {
			u := r.LoadMax / r.Capacity
			switch {
			case u > 1:
				add(calc.Error, r.SourcePart, r.Name, 0, "%s load %s exceeds its capacity %s (%s)", r.Name, calc.Eng(r.LoadMax, "A"), calc.Eng(r.Capacity, "A"), r.CapacityFrom)
			case u > 0.8:
				add(calc.Warn, r.SourcePart, r.Name, 0, "%s load %s is %.0f%% of its capacity %s (%s)", r.Name, calc.Eng(r.LoadMax, "A"), u*100, calc.Eng(r.Capacity, "A"), r.CapacityFrom)
			default:
				add(calc.OK, r.SourcePart, r.Name, 0, "%s load %s is %.0f%% of %s", r.Name, calc.Eng(r.LoadMax, "A"), u*100, calc.Eng(r.Capacity, "A"))
			}
		}
		if r.VNom == nil && r.VMin == nil && r.VMax == nil {
			add(calc.Warn, "", r.Name, 0, "%s has no voltage set", r.Name)
		}
	}
	rank := map[string]int{calc.Error: 0, calc.Warn: 1, calc.OK: 2}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Level] < rank[out[j].Level] })
	return out
}

// passive reports whether a category draws no supply current of its own.
func passive(cat string) bool {
	switch cat {
	case "inductor", "capacitor", "diode", "mosfet", "mosfet-n", "mosfet-p", "resistor":
		return true
	}
	return false
}

// AddToProject places a part in a project by part reference.
func (s *Service) AddToProject(ctx context.Context, project, part string, pp library.ProjectPart) (library.ProjectPart, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return pp, err
	}
	r, err := s.Lib.Resolve(ctx, part)
	if err != nil {
		return pp, err
	}
	pp.ProjectID, pp.PartID = p.ID, r.Part.ID
	if pp.Variant == "" && r.Via == "variant" {
		pp.Variant = r.Variant
	}
	out, err := s.Lib.AddProjectPart(ctx, pp)
	if err == nil {
		out.MPN, out.Category = r.Part.MPN, r.Part.Category
		s.emit(EvProject, map[string]any{"project": p.Name, "action": "part"})
	}
	return out, err
}

// ProjectChanged tells the UI a project changed.
func (s *Service) ProjectChanged(name, action string) {
	s.emit(EvProject, map[string]any{"project": name, "action": action})
}
