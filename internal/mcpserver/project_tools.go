package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/catalog"
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

type filterArg struct {
	Key   string `json:"key" jsonschema:"parameter key, e.g. vin, iout, fsw, rdson, iq"`
	Op    string `json:"op" jsonschema:"one of >=, <=, > or <"`
	Value string `json:"value" jsonschema:"a number with an optional unit or SI prefix: '24 V', '3 A', '1 µA', '650k', '30m'"`
	Stat  string `json:"stat,omitempty" jsonschema:"min, typ or max; default: >= tests the highest the part can do (max), <= tests typ"`
}

type compareArgs struct {
	Category string      `json:"category" jsonschema:"component type: buck, boost, ldo, mosfet-n, charger, mcu… (see list_categories)"`
	Keys     []string    `json:"keys,omitempty" jsonschema:"columns to show; default: the type's key parameters"`
	Filters  []filterArg `json:"filters,omitempty" jsonschema:"requirements, all must hold, e.g. [{key: vin, op: '>=', value: '24 V'}, {key: iout, op: '>=', value: '3 A'}]"`
	Query    string      `json:"query,omitempty" jsonschema:"part number, manufacturer or description fragment"`
	Sort     string      `json:"sort,omitempty" jsonschema:"a key to sort by (typ value), or mpn"`
	Desc     bool        `json:"desc,omitempty"`
	Favorite bool        `json:"favorite,omitempty" jsonschema:"only starred parts"`
	Status   string      `json:"status,omitempty" jsonschema:"only parts with this status, e.g. preferred"`
	Show     bool        `json:"show,omitempty" jsonschema:"also open the comparison in SheetDiver"`
}

