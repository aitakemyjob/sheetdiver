package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

type createProjectArgs struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Activate    bool   `json:"activate,omitempty" jsonschema:"make it the default project"`
}

type projectArgs struct {
	Project string `json:"project,omitempty" jsonschema:"project name or id; default: the active project"`
	Show    bool   `json:"show,omitempty" jsonschema:"also open the project in SheetDiver"`
}

type railArgs struct {
	Project      string   `json:"project,omitempty" jsonschema:"project name or id; default: the active project"`
	Name         string   `json:"name" jsonschema:"rail name, e.g. VIN, 5V, 3V3, VBAT"`
	VNom         *float64 `json:"v_nom,omitempty" jsonschema:"nominal voltage (V)"`
	VMin         *float64 `json:"v_min,omitempty" jsonschema:"minimum voltage incl. tolerance and sag (V)"`
	VMax         *float64 `json:"v_max,omitempty" jsonschema:"maximum voltage incl. tolerance and transients (V)"`
	CurrentLimit *float64 `json:"current_limit,omitempty" jsonschema:"what the source can deliver (A); regulators in the project are used if omitted"`
	Source       string   `json:"source,omitempty" jsonschema:"what supplies it (a designator, 'USB', 'battery')"`
	Notes        string   `json:"notes,omitempty"`
	Delete       bool     `json:"delete,omitempty" jsonschema:"remove the rail"`
}

type addPartArgs struct {
	Project    string   `json:"project,omitempty" jsonschema:"project name or id; default: the active project"`
	Part       string   `json:"part" jsonschema:"part number (orderable codes are kept as the variant)"`
	Designator string   `json:"designator,omitempty" jsonschema:"e.g. U3, Q1"`
	Qty        int      `json:"qty,omitempty"`
	Role       string   `json:"role,omitempty" jsonschema:"what it does on the board, e.g. '3V3 buck', 'load switch'"`
	SupplyRail string   `json:"supply_rail,omitempty" jsonschema:"rail it is powered from"`
	OutputRail string   `json:"output_rail,omitempty" jsonschema:"rail it generates (regulators)"`
	ITyp       *float64 `json:"i_typ,omitempty" jsonschema:"typical current drawn from the supply rail (A); omit for regulators to derive it from their output load"`
	IMax       *float64 `json:"i_max,omitempty" jsonschema:"maximum current drawn from the supply rail (A)"`
	Notes      string   `json:"notes,omitempty"`
}

type updateEntryArgs struct {
	Project    string   `json:"project,omitempty"`
	Entry      string   `json:"entry" jsonschema:"BOM entry id or designator (from get_project)"`
	Designator *string  `json:"designator,omitempty"`
	Qty        *int     `json:"qty,omitempty"`
	Role       *string  `json:"role,omitempty"`
	SupplyRail *string  `json:"supply_rail,omitempty"`
	OutputRail *string  `json:"output_rail,omitempty"`
	ITyp       *float64 `json:"i_typ,omitempty"`
	IMax       *float64 `json:"i_max,omitempty"`
	Notes      *string  `json:"notes,omitempty"`
	Remove     bool     `json:"remove,omitempty" jsonschema:"take the part out of the project"`
}

type calcArgs struct {
	Kind    string         `json:"kind" jsonschema:"buck, boost, ldo, gate_drive, gate_divider or divider"`
	Part    string         `json:"part,omitempty" jsonschema:"fill device limits and values from this part's datasheet"`
	Project string         `json:"project,omitempty" jsonschema:"also take vin/vout/iout from the part's rails in this project"`
	Inputs  map[string]any `json:"inputs,omitempty" jsonschema:"calculator inputs in SI units (V, A, Hz, s, H, F, Ω, C); see list_calculators"`
	Save    bool           `json:"save,omitempty" jsonschema:"save the result (to the project when given)"`
	Title   string         `json:"title,omitempty"`
	Show    bool           `json:"show,omitempty" jsonschema:"show the result in SheetDiver"`
}

