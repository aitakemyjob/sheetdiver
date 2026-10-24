package core

import (
	"context"
	"fmt"
	"math"
	"regexp"
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
	// Connections is the connection table and block diagram.
	Connections Connections `json:"connections"`
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
	// Capacity is the rail's current limit or its regulator's rated
	// output current.
	Capacity     float64 `json:"capacity,omitempty"`
	CapacityFrom string  `json:"capacityFrom,omitempty"`
	// Ceiling is the regulator's switch current limit when it has no
	// output rating: the load can't exceed it, but what the regulator
	// actually delivers is lower (ripple, VIN, duty cycle), so it isn't
	// a capacity.
	Ceiling     float64  `json:"ceiling,omitempty"`
	CeilingFrom string   `json:"ceilingFrom,omitempty"`
	SourcePart  string   `json:"sourcePart,omitempty"`
	Consumers   []string `json:"consumers"`
	Power       float64  `json:"power"`
	// Ripple is how much ripple (peak to peak, volts) the rail can carry
	// before a part on it leaves its recommended input range, on top of
	// the rail's DC tolerance; nil when no part's range is known.
	Ripple *float64 `json:"ripple,omitempty"`
	// RippleLimit is the part and limit that set it, RipplePage its
	// datasheet page (0-based).
	RippleLimit string `json:"rippleLimit,omitempty"`
	RipplePage  int    `json:"ripplePage,omitempty"`
}

// ProjectCheck is a finding about a project.
type ProjectCheck struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Part    string `json:"part,omitempty"`
	Rail    string `json:"rail,omitempty"`
	Page    int    `json:"page,omitempty"`
	// Area is "connections" for checks on the connection table.
	Area string `json:"area,omitempty"`
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
	var loops []string
	out.Rails, out.InputPower, loops = railStatus(rails, out.Parts, params)
	rippleBudgets(out.Rails, out.Parts, params)
	out.Checks = projectChecks(out.Rails, out.Parts, params, loops)
	conns, checks, err := s.connections(ctx, p, out.Parts, out.Rails)
	if err != nil {
		return out, err
	}
	out.Connections = conns
	out.Checks = append(out.Checks, checks...)
	sortChecks(out.Checks)
	return out, nil
}