// registerCompare adds the component-type tools.
func (s *Server) registerCompare() {
	c := s.core
	addTool(s, &mcp.Tool{Name: "list_categories", Description: "Component types in the library with their part counts and the key parameters compared for each.", Annotations: readOnly("List categories")},
		read, func(noArgs) string { return "categories" }, func(ctx context.Context, _ noArgs) (result, error) {
			cs, err := c.Categories(ctx)
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			for _, x := range cs {
				fmt.Fprintf(&b, "%s: %d parts", firstNonEmpty(x.Category, "(uncategorized)"), x.Count)
				if len(x.Keys) > 0 {
					fmt.Fprintf(&b, " — compares %s", strings.Join(x.Keys, ", "))
				}
				b.WriteString("\n")
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "compare_parts", Description: "Compare the parts of one component type side by side on their key parameters (SI values with min/typ/max), filtered by requirements (vin >= 24 V, iout >= 3 A, iq <= 1 µA…) and sorted. Use it to pick a part for a design from the library.", Annotations: readOnly("Compare parts")},
		read, func(a compareArgs) string { return "compare " + a.Category }, func(ctx context.Context, a compareArgs) (result, error) {
			req := core.CompareRequest{Category: a.Category, Keys: a.Keys, Query: a.Query, Sort: a.Sort, Desc: a.Desc, Favorite: a.Favorite, Status: a.Status}
			for _, f := range a.Filters {
				v, err := core.ParseQuantity(f.Value)
				if err != nil {
					return result{}, fmt.Errorf("filter %s: %w", f.Key, err)
				}
				req.Filters = append(req.Filters, core.Filter{Key: f.Key, Op: f.Op, Value: v, Stat: f.Stat})
			}
			res, err := c.Compare(ctx, req)
			if err != nil {
				return result{}, err
			}
			if a.Show {
				c.Show(core.Focus{Kind: "category", Query: a.Category})
			}
			return text(compareText(res)), nil
		})
}

type altArgs struct {
	Part        string   `json:"part" jsonschema:"the part to replace (MPN, orderable code or id)"`
	Keys        []string `json:"keys,omitempty" jsonschema:"compare only these keys (default: the type's comparison keys the part has)"`
	Discouraged bool     `json:"discouraged,omitempty" jsonschema:"also list parts marked avoid, nrnd or eol"`
	Limit       int      `json:"limit,omitempty" jsonschema:"max candidates (default 10)"`
	Show        bool     `json:"show,omitempty" jsonschema:"also open the part's alternatives in SheetDiver"`
}

type reportArgs struct {
	Project string `json:"project"`
}

type updateArgs struct {
	Part string `json:"part,omitempty" jsonschema:"check the datasheet of this part; omit to check every datasheet in the library"`
}

type catalogFilter struct {
	Parameter string `json:"parameter" jsonschema:"a parameter name from catalog_parameters, such as 'Drain to Source Voltage', 'RDS(on)' or 'Package'"`
	Op        string `json:"op" jsonschema:"one of >=, <= (numbers) or = (text values and Package)"`
	Value     string `json:"value" jsonschema:"for numbers a value with unit like 30V, 10mΩ, 4.7u, 3A; for text and Package one or more values separated by |, like N-Channel or SOT-23|SOT-23-3"`
}

type catalogArgs struct {
	Query       string          `json:"query,omitempty" jsonschema:"words that must all appear in the part number, maker, package, category or description, such as 'buck 3A SOT-23' or '10k 0402' or 'TPS563200'"`
	InStock     bool            `json:"in_stock,omitempty" jsonschema:"only parts JLCPCB has in stock"`
	Basic       bool            `json:"basic,omitempty" jsonschema:"only JLCPCB basic/preferred parts (no extended-part setup fee)"`
	Subcategory string          `json:"subcategory,omitempty" jsonschema:"exact JLCPCB category from catalog_parameters, such as 'DC-DC Converters' or 'MOSFETs'"`
	Filters     []catalogFilter `json:"filters,omitempty" jsonschema:"parameter requirements (need a subcategory or query). For ranges such as an input voltage range, >= 24V means the part accepts 24 V"`
	Sort        string          `json:"sort,omitempty" jsonschema:"stock (default), price, mpn, or a parameter name to sort by it"`
	Desc        bool            `json:"desc,omitempty"`
	Limit       int             `json:"limit,omitempty" jsonschema:"max results (default 20)"`
	Offset      int             `json:"offset,omitempty" jsonschema:"skip this many results (paging)"`
}

type catalogParamsArgs struct {
	Subcategory string          `json:"subcategory,omitempty" jsonschema:"a JLCPCB category; omit to list the categories"`
	Query       string          `json:"query,omitempty" jsonschema:"without a subcategory: only categories containing this text"`
	InStock     bool            `json:"in_stock,omitempty"`
	Filters     []catalogFilter `json:"filters,omitempty" jsonschema:"with a subcategory: summarize each parameter over the parts that pass the other filters (e.g. the packages still available after Drain to Source Voltage >= 30V)"`
}

// registerDesign adds alternatives, reports, datasheet updates and the
// parts catalogue.
func (s *Server) registerDesign() {
	c := s.core
	addTool(s, &mcp.Tool{Name: "search_catalog", Description: "Search the JLCPCB/LCSC parts catalogue (about 2.5 million parts, if the user downloaded it in Settings) for parts to use: by words and/or a category with parameter filters (get a category's parameters from catalog_parameters), sorted by stock, price or a parameter. Results show stock, JLC basic/extended library, price, package, the filtered parameters and whether the library already has the part. Import the datasheets of the ones worth considering with import_datasheets lcsc=[...].", Annotations: readOnly("Search parts catalogue")},
		read, func(a catalogArgs) string { return "catalog " + a.Query }, func(ctx context.Context, a catalogArgs) (result, error) {
			if a.Limit == 0 {
				a.Limit = 20
			}
			q := catalog.Query{Text: a.Query, InStock: a.InStock, Basic: a.Basic, Subcategory: a.Subcategory, Limit: a.Limit, Offset: a.Offset, Desc: a.Desc}
			switch a.Sort {
			case "", "stock", "price", "mpn":
				q.Sort = a.Sort
			default:
				q.Sort = "attr:" + a.Sort
			}
			var cols []string
			for _, f := range a.Filters {
				q.Attrs = append(q.Attrs, catalog.AttrFilter{Key: f.Parameter, Op: f.Op, Input: f.Value})
				cols = append(cols, f.Parameter)
			}
			if k := strings.TrimPrefix(q.Sort, "attr:"); k != q.Sort && !slices.Contains(cols, k) {
				cols = append(cols, k)
			}
			items, err := c.CatalogSearch(ctx, q)
			if err != nil {
				return result{}, err
			}
			st := c.CatalogStatus(ctx)
			var b strings.Builder
			fmt.Fprintf(&b, "%d parts (catalogue published %s", len(items), st.Published.Format("2006-01-02"))
			if st.Stale {
				fmt.Fprintf(&b, ", %d days old: suggest the user refreshes it", st.AgeDays)
			}
			b.WriteString(")\n| LCSC | part | maker | package | JLC | stock | $/1 | in library |")
			for _, k := range cols {
				b.WriteString(" " + k + " |")
			}
			b.WriteString(" description |\n|---|---|---|---|---|---:|---:|---|" + strings.Repeat("---|", len(cols)) + "---|\n")
			for _, it := range items {
				fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %d | %.4g | %s |", it.LCSC, it.MPN, it.Manufacturer, it.Package, it.Library, it.Stock, it.Price, firstNonEmpty(it.InLibrary, "–"))
				for _, k := range cols {
					fmt.Fprintf(&b, " %s |", firstNonEmpty(it.Attributes[k], "–"))
				}
				fmt.Fprintf(&b, " %s |\n", strings.ReplaceAll(it.Description, "|", "/"))
			}
			if len(items) == q.Limit {
				fmt.Fprintf(&b, "(more results: offset %d)\n", q.Offset+len(items))
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "catalog_parameters", Description: "Browse the JLCPCB parts catalogue's structure: without a subcategory, its categories with part and in-stock counts; with one, the parameters its parts have (Package first, then numeric ones with unit and range, or text with the common values) to use as search_catalog filters. Pass filters to see what remains available under them (faceted: each parameter over the parts passing the other filters).", Annotations: readOnly("Catalogue parameters")},
		read, func(a catalogParamsArgs) string { return "catalog parameters " + a.Subcategory }, func(ctx context.Context, a catalogParamsArgs) (result, error) {
			var b strings.Builder
			if a.Subcategory == "" {
				cats, err := c.CatalogCategories(ctx)
				if err != nil {
					return result{}, err
				}
				q := strings.ToLower(a.Query)
				b.WriteString("category › subcategory: parts (in stock)\n")
				for _, x := range cats {
					if q != "" && !strings.Contains(strings.ToLower(x.Category+" "+x.Subcategory), q) {
						continue
					}
					if a.InStock && x.InStock == 0 {
						continue
					}
					fmt.Fprintf(&b, "%s › %s: %d (%d)\n", x.Category, x.Subcategory, x.Parts, x.InStock)
				}
				return text(b.String()), nil
			}
			q := catalog.Query{Subcategory: a.Subcategory, InStock: a.InStock}
			for _, f := range a.Filters {
				q.Attrs = append(q.Attrs, catalog.AttrFilter{Key: f.Parameter, Op: f.Op, Input: f.Value})
			}
			ps, err := c.CatalogParams(ctx, q)
			if err != nil {
				return result{}, err
			}
			fmt.Fprintf(&b, "parameters of %s (how many parts have each):\n", a.Subcategory)
			for _, p := range ps {
				if p.Kind == "number" {
					fmt.Fprintf(&b, "- %s [number, %s] %d parts, %s … %s\n", p.Key, firstNonEmpty(p.Unit, "count"), p.Count, calc.Eng(p.Min, p.Unit), calc.Eng(p.Max, p.Unit))
					continue
				}
				var vs []string
				shown := 12
				if p.Key == catalog.PackageKey {
					shown = 30
				}
				for _, v := range p.Values[:min(shown, len(p.Values))] {
					vs = append(vs, fmt.Sprintf("%s (%d)", v.Value, v.Count))
				}
				fmt.Fprintf(&b, "- %s [text] %d parts: %s\n", p.Key, p.Count, strings.Join(vs, ", "))
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "find_alternatives", Description: "Find library parts that could replace a part (second source, better spec, part out of stock): same or related type, compared key by key in the direction that matters (more output current, less quiescent current, input range covering the original's, matching reference voltage). Drop-ins (nothing worse) first; preferred parts win ties.", Annotations: readOnly("Find alternatives")},
		read, func(a altArgs) string { return "alternatives " + a.Part }, func(ctx context.Context, a altArgs) (result, error) {
			if a.Limit == 0 {
				a.Limit = 10
			}
			res, err := c.Alternatives(ctx, core.AltRequest{Part: a.Part, Keys: a.Keys, Discouraged: a.Discouraged, Limit: a.Limit})
			if err != nil {
				return result{}, err
			}
			if a.Show {
				c.Show(core.Focus{Kind: "part", Part: res.Part.MPN, Tab: "alternatives"})
			}
			return text(altText(res)), nil
		})

	addTool(s, &mcp.Tool{Name: "export_project_report", Description: "A project as a Markdown design-review document: findings (errors and warnings), rails with load and capacity, parts with status and key parameters, saved calculations and notes. Hand it to the user or summarize it.", Annotations: readOnly("Project report")},
		read, func(a reportArgs) string { return "report " + a.Project }, func(ctx context.Context, a reportArgs) (result, error) {
			md, err := c.ProjectReport(ctx, a.Project)
			return text(md), err
		})

	addTool(s, &mcp.Tool{Name: "check_datasheet_updates", Description: "Download a datasheet again from its source (or by part number) and report whether a newer revision exists, with the spec rows that changed. Without a part, checks every datasheet. Apply with apply_datasheet_update.", Annotations: &mcp.ToolAnnotations{Title: "Check datasheet updates", ReadOnlyHint: true, OpenWorldHint: boolPtr(true)}},
		importing, func(a updateArgs) string { return "check updates " + a.Part }, func(ctx context.Context, a updateArgs) (result, error) {
			var checks []core.UpdateCheck
			if a.Part == "" {
				all, err := c.CheckAllUpdates(ctx)
				if err != nil {
					return result{}, err
				}
				checks = all
			} else {
				d, err := c.PartDetail(ctx, a.Part)
				if err != nil {
					return result{}, err
				}
				if d.Datasheet == nil {
					return result{}, fmt.Errorf("%s has no datasheet", a.Part)
				}
				ch, err := c.CheckDatasheetUpdate(ctx, d.Datasheet.ID)
				if err != nil {
					return result{}, err
				}
				checks = []core.UpdateCheck{ch}
			}
			var b strings.Builder
			for _, ch := range checks {
				b.WriteString(updateText(ch))
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "apply_datasheet_update", Description: "Replace a part's datasheet with the newer revision found by check_datasheet_updates. Same datasheet, parts and projects; extracted data is replaced; recorded data is kept (check any that cite pages).", Annotations: &mcp.ToolAnnotations{Title: "Apply datasheet update", DestructiveHint: boolPtr(false)}},
		importing, func(a updateArgs) string { return "apply update " + a.Part }, func(ctx context.Context, a updateArgs) (result, error) {
			d, err := c.PartDetail(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			if d.Datasheet == nil {
				return result{}, fmt.Errorf("%s has no datasheet", a.Part)
			}
			res, err := c.ApplyDatasheetUpdate(ctx, d.Datasheet.ID)
			if err != nil {
				return result{}, err
			}
			return text(fmt.Sprintf("updated datasheet #%d to %s (was %s): %v", res.Datasheet.ID, res.Datasheet.Meta["revision"], res.Datasheet.Meta["previousRevision"], res.Counts)), nil
		})
}

// altText renders alternatives as a table.
func altText(r core.AltResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "alternatives to %s (%s), compared on %s; %d candidates considered\n", r.Part.MPN, r.Category, strings.Join(r.Keys, ", "), r.Considered)
	if len(r.Alternatives) == 0 {
		b.WriteString("none in the library: import more parts of this type (import_datasheets) and try again\n")
		return b.String()
	}
	b.WriteString("| part | fit | ")
	b.WriteString(strings.Join(r.Keys, " | "))
	b.WriteString(" |\n|---|---|" + strings.Repeat("---|", len(r.Keys)) + "\n")
	mark := map[string]string{core.VerdictBetter: "▲", core.VerdictSame: "=", core.VerdictWorse: "▼", core.VerdictUnknown: "?"}
	for _, a := range r.Alternatives {
		fit := fmt.Sprintf("%d worse, %d unknown", a.Worse, a.Unknown)
		if a.DropIn {
			fit = "drop-in"
		}
		name := a.Part.MPN
		if a.Part.Status != "" {
			name += " [" + strings.ToUpper(a.Part.Status) + "]"
		}
		if a.SameDatasheet {
			name += " (same datasheet)"
		}
		fmt.Fprintf(&b, "| %s | %s |", name, fit)
		for _, ch := range a.Checks {
			fmt.Fprintf(&b, " %s %s |", mark[ch.Verdict], ch.Candidate)
		}
		b.WriteString("\n")
	}
	if len(r.Alternatives) > 0 {
		fmt.Fprintf(&b, "\n%s's values: ", r.Part.MPN)
		var vs []string
		for _, ch := range r.Alternatives[0].Checks {
			vs = append(vs, ch.Key+" "+ch.Original)
		}
		b.WriteString(strings.Join(vs, ", ") + "\n▲ better, = same, ▼ worse, ? unknown\n")
	}
	return b.String()
}

// updateText renders one update check.
func updateText(ch core.UpdateCheck) string {
	var b strings.Builder
	switch {
	case ch.Error != "":
		fmt.Fprintf(&b, "#%d %s: could not check: %s\n", ch.DatasheetID, ch.Filename, ch.Error)
	case ch.UpToDate:
		fmt.Fprintf(&b, "#%d %s: up to date (%s)\n", ch.DatasheetID, ch.Filename, ch.Current)
	default:
		fmt.Fprintf(&b, "#%d %s: NEW REVISION %s (have %s): %d changed, %d added, %d removed spec rows; %d recorded values kept on update\n",
			ch.DatasheetID, ch.Filename, ch.Latest, ch.Current, ch.Counts[core.ChangeChanged], ch.Counts[core.ChangeAdded], ch.Counts[core.ChangeRemoved], ch.Recorded)
		for _, x := range ch.Changes {
			fmt.Fprintf(&b, "  %s %s%s [%s]", x.Kind, keyTag(x.Key), x.Name, x.Section)
			if x.Conditions != "" {
				fmt.Fprintf(&b, " (%s)", x.Conditions)
			}
			fmt.Fprintf(&b, ": %s → %s\n", firstNonEmpty(x.Before, "–"), firstNonEmpty(x.After, "–"))
		}
	}
	return b.String()
}

// keyTag renders "[key] " for keyed rows.
func keyTag(k string) string {
	if k == "" {
		return ""
	}
	return "[" + k + "] "
}

// compareText renders a comparison as a markdown table.
func compareText(r core.CompareResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d of %d parts match\n", r.Category, len(r.Rows), r.Total)
	for k, n := range r.Unknown {
		fmt.Fprintf(&b, "(%d parts excluded only because they have no %s value)\n", n, k)
	}
	b.WriteString("\n| part | manufacturer | status |")
	for _, col := range r.Columns {
		fmt.Fprintf(&b, " %s |", col.Key)
	}
	b.WriteString("\n|---|---|---|" + strings.Repeat("---|", len(r.Columns)) + "\n")
	for _, row := range r.Rows {
		st := row.Part.Status
		if row.Part.Favorite {
			st = strings.TrimSpace("★ " + st)
		}
		fmt.Fprintf(&b, "| %s | %s | %s |", row.Part.MPN, row.Part.Manufacturer, st)
		for _, col := range r.Columns {
			v, ok := row.Values[col.Key]
			if !ok {
				b.WriteString(" – |")
				continue
			}
			fmt.Fprintf(&b, " %s |", rangeText(v))
		}
		b.WriteString("\n")
	}
	b.WriteString("\nValues are min/typ/max in SI with prefixes; (r) marks absolute-maximum ratings. get_parameters shows conditions and pages.")
	return b.String()
}

// rangeText renders a compared value compactly.
func rangeText(v core.CompareValue) string {
	e := func(x *float64) string {
		if x == nil {
			return "–"
		}
		return calc.Eng(*x, v.Unit)
	}
	s := ""
	switch {
	case v.Min != nil && v.Max != nil && v.Typ == nil:
		s = e(v.Min) + " … " + e(v.Max)
	case v.Min == nil && v.Max == nil:
		s = e(v.Typ)
	default:
		s = e(v.Min) + " / " + e(v.Typ) + " / " + e(v.Max)
	}
	if v.Rating {
		s += " (r)"
	}
	return s
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