// registerProjects adds the project tools.
func (s *Server) registerProjects() {
	c := s.core

	addTool(s, &mcp.Tool{Name: "list_projects", Description: "List projects (boards/designs) with part and rail counts.", Annotations: readOnly("List projects")},
		read, func(noArgs) string { return "projects" }, func(ctx context.Context, _ noArgs) (result, error) {
			ps, err := c.Lib.Projects(ctx)
			if err != nil {
				return result{}, err
			}
			if len(ps) == 0 {
				return text("no projects yet; create one with create_project"), nil
			}
			active := c.Config.Get().ActiveProject
			var b strings.Builder
			for _, p := range ps {
				mark := ""
				if strings.EqualFold(p.Name, active) {
					mark = " (active)"
				}
				fmt.Fprintf(&b, "%s%s: %d parts, %d rails — %s\n", p.Name, mark, p.Parts, p.Rails, p.Description)
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "create_project", Description: "Create a project to group the parts of a board or design.", Annotations: writes("Create project", false)},
		write, func(a createProjectArgs) string { return "create project " + a.Name }, func(ctx context.Context, a createProjectArgs) (result, error) {
			p, err := c.Lib.CreateProject(ctx, a.Name, a.Description)
			if err != nil {
				return result{}, err
			}
			if a.Activate || c.Config.Get().ActiveProject == "" {
				_ = c.Config.SetActiveProject(p.Name)
			}
			c.ProjectChanged(p.Name, "created")
			return text(fmt.Sprintf("created project %s (#%d). Add rails with set_rail and parts with add_to_project.", p.Name, p.ID)), nil
		})

	addTool(s, &mcp.Tool{Name: "get_project", Description: "A project's BOM, rails with computed load (direct loads plus regulator input currents), capacity and power, input power, voltage/current checks against each part's datasheet limits, saved calculations and notes.", Annotations: readOnly("Get project")},
		read, func(a projectArgs) string { return "project " + a.Project }, func(ctx context.Context, a projectArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			d, err := c.ProjectDetail(ctx, name)
			if err != nil {
				return result{}, err
			}
			if a.Show {
				c.Show(core.Focus{Kind: "project", Project: d.Project.Name})
			}
			return text(projectText(d)), nil
		})

	addTool(s, &mcp.Tool{Name: "set_rail", Description: "Add or update a supply rail of a project (voltage range, current limit, source), or delete it.", Annotations: writes("Set rail", true)},
		write, func(a railArgs) string { return "rail " + a.Name }, func(ctx context.Context, a railArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			p, err := c.Lib.Project(ctx, name)
			if err != nil {
				return result{}, err
			}
			if a.Delete {
				if err := c.Lib.DeleteRail(ctx, p.ID, a.Name); err != nil {
					return result{}, err
				}
				c.ProjectChanged(p.Name, "rail")
				return text("deleted rail " + a.Name), nil
			}
			r, err := c.Lib.SetRail(ctx, library.Rail{ProjectID: p.ID, Name: a.Name, VNom: a.VNom, VMin: a.VMin, VMax: a.VMax, CurrentLimit: a.CurrentLimit, Source: a.Source, Notes: a.Notes})
			if err != nil {
				return result{}, err
			}
			c.ProjectChanged(p.Name, "rail")
			lo, hi := railBounds(r)
			return text(fmt.Sprintf("rail %s: %s (%s–%s)", r.Name, calc.Eng(deref(r.VNom), "V"), calc.Eng(lo, "V"), calc.Eng(hi, "V"))), nil
		})

	addTool(s, &mcp.Tool{Name: "add_to_project", Description: "Place a part in a project with its designator, role, supply rail, output rail (for regulators) and load current. Existing part+designator entries are updated.", Annotations: writes("Add to project", true)},
		write, func(a addPartArgs) string { return "add " + a.Part + " to " + a.Project }, func(ctx context.Context, a addPartArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			pp, err := c.AddToProject(ctx, name, a.Part, library.ProjectPart{Designator: a.Designator, Qty: a.Qty, Role: a.Role, SupplyRail: a.SupplyRail, OutputRail: a.OutputRail, ITyp: a.ITyp, IMax: a.IMax, Notes: a.Notes})
			if err != nil {
				return result{}, err
			}
			return text(fmt.Sprintf("added %s as entry #%d %s in %s (supply %s, output %s). get_project shows the updated budget and checks.", pp.MPN, pp.ID, pp.Designator, name, firstNonEmpty(pp.SupplyRail, "–"), firstNonEmpty(pp.OutputRail, "–"))), nil
		})

	addTool(s, &mcp.Tool{Name: "update_project_part", Description: "Change or remove a BOM entry (by entry id or designator): designator, qty, role, rails, currents, notes.", Annotations: writes("Update project part", true)},
		write, func(a updateEntryArgs) string { return "update entry " + a.Entry }, func(ctx context.Context, a updateEntryArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			p, err := c.Lib.Project(ctx, name)
			if err != nil {
				return result{}, err
			}
			entries, err := c.Lib.ProjectParts(ctx, p.ID)
			if err != nil {
				return result{}, err
			}
			var e *library.ProjectPart
			for i := range entries {
				if strconv.FormatInt(entries[i].ID, 10) == strings.TrimPrefix(a.Entry, "#") || strings.EqualFold(entries[i].Designator, a.Entry) {
					e = &entries[i]
					break
				}
			}
			if e == nil {
				return result{}, fmt.Errorf("no BOM entry %q in %s", a.Entry, p.Name)
			}
			if a.Remove {
				if err := c.Lib.RemoveProjectPart(ctx, p.ID, e.ID); err != nil {
					return result{}, err
				}
				c.ProjectChanged(p.Name, "part")
				return text("removed " + e.MPN + " " + e.Designator), nil
			}
			if a.Designator != nil {
				e.Designator = *a.Designator
			}
			if a.Qty != nil {
				e.Qty = *a.Qty
			}
			if a.Role != nil {
				e.Role = *a.Role
			}
			if a.SupplyRail != nil {
				e.SupplyRail = *a.SupplyRail
			}
			if a.OutputRail != nil {
				e.OutputRail = *a.OutputRail
			}
			if a.ITyp != nil {
				e.ITyp = a.ITyp
			}
			if a.IMax != nil {
				e.IMax = a.IMax
			}
			if a.Notes != nil {
				e.Notes = *a.Notes
			}
			if err := c.Lib.UpdateProjectPart(ctx, *e); err != nil {
				return result{}, err
			}
			c.ProjectChanged(p.Name, "part")
			return text("updated " + e.MPN + " " + e.Designator), nil
		})
}

// registerCalc adds the calculator tools.
func (s *Server) registerCalc() {
	c := s.core

	addTool(s, &mcp.Tool{Name: "list_calculators", Description: "The design calculators, their inputs (SI units) and which inputs are filled from datasheet parameter keys; also lists the canonical parameter keys.", Annotations: readOnly("List calculators")},
		read, func(noArgs) string { return "calculators" }, func(ctx context.Context, _ noArgs) (result, error) {
			kinds := make([]string, 0, len(core.CalcKinds))
			for k := range core.CalcKinds {
				kinds = append(kinds, k)
			}
			sort.Strings(kinds)
			var b strings.Builder
			for _, k := range kinds {
				fmt.Fprintf(&b, "## %s — %s\n", k, core.CalcKinds[k])
				for _, f := range core.CalcFields(k) {
					req := ""
					if f["required"] == "true" {
						req = " (required)"
					}
					fmt.Fprintf(&b, "  %s%s: %s\n", f["name"], req, f["description"])
				}
				b.WriteString("\n")
			}
			b.WriteString("With part set, fields like fsw, vref, ilim, tonMin, vgsTh, qg… are filled from the datasheet. With project set too, vinMin/vinMax come from the part's supply rail and vout/iout from its output rail.\n\nparameter keys:\n")
			for _, k := range sortedKeys() {
				fmt.Fprintf(&b, "  %s: %s\n", k, extract.Keys[k])
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "calculate", Description: "Run a design calculator (buck, boost, ldo, gate_drive, gate_divider, divider). Missing inputs are filled from the part's datasheet and project rails, each with its source; the result lists outputs with formulas, checks against device limits and component suggestions.", Annotations: writes("Calculate", true)},
		read, func(a calcArgs) string { return "calculate " + a.Kind + " " + a.Part }, func(ctx context.Context, a calcArgs) (result, error) {
			if a.Save {
				if err := c.AgentWrite(); err != nil {
					return result{}, err
				}
			}
			project := a.Project
			if project == "" && a.Save {
				project = c.Config.Get().ActiveProject
			}
			raw, _ := json.Marshal(a.Inputs)
			resp, err := c.Calc(ctx, core.CalcRequest{Kind: a.Kind, Part: a.Part, Project: project, Inputs: raw, Save: a.Save, Title: a.Title})
			if err != nil {
				return result{}, err
			}
			if a.Show {
				c.Show(core.Focus{Kind: "calc", Part: resp.Part, Project: resp.Project, Calc: resp})
			}
			return text(calcText(resp)), nil
		})
}

// project resolves a project argument, defaulting to the active project.
func (s *Server) project(ref string) (string, error) {
	if strings.TrimSpace(ref) != "" {
		return ref, nil
	}
	if a := s.core.Config.Get().ActiveProject; a != "" {
		return a, nil
	}
	return "", fmt.Errorf("no project given and none is active; pass project or create one with create_project")
}

// deref reads an optional value (0 when absent).
func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// railBounds is a rail's voltage range.
func railBounds(r library.Rail) (float64, float64) {
	lo, hi := deref(r.VNom), deref(r.VNom)
	if r.VMin != nil {
		lo = *r.VMin
	}
	if r.VMax != nil {
		hi = *r.VMax
	}
	return lo, hi
}

// projectText renders a project analysis.
func projectText(d core.ProjectDetail) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n%s\n\n", d.Project.Name, d.Project.Description)
	b.WriteString("## BOM\n")
	if len(d.Parts) == 0 {
		b.WriteString("(empty — add_to_project)\n")
	}
	for _, p := range d.Parts {
		cur := ""
		if p.ITyp != nil || p.IMax != nil {
			cur = fmt.Sprintf(" draws %s typ / %s max", calc.Eng(deref(p.ITyp), "A"), calc.Eng(deref(p.IMax), "A"))
		}
		v := ""
		if p.Variant != "" {
			v = " (" + p.Variant + ")"
		}
		fmt.Fprintf(&b, "#%d %s ×%d %s%s [%s] %s | supply %s → output %s%s\n", p.ID, firstNonEmpty(p.Designator, "?"), p.Qty, p.MPN, v, p.Category, p.Role, firstNonEmpty(p.SupplyRail, "–"), firstNonEmpty(p.OutputRail, "–"), cur)
	}
	b.WriteString("\n## Rails\n")
	for _, r := range d.Rails {
		lo, hi := railBounds(r.Rail)
		fmt.Fprintf(&b, "%s: %s (%s–%s) from %s | load %s typ / %s max", r.Name, calc.Eng(deref(r.VNom), "V"), calc.Eng(lo, "V"), calc.Eng(hi, "V"), firstNonEmpty(r.SourcePart, r.Source, "external"), calc.Eng(r.LoadTyp, "A"), calc.Eng(r.LoadMax, "A"))
		if r.Capacity > 0 {
			fmt.Fprintf(&b, " of %s (%s, %.0f%%)", calc.Eng(r.Capacity, "A"), r.CapacityFrom, 100*r.LoadMax/r.Capacity)
		}
		fmt.Fprintf(&b, " | %s | consumers: %s\n", calc.Eng(r.Power, "W"), strings.Join(r.Consumers, ", "))
	}
	fmt.Fprintf(&b, "input power at max load: %s\n", calc.Eng(d.InputPower, "W"))
	b.WriteString("\n## Checks\n")
	for _, ch := range d.Checks {
		fmt.Fprintf(&b, "[%s] %s\n", ch.Level, ch.Message)
	}
	if len(d.Calculations) > 0 {
		b.WriteString("\n## Saved calculations\n")
		for _, cl := range d.Calculations {
			var resp core.CalcResponse
			_ = json.Unmarshal(cl.Outputs, &resp)
			fmt.Fprintf(&b, "#%d %s (%s): %s\n", cl.ID, cl.Title, cl.CreatedAt.Format("2006-01-02"), resp.Result.Summary)
		}
	}
	if len(d.Notes) > 0 {
		b.WriteString("\n## Notes\n")
		for _, n := range d.Notes {
			fmt.Fprintf(&b, "#%d (%s): %s\n", n.ID, n.Author, n.Body)
		}
	}
	return b.String()
}

// calcText renders a calculator response.
func calcText(r core.CalcResponse) string {
	var b strings.Builder
	b.WriteString(r.Result.Summary + "\n")
	if len(r.Filled) > 0 {
		b.WriteString("\nfrom the datasheet/project:\n")
		for _, f := range r.Filled {
			fmt.Fprintf(&b, "  %s = %s — %s\n", f.Field, calc.Eng(f.Value, f.Unit), f.From)
		}
	}
	if len(r.Missing) > 0 {
		fmt.Fprintf(&b, "\nnot found on the datasheet (checks skipped; pass in inputs or record_parameters): %s\n", strings.Join(r.Missing, ", "))
	}
	b.WriteString("\noutputs:\n")
	for _, o := range r.Result.Outputs {
		f := ""
		if o.Formula != "" {
			f = "  [" + o.Formula + "]"
		}
		fmt.Fprintf(&b, "  %s (%s) = %s%s\n", o.Name, o.Symbol, calc.Eng(o.Value, o.Unit), f)
	}
	if len(r.Result.Checks) > 0 {
		b.WriteString("\nchecks:\n")
		for _, ch := range r.Result.Checks {
			fmt.Fprintf(&b, "  [%s] %s\n", ch.Level, ch.Message)
		}
	}
	if len(r.Result.Suggestions) > 0 {
		b.WriteString("\nsuggestions:\n")
		for _, sg := range r.Result.Suggestions {
			fmt.Fprintf(&b, "  - %s\n", sg)
		}
	}
	if r.SavedID != 0 {
		fmt.Fprintf(&b, "\nsaved as calculation #%d\n", r.SavedID)
	}
	return b.String()
}

// sortStrings sorts in place.
func sortStrings(s []string) { sort.Strings(s) }