// railStatus computes each rail's load (the loads entered on the rail,
// the parts on it, and the input current of regulators fed from it),
// propagating regulator input currents up the power tree. A part with the same supply and output rail
// is in line on that rail (a protection switch): a load, not a source. A
// regulator whose output feeds back to its own supply (a loop in the tree)
// can't be propagated; it counts as a plain load and is returned in loops.
// A charger's input current includes its charge current.
func railStatus(rails []library.Rail, parts []library.ProjectPart, params map[int64][]library.Parameter) ([]RailStatus, float64, []string) {
	st := make([]RailStatus, len(rails))
	idx := map[string]int{}
	for i, r := range rails {
		st[i] = RailStatus{Rail: r, Consumers: []string{}}
		idx[strings.ToLower(r.Name)] = i
	}
	name := func(pp library.ProjectPart) string { return firstNonEmpty(pp.Designator, pp.MPN) }
	inLoop := railLoops(parts, idx)
	var loops []string
	for _, pp := range parts {
		if inLoop[pp.ID] {
			loops = append(loops, name(pp))
		}
	}
	for _, pp := range parts {
		if i, ok := idx[strings.ToLower(pp.OutputRail)]; ok && pp.OutputRail != "" && !inLine(pp) {
			st[i].SourcePart = name(pp) + " " + pp.MPN
			if st[i].Source == "" {
				st[i].Source = name(pp)
			}
			if c, from := regulatorCapacity(params[pp.PartID]); c > 0 && st[i].CurrentLimit == nil {
				st[i].Capacity, st[i].CapacityFrom = c, pp.MPN+" "+from
			} else if c, from := regulatorCeiling(params[pp.PartID]); c > 0 && st[i].CurrentLimit == nil {
				st[i].Ceiling, st[i].CeilingFrom = c, pp.MPN+" "+from
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
		// Loads entered on the rail itself (not in the BOM).
		for i, r := range st {
			if r.OtherTyp != nil {
				typ[i] = *r.OtherTyp
			}
			if r.OtherMax != nil {
				mx[i] = *r.OtherMax
			} else {
				mx[i] = typ[i]
			}
		}
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
			oi, regulates := idx[strings.ToLower(pp.OutputRail)]
			regulates = regulates && pp.OutputRail != "" && !inLine(pp) && !inLoop[pp.ID]
			charger := pp.Category == "charger"
			switch {
			case (pp.ITyp != nil || pp.IMax != nil) && (!charger || !regulates):
				// The currents entered for the part.
			case regulates:
				// A regulator: input current from its output load (and, for
				// a charger, the battery's charge current).
				eff, isReg := regulatorCats[pp.Category]
				if !isReg {
					eff = 0.85
				}
				outTyp, outMax := st[oi].LoadTyp, st[oi].LoadMax
				if charger {
					// A charger's entered currents are the charge current
					// set on the board; none set, no charging is counted.
					outTyp, outMax = chargerOutput(params[pp.PartID], pp.ITyp, pp.IMax, outTyp, outMax)
				}
				vin := railNominal(st[si].Rail)
				vout := railNominal(st[oi].Rail)
				iq := quiescent(params[pp.PartID])
				if eff == 0 || vin == 0 || vout == 0 {
					it, im = outTyp+iq, outMax+iq
				} else {
					it = vout*outTyp/(vin*eff) + iq
					im = vout*outMax/(vin*eff) + iq
				}
			case pp.OutputRail != "":
				// In line on its rail, or in a loop: its own quiescent current.
				it = quiescent(params[pp.PartID])
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
	return st, input, loops
}

// rippleBudgets sets each rail's ripple budget: twice the smaller of its
// margins to the parts on it, the low margin from the rail's lowest DC
// voltage down to the highest minimum input, the high margin from its
// highest DC voltage up to the lowest maximum input (recommended
// operating ranges). Ripple swings both ways around the DC level, so the
// smaller margin limits it.
func rippleBudgets(rails []RailStatus, parts []library.ProjectPart, params map[int64][]library.Parameter) {
	for i := range rails {
		r := &rails[i]
		lo, hi := railRange(r.Rail)
		if hi <= 0 {
			continue
		}
		margin, limit, page := math.Inf(1), "", 0
		for _, pp := range parts {
			if !strings.EqualFold(pp.SupplyRail, r.Name) {
				continue
			}
			ps := params[pp.PartID]
			label := firstNonEmpty(pp.Designator, pp.MPN)
			if v, p, ok := paramSI(ps, "vin", []string{extract.KindRecommended}, "max"); ok && p.Section == extract.KindRecommended {
				if m := v - hi; m < margin {
					margin, page = m, p.Page
					limit = fmt.Sprintf("%s's maximum input %s (%s p%d)", label, calc.Eng(v, "V"), pp.MPN, p.Page+1)
				}
			}
			if v, p, ok := paramSI(ps, "vin", []string{extract.KindRecommended, extract.KindElectrical}, "min"); ok && p.Section != extract.KindAbsMax && v > 0 {
				if m := lo - v; m < margin {
					margin, page = m, p.Page
					limit = fmt.Sprintf("%s's minimum input %s (%s p%d)", label, calc.Eng(v, "V"), pp.MPN, p.Page+1)
				}
			}
		}
		if math.IsInf(margin, 1) {
			continue
		}
		budget := math.Max(0, 2*margin)
		r.Ripple, r.RippleLimit, r.RipplePage = &budget, limit, page
	}
}

// firstPositive is the first value set and above zero, or 0.
func firstPositive(vs ...*float64) float64 {
	for _, v := range vs {
		if v != nil && *v > 0 {
			return *v
		}
	}
	return 0
}

// inLine reports whether a part's supply and output are the same rail: it
// sits in line on the rail (a battery protection IC, a load switch).
func inLine(pp library.ProjectPart) bool {
	return pp.OutputRail != "" && strings.EqualFold(pp.SupplyRail, pp.OutputRail)
}

// railLoops finds the parts whose output rail powers, through other
// regulators, their own supply rail: the power tree loops through them.
func railLoops(parts []library.ProjectPart, idx map[string]int) map[int64]bool {
	next := map[int][]int{}
	type edge struct {
		id       int64
		from, to int
	}
	var edges []edge
	for _, pp := range parts {
		si, ok1 := idx[strings.ToLower(pp.SupplyRail)]
		oi, ok2 := idx[strings.ToLower(pp.OutputRail)]
		if ok1 && ok2 && pp.SupplyRail != "" && pp.OutputRail != "" && !inLine(pp) {
			next[si] = append(next[si], oi)
			edges = append(edges, edge{pp.ID, si, oi})
		}
	}
	reaches := func(from, to int) bool {
		seen := map[int]bool{from: true}
		todo := []int{from}
		for len(todo) > 0 {
			r := todo[0]
			todo = todo[1:]
			if r == to {
				return true
			}
			for _, n := range next[r] {
				if !seen[n] {
					seen[n] = true
					todo = append(todo, n)
				}
			}
		}
		return false
	}
	out := map[int64]bool{}
	for _, e := range edges {
		if reaches(e.to, e.from) {
			out[e.id] = true
		}
	}
	return out
}

// chargerOutput is what a charger delivers while charging at the charge
// current set for it (typ, max; one may be nil), given the load on its
// output rail. Without a charge current set, charging isn't counted. A
// simple linear charger (TP4056) limits its total output, battery and
// system together, to the charge current, so it delivers the larger of
// the two. A power-path charger (it has an input current limit, ilim_in)
// charges on top of the system load, up to that limit.
func chargerOutput(ps []library.Parameter, chgTyp, chgMax *float64, loadTyp, loadMax float64) (float64, float64) {
	if chgTyp == nil && chgMax == nil {
		return loadTyp, loadMax
	}
	ct, cm := 0.0, 0.0
	if chgTyp != nil {
		ct, cm = *chgTyp, *chgTyp
	}
	if chgMax != nil {
		cm = *chgMax
		if chgTyp == nil {
			ct = cm
		}
	}
	if lim, _, ok := paramSI(ps, "ilim_in", nil, "max", "typ"); ok && lim > 0 {
		return math.Min(loadTyp+ct, lim), math.Min(loadMax+cm, lim)
	}
	return math.Max(loadTyp, ct), math.Max(loadMax, cm)
}

// chargeCurrent is a charger's largest charge current from its datasheet
// (typical and maximum, in amps) and the page it's on; absolute maximum
// ratings don't count. The actual current is set on the board (a
// programming resistor), so this is the worst case.
func chargeCurrent(ps []library.Parameter) (typ, max float64, page int) {
	for _, p := range ps {
		if p.Key != "ichg" || p.Section == extract.KindAbsMax {
			continue
		}
		v, _, ok := paramSI([]library.Parameter{p}, "ichg", nil, "max", "typ")
		if ok && v > max {
			max, page = v, p.Page
			typ = v
			if t, _, ok := paramSI([]library.Parameter{p}, "ichg", nil, "typ"); ok {
				typ = t
			}
		}
	}
	return typ, max, page
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

// regulatorCapacity is a regulator's rated output current.
func regulatorCapacity(ps []library.Parameter) (float64, string) {
	if v, p, ok := paramSI(ps, "iout", []string{extract.KindRecommended, extract.KindElectrical}, "max", "typ"); ok {
		return v, fmt.Sprintf("rated output (p%d)", p.Page+1)
	}
	return 0, ""
}

// regulatorCeiling is a regulator's minimum switch current limit: an
// upper bound on its output, not a rating.
func regulatorCeiling(ps []library.Parameter) (float64, string) {
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
func projectChecks(rails []RailStatus, parts []library.ProjectPart, params map[int64][]library.Parameter, loops []string) []ProjectCheck {
	out := []ProjectCheck{}
	add := func(level, part, rail string, page int, format string, a ...any) {
		out = append(out, ProjectCheck{Level: level, Part: part, Rail: rail, Page: page, Message: fmt.Sprintf(format, a...)})
	}
	byName := map[string]*RailStatus{}
	for i := range rails {
		byName[strings.ToLower(rails[i].Name)] = &rails[i]
	}
	for _, r := range rails {
		if r.Ripple == nil {
			continue
		}
		vn := railNominal(r.Rail)
		switch {
		case *r.Ripple == 0:
			add(calc.Error, "", r.Name, r.RipplePage, "%s has no room for ripple: its DC range already reaches %s", r.Name, r.RippleLimit)
		case vn > 0 && *r.Ripple < 0.02*vn:
			add(calc.Warn, "", r.Name, r.RipplePage, "%s can carry only %s p-p ripple before reaching %s; a switching regulator's ripple is often 1–2%% of its output, so tighten the rail's tolerance or filter it", r.Name, calc.Eng(*r.Ripple, "V"), r.RippleLimit)
		default:
			add(calc.OK, "", r.Name, r.RipplePage, "%s can carry up to %s p-p ripple; limited by %s", r.Name, calc.Eng(*r.Ripple, "V"), r.RippleLimit)
		}
	}
	if len(loops) > 0 {
		add(calc.Error, strings.Join(loops, ", "), "", 0, "%s power each other's supply: the power tree loops, so their currents aren't passed upstream; check their supply and output rails", strings.Join(loops, " and "))
	}
	for _, pp := range parts {
		label := firstNonEmpty(pp.Designator, pp.MPN)
		if inLine(pp) && pp.Category != "battery-protection" && pp.Category != "load-switch" {
			add(calc.Warn, label, pp.SupplyRail, 0, "%s (%s) has %s as both supply and output, so it's counted as a load on %s, not as its source; if it converts the voltage, set its output to another rail", label, pp.MPN, pp.SupplyRail, pp.SupplyRail)
		}
		// Currents entered on a regulator's row are its input current, so
		// its output rail's loads aren't passed up.
		if (pp.ITyp != nil || pp.IMax != nil) && pp.OutputRail != "" && !inLine(pp) && pp.Category != "charger" {
			if r := byName[strings.ToLower(pp.OutputRail)]; r != nil && r.LoadMax > 0 {
				add(calc.Warn, label, pp.SupplyRail, 0, "%s has its own current entered (%s), which is used as its input current instead of deriving it from %s's load (%s); clear it to pass %s's load up to %s", label, calc.Eng(firstPositive(pp.IMax, pp.ITyp), "A"), r.Name, calc.Eng(r.LoadMax, "A"), r.Name, pp.SupplyRail)
			}
		}
		if pp.Category == "charger" && pp.OutputRail != "" && !inLine(pp) {
			if pp.ITyp == nil && pp.IMax == nil {
				hint := ""
				if _, m, page := chargeCurrent(params[pp.PartID]); m > 0 {
					hint = fmt.Sprintf(" (the datasheet goes up to %s, %s p%d)", calc.Eng(m, "A"), pp.MPN, page+1)
				}
				add(calc.Warn, label, pp.SupplyRail, 0, "%s has no charge current set, so charging isn't counted on %s; enter the charge current your board programs as its current%s", label, pp.SupplyRail, hint)
			} else {
				add(calc.OK, label, pp.SupplyRail, 0, "%s charges at %s (entered); its input on %s covers that and its output's load", label, calc.Eng(firstPositive(pp.IMax, pp.ITyp), "A"), pp.SupplyRail)
			}
		}
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
		if pp.SupplyRail == "" && pp.OutputRail == "" && !unpowered(pp) {
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
				if pp.ITyp == nil && pp.IMax == nil && pp.OutputRail == "" && !unpowered(pp) {
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
		if r.Capacity == 0 && r.Ceiling > 0 && r.LoadMax > 0 {
			if r.LoadMax > r.Ceiling {
				add(calc.Error, r.SourcePart, r.Name, 0, "%s load %s exceeds the regulator's current limit %s (%s)", r.Name, calc.Eng(r.LoadMax, "A"), calc.Eng(r.Ceiling, "A"), r.CeilingFrom)
			} else {
				add(calc.Warn, r.SourcePart, r.Name, 0, "%s load %s: the regulator has no output-current rating, so its capacity is unknown. Its current limit %s (%s) is only a ceiling: inductor ripple and the input voltage lower what it can deliver. Check the datasheet's output-current graphs or record iout", r.Name, calc.Eng(r.LoadMax, "A"), calc.Eng(r.Ceiling, "A"), r.CeilingFrom)
			}
		}
		if r.VNom == nil && r.VMin == nil && r.VMax == nil {
			add(calc.Warn, "", r.Name, 0, "%s has no voltage set", r.Name)
		}
	}
	sortChecks(out)
	return out
}

// sortChecks puts errors first, then warnings, then the rest.
func sortChecks(cs []ProjectCheck) {
	rank := map[string]int{calc.Error: 0, calc.Warn: 1, calc.OK: 2}
	sort.SliceStable(cs, func(i, j int) bool { return rank[cs[i].Level] < rank[cs[j].Level] })
}

// Parts that draw no supply current of their own, by category (as the
// datasheet or the BOM names it) or by designator (R1, D1, SW2, Y1, J3).
// An oscillator is powered, whatever it's called.
var (
	unpoweredCategoryRe = regexp.MustCompile(`(?i)resistor|capacitor|inductor|ferrite|diode|\bled|tvs|zener|schottky|fuse|switch|button|crystal|resonator|connector|header|jumper|test ?point|mosfet|transistor|\bbjt|mechanical|mounting|heat ?sink`)
	unpoweredRefRe      = regexp.MustCompile(`(?i)^(R|RN|C|L|FB|D|LED|Q|F|SW|S|BTN|Y|X|J|P|CN|H|MH|TP|JP)\d`)
	poweredCategoryRe   = regexp.MustCompile(`(?i)oscillator`)
)

// unpowered reports whether a BOM entry draws no supply current of its
// own (a passive, an LED, a switch, a crystal, a connector), so isn't
// expected on a rail or to have a load current.
func unpowered(pp library.ProjectPart) bool {
	if poweredCategoryRe.MatchString(pp.Category) {
		return false
	}
	ref := strings.TrimSpace(strings.Split(pp.Designator, ",")[0])
	return unpoweredCategoryRe.MatchString(pp.Category) || unpoweredRefRe.MatchString(ref)
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
