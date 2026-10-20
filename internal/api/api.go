// Package api is the JSON method table the SheetDiver frontend calls. The
// same table is reached through Wails bindings in the desktop app and
// through POST /api/invoke in browser ("serve") mode. UI writes are
// recorded with source "user".
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"

	"github.com/aitakemyjob/sheetdiver/internal/catalog"
	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// handler is one API method.
type handler func(ctx context.Context, raw json.RawMessage) (any, error)

// API is the method table.
type API struct {
	core    *core.Service
	methods map[string]handler
	// PickPDFs is provided by the desktop shell; nil in browser mode.
	PickPDFs func() ([]string, error)
	// PickFolder chooses a folder of datasheets (desktop only).
	PickFolder func() (string, error)
	// OpenFile opens a local file in its default application (desktop
	// only; the browser opens PDFs in a tab instead).
	OpenFile func(path string) error
	// SaveFile asks where to save a file and writes it (desktop only).
	SaveFile func(name string, data []byte) (string, error)
}

// method adapts a typed function into a handler.
func method[T any](fn func(context.Context, T) (any, error)) handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var in T
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, fmt.Errorf("bad arguments: %w", err)
			}
		}
		return fn(ctx, in)
	}
}

// New builds the method table over a core service.
func New(c *core.Service) *API {
	a := &API{core: c, methods: map[string]handler{}}
	a.registerApp()
	a.registerLibrary()
	a.registerProjects()
	return a
}

// Methods lists available method names (used by tests and docs).
func (a *API) Methods() []string {
	out := make([]string, 0, len(a.methods))
	for k := range a.methods {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Invoke runs a method with JSON arguments and returns JSON.
func (a *API) Invoke(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	h, ok := a.methods[name]
	if !ok {
		return nil, fmt.Errorf("unknown method %q", name)
	}
	out, err := h(ctx, args)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}

// AppState is what the UI needs at startup.
type AppState struct {
	Version     string            `json:"version"`
	Platform    string            `json:"platform"`
	Config      config.Config     `json:"config"`
	LibraryDir  string            `json:"libraryDir"`
	MCP         core.MCPStatus    `json:"mcp"`
	CanPickFile bool              `json:"canPickFile"`
	CanOpenFile bool              `json:"canOpenFile"`
	CanSaveFile bool              `json:"canSaveFile"`
	Keys        map[string]string `json:"keys"`
	Statuses    map[string]string `json:"statuses"`
	Calculators map[string]string `json:"calculators"`
}

// idReq names a row by ID.
type idReq struct {
	ID int64 `json:"id"`
}

// partReq names a part.
type partReq struct {
	Part string `json:"part"`
}

// registerApp adds app state, settings, activity and file methods.
func (a *API) registerApp() {
	c := a.core
	a.methods["app.state"] = method(func(ctx context.Context, _ struct{}) (any, error) {
		return AppState{
			Version: c.Version, Platform: runtime.GOOS, Config: c.Config.Get(), LibraryDir: c.Config.LibraryDir(), MCP: c.MCPStatus(),
			CanPickFile: a.PickPDFs != nil, CanOpenFile: a.OpenFile != nil, Statuses: library.Statuses, CanSaveFile: a.SaveFile != nil, Keys: extract.Keys, Calculators: core.CalcKinds,
		}, nil
	})
	a.methods["settings.update"] = method(func(ctx context.Context, in config.Settings) (any, error) {
		return nil, c.UpdateSettings(in)
	})
	a.methods["activity.list"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.Activity(), nil })
	a.methods["ui.context"] = method(func(ctx context.Context, in core.UIContext) (any, error) {
		c.SetUIContext(in)
		return nil, nil
	})
	a.methods["files.pickPDFs"] = method(func(ctx context.Context, _ struct{}) (any, error) {
		if a.PickPDFs == nil {
			return nil, fmt.Errorf("the file picker needs the desktop app; drop PDFs on the window instead")
		}
		paths, err := a.PickPDFs()
		return map[string]any{"paths": paths}, err
	})
	a.methods["files.pickFolder"] = method(func(ctx context.Context, _ struct{}) (any, error) {
		if a.PickFolder == nil {
			return nil, fmt.Errorf("the folder picker needs the desktop app; type the folder path instead")
		}
		p, err := a.PickFolder()
		return map[string]string{"path": p}, err
	})
	a.methods["file.save"] = method(func(ctx context.Context, in struct {
		Name string `json:"name"`
		Data string `json:"data"` // base64
	}) (any, error) {
		if a.SaveFile == nil {
			return nil, fmt.Errorf("saving files needs the desktop app; the browser downloads them instead")
		}
		data, err := base64.StdEncoding.DecodeString(in.Data)
		if err != nil {
			return nil, err
		}
		p, err := a.SaveFile(in.Name, data)
		return map[string]string{"path": p}, err
	})
}

// registerLibrary adds datasheet, part and extracted-content methods.
func (a *API) registerLibrary() {
	c := a.core
	a.methods["datasheets.import"] = method(func(ctx context.Context, in core.ImportRequest) (any, error) {
		in.Data = nil // uploads go through /api/upload
		return c.Import(ctx, in)
	})
	a.methods["imports.start"] = method(func(ctx context.Context, in struct {
		Paths     []string `json:"paths"`
		URLs      []string `json:"urls"`
		MPNs      []string `json:"mpns"`
		Folder    string   `json:"folder"`
		Recursive bool     `json:"recursive"`
	}) (any, error) {
		var reqs []core.ImportRequest
		for _, p := range in.Paths {
			reqs = append(reqs, core.ImportRequest{Path: p})
		}
		for _, u := range in.URLs {
			reqs = append(reqs, core.ImportRequest{URL: u})
		}
		for _, m := range in.MPNs {
			reqs = append(reqs, core.ImportRequest{Lookup: m})
		}
		if in.Folder != "" {
			fr, err := core.FolderRequests(in.Folder, in.Recursive)
			if err != nil {
				return nil, err
			}
			if len(fr) == 0 {
				return nil, fmt.Errorf("no PDFs in %s", in.Folder)
			}
			reqs = append(reqs, fr...)
		}
		if len(reqs) == 0 {
			return nil, fmt.Errorf("nothing to import")
		}
		return c.StartImports(reqs), nil
	})
	a.methods["imports.list"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.ImportJobs(), nil })
	a.methods["imports.retry"] = method(func(ctx context.Context, in struct {
		ID int `json:"id"`
	}) (any, error) {
		return c.RetryImport(in.ID)
	})
	a.methods["imports.clear"] = method(func(ctx context.Context, _ struct{}) (any, error) {
		c.ClearImports()
		return nil, nil
	})
	a.methods["bom.import"] = method(func(ctx context.Context, in struct {
		Text     string `json:"text"`
		Path     string `json:"path"`
		Filename string `json:"filename"`
		Project  string `json:"project"`
		Fetch    bool   `json:"fetch"`
	}) (any, error) {
		return c.ImportBOM(ctx, core.BOMRequest{Data: []byte(in.Text), Path: in.Path, Filename: in.Filename, Project: in.Project, Fetch: in.Fetch})
	})
	a.methods["translate.status"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.TranslateStatus(), nil })
	a.methods["translate.download"] = method(func(ctx context.Context, in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}) (any, error) {
		if err := c.StartTranslateDownload(in.From, in.To); err != nil {
			return nil, err
		}
		return c.TranslateStatus(), nil
	})
	a.methods["translate.remove"] = method(func(ctx context.Context, in struct {
		From string `json:"from"`
		To   string `json:"to"`
	}) (any, error) {
		if err := c.Translate.Remove(in.From, in.To); err != nil {
			return nil, err
		}
		return c.TranslateStatus(), nil
	})
	a.methods["translations.get"] = method(func(ctx context.Context, in struct {
		Lang  string   `json:"lang"`
		Texts []string `json:"texts"`
	}) (any, error) {
		return c.Translations(ctx, in.Lang, in.Texts)
	})
	a.methods["translations.save"] = method(func(ctx context.Context, in struct {
		Items []library.Translation `json:"items"`
	}) (any, error) {
		return nil, c.SaveTranslations(ctx, in.Items)
	})
	a.methods["catalog.status"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.CatalogStatus(ctx), nil })
	a.methods["catalog.download"] = method(func(ctx context.Context, _ struct{}) (any, error) {
		if err := c.StartCatalogDownload(); err != nil {
			return nil, err
		}
		return c.CatalogStatus(ctx), nil
	})
	a.methods["catalog.remove"] = method(func(ctx context.Context, _ struct{}) (any, error) {
		if err := c.Catalog.Remove(); err != nil {
			return nil, err
		}
		return c.CatalogStatus(ctx), nil
	})
	a.methods["catalog.search"] = method(func(ctx context.Context, in catalog.Query) (any, error) { return c.CatalogSearch(ctx, in) })
	a.methods["catalog.params"] = method(func(ctx context.Context, in catalog.Query) (any, error) { return c.CatalogParams(ctx, in) })
	a.methods["catalog.categories"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.CatalogCategories(ctx) })
	a.methods["catalog.subcategories"] = method(func(ctx context.Context, _ struct{}) (any, error) {
		m, err := c.Catalog.Subcategories(ctx)
		if err != nil {
			return nil, err
		}
		return catalog.TopSubcategories(m, 300), nil
	})
	a.methods["catalog.openShop"] = method(func(ctx context.Context, in struct {
		LCSC string `json:"lcsc"`
		Shop string `json:"shop"`
	}) (any, error) {
		// The URL is built here from the part code, so only LCSC and
		// JLCPCB part pages can be opened.
		u, err := catalog.ShopURL(in.LCSC, in.Shop)
		if err != nil {
			return nil, err
		}
		if a.OpenFile == nil {
			return nil, fmt.Errorf("opening the browser needs the desktop app; open %s", u)
		}
		return map[string]string{"url": u}, a.OpenFile(u)
	})
	a.methods["catalog.import"] = method(func(ctx context.Context, in struct {
		LCSC []string `json:"lcsc"`
	}) (any, error) {
		return c.CatalogImport(ctx, in.LCSC)
	})
	a.methods["datasheets.list"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.Lib.Datasheets(ctx) })
	a.methods["datasheet.get"] = method(func(ctx context.Context, in idReq) (any, error) {
		d, err := c.Lib.Datasheet(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		pages, _ := c.Lib.PageSizes(ctx, in.ID)
		parts, _ := c.Lib.PartsOfDatasheet(ctx, in.ID)
		return map[string]any{"datasheet": d, "pages": pages, "parts": parts}, nil
	})
	a.methods["datasheet.update"] = method(func(ctx context.Context, in library.Datasheet) (any, error) {
		err := c.Lib.UpdateDatasheet(ctx, in)
		c.Changed("datasheet", in.ID)
		return nil, err
	})
	a.methods["datasheet.open"] = method(func(ctx context.Context, in idReq) (any, error) {
		if a.OpenFile == nil {
			return nil, fmt.Errorf("opening files needs the desktop app; the browser opens the PDF in a tab")
		}
		d, err := c.Lib.Datasheet(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		p := c.Lib.DatasheetPath(d.File)
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("the PDF for %s is missing from the library folder: %w", d.Filename, err)
		}
		return map[string]string{"path": p}, a.OpenFile(p)
	})
	a.methods["datasheet.delete"] = method(func(ctx context.Context, in idReq) (any, error) { return nil, c.DeleteDatasheet(ctx, in.ID) })
	a.methods["datasheet.reextract"] = method(func(ctx context.Context, in struct {
		ID         int64 `json:"id"`
		Translated bool  `json:"translated"`
	}) (any, error) {
		return c.Reextract(ctx, in.ID, in.Translated)
	})
	a.methods["datasheet.translationSources"] = method(func(ctx context.Context, in idReq) (any, error) { return c.TranslationSources(ctx, in.ID) })
	a.methods["page.get"] = method(func(ctx context.Context, in struct {
		DatasheetID int64 `json:"datasheetId"`
		Page        int   `json:"page"`
	}) (any, error) {
		return c.Lib.Page(ctx, in.DatasheetID, in.Page)
	})

	a.methods["parts.list"] = method(func(ctx context.Context, in library.PartFilter) (any, error) { return c.Lib.Parts(ctx, in) })
	a.methods["parts.categories"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.Lib.Categories(ctx) })
	a.methods["categories.list"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.Categories(ctx) })
	a.methods["compare"] = method(func(ctx context.Context, in core.CompareRequest) (any, error) { return c.Compare(ctx, in) })
	a.methods["part.alternatives"] = method(func(ctx context.Context, in core.AltRequest) (any, error) { return c.Alternatives(ctx, in) })
	a.methods["project.report"] = method(func(ctx context.Context, in struct {
		Project string `json:"project"`
	}) (any, error) {
		md, err := c.ProjectReport(ctx, in.Project)
		return map[string]string{"markdown": md}, err
	})
	a.methods["datasheet.checkUpdate"] = method(func(ctx context.Context, in idReq) (any, error) { return c.CheckDatasheetUpdate(ctx, in.ID) })
	a.methods["datasheet.applyUpdate"] = method(func(ctx context.Context, in idReq) (any, error) { return c.ApplyDatasheetUpdate(ctx, in.ID) })
	a.methods["datasheets.checkUpdates"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.CheckAllUpdates(ctx) })
	a.methods["part.detail"] = method(func(ctx context.Context, in partReq) (any, error) { return c.PartDetail(ctx, in.Part) })
	a.methods["part.update"] = method(func(ctx context.Context, in struct {
		ID int64 `json:"id"`
		library.PartUpdate
	}) (any, error) {
		p, err := c.Lib.UpdatePart(ctx, in.ID, in.PartUpdate)
		c.Changed("part", p.DatasheetID)
		return p, err
	})
	a.methods["part.add"] = method(func(ctx context.Context, in library.Part) (any, error) {
		p, err := c.Lib.AddPart(ctx, in)
		c.Changed("part", p.DatasheetID)
		return p, err
	})
	// Deleting from the library is the user's alone (no MCP tool): the UI
	// shows the plan and asks for confirmation before applying it.
	a.methods["delete.preview"] = method(func(ctx context.Context, in partReq) (any, error) { return c.DeletePreview(ctx, in.Part) })
	a.methods["delete.apply"] = method(func(ctx context.Context, in struct {
		Part           string `json:"part"`
		KeepInProjects bool   `json:"keepInProjects"`
	}) (any, error) {
		return c.DeletePart(ctx, in.Part, in.KeepInProjects)
	})
	a.methods["part.delete"] = method(func(ctx context.Context, in idReq) (any, error) {
		err := c.Lib.DeletePart(ctx, in.ID)
		c.Changed("part", 0)
		return nil, err
	})
	a.methods["part.variants"] = method(func(ctx context.Context, in struct {
		ID       int64             `json:"id"`
		Variants []library.Variant `json:"variants"`
	}) (any, error) {
		return nil, c.Lib.SetVariants(ctx, in.ID, in.Variants, library.SourceUser)
	})

	a.methods["parameters.list"] = method(func(ctx context.Context, in struct {
		DatasheetID int64 `json:"datasheetId"`
		library.ParamFilter
	}) (any, error) {
		return c.Lib.Parameters(ctx, in.DatasheetID, in.ParamFilter)
	})
	a.methods["parameters.record"] = method(func(ctx context.Context, in struct {
		DatasheetID int64               `json:"datasheetId"`
		Parameters  []library.Parameter `json:"parameters"`
	}) (any, error) {
		out, err := c.Lib.RecordParameters(ctx, in.DatasheetID, in.Parameters, library.SourceUser, false)
		c.Changed("parameters", in.DatasheetID)
		return out, err
	})
	a.methods["parameters.delete"] = method(func(ctx context.Context, in struct {
		DatasheetID int64   `json:"datasheetId"`
		IDs         []int64 `json:"ids"`
	}) (any, error) {
		err := c.Lib.DeleteParameters(ctx, in.DatasheetID, in.IDs)
		c.Changed("parameters", in.DatasheetID)
		return nil, err
	})
	a.methods["sections.list"] = method(func(ctx context.Context, in idReq) (any, error) { return c.Lib.Sections(ctx, in.ID, false) })
	a.methods["section.get"] = method(func(ctx context.Context, in idReq) (any, error) { return c.Lib.Section(ctx, in.ID) })
	a.methods["figures.list"] = method(func(ctx context.Context, in struct {
		DatasheetID int64 `json:"datasheetId"`
		library.FigureFilter
	}) (any, error) {
		return c.Lib.Figures(ctx, in.DatasheetID, in.FigureFilter)
	})
	a.methods["figure.get"] = method(func(ctx context.Context, in idReq) (any, error) {
		f, err := c.Lib.Figure(ctx, in.ID)
		if err != nil {
			return nil, err
		}
		notes, _ := c.Lib.Notes(ctx, library.TargetFigure, fmt.Sprint(f.ID))
		txt, _ := c.Lib.RegionText(ctx, f.DatasheetID, f.Page, f.Box)
		return map[string]any{"figure": f, "notes": notes, "text": txt}, nil
	})
	a.methods["figure.update"] = method(func(ctx context.Context, in struct {
		ID int64 `json:"id"`
		library.FigureUpdate
	}) (any, error) {
		f, err := c.Lib.UpdateFigure(ctx, in.ID, in.FigureUpdate, library.SourceUser)
		c.Changed("figure", f.DatasheetID)
		return f, err
	})
	a.methods["figure.add"] = method(func(ctx context.Context, in library.Figure) (any, error) {
		in.Source = library.SourceUser
		f, err := c.Lib.AddFigure(ctx, in)
		c.Changed("figure", in.DatasheetID)
		return f, err
	})
	a.methods["tables.list"] = method(func(ctx context.Context, in idReq) (any, error) { return c.Lib.Tables(ctx, in.ID, true) })
	a.methods["table.record"] = method(func(ctx context.Context, in struct {
		ID int64 `json:"id"`
		library.TableUpdate
	}) (any, error) {
		t, err := c.Lib.RecordTable(ctx, in.ID, in.TableUpdate, library.SourceUser)
		c.Changed("table", t.DatasheetID)
		return t, err
	})
	a.methods["pins.list"] = method(func(ctx context.Context, in idReq) (any, error) { return c.Lib.Pins(ctx, in.ID, "") })
	a.methods["registers.list"] = method(func(ctx context.Context, in idReq) (any, error) { return c.Lib.Registers(ctx, in.ID) })
	a.methods["recommendations.list"] = method(func(ctx context.Context, in struct {
		DatasheetID int64  `json:"datasheetId"`
		MPN         string `json:"mpn"`
	}) (any, error) {
		return c.Lib.Recommendations(ctx, in.DatasheetID, in.MPN)
	})
	a.methods["recommendations.delete"] = method(func(ctx context.Context, in idReq) (any, error) {
		return nil, c.Lib.DeleteRecommendation(ctx, in.ID)
	})
	a.methods["search"] = method(func(ctx context.Context, in struct {
		Query string `json:"query"`
		library.SearchOptions
	}) (any, error) {
		return c.Search(ctx, in.Query, in.SearchOptions)
	})
	a.methods["notes.list"] = method(func(ctx context.Context, in struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
	}) (any, error) {
		return c.Lib.Notes(ctx, in.Kind, in.ID)
	})
	a.methods["notes.add"] = method(func(ctx context.Context, in struct {
		library.Note
		DatasheetID int64 `json:"datasheetId"`
	}) (any, error) {
		in.Author = library.SourceUser
		n, err := c.Lib.AddNote(ctx, in.Note, in.DatasheetID)
		c.Changed("note", in.DatasheetID)
		return n, err
	})
	a.methods["notes.update"] = method(func(ctx context.Context, in struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
	}) (any, error) {
		return c.Lib.UpdateNote(ctx, in.ID, in.Body)
	})
	a.methods["notes.delete"] = method(func(ctx context.Context, in idReq) (any, error) { return nil, c.Lib.DeleteNote(ctx, in.ID) })
}

// registerProjects adds project and calculator methods.
func (a *API) registerProjects() {
	c := a.core
	a.methods["projects.list"] = method(func(ctx context.Context, _ struct{}) (any, error) { return c.Lib.Projects(ctx) })
	a.methods["projects.create"] = method(func(ctx context.Context, in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}) (any, error) {
		p, err := c.Lib.CreateProject(ctx, in.Name, in.Description)
		if err == nil {
			c.ProjectChanged(p.Name, "created")
		}
		return p, err
	})
	a.methods["projects.update"] = method(func(ctx context.Context, in library.Project) (any, error) {
		err := c.Lib.UpdateProject(ctx, in.ID, in.Name, in.Description)
		c.ProjectChanged(in.Name, "updated")
		return nil, err
	})
	a.methods["projects.delete"] = method(func(ctx context.Context, in idReq) (any, error) {
		err := c.Lib.DeleteProject(ctx, in.ID)
		c.ProjectChanged("", "deleted")
		return nil, err
	})
	a.methods["projects.setActive"] = method(func(ctx context.Context, in struct {
		Name string `json:"name"`
	}) (any, error) {
		return nil, c.Config.SetActiveProject(in.Name)
	})
	a.methods["project.detail"] = method(func(ctx context.Context, in struct {
		Project string `json:"project"`
	}) (any, error) {
		return c.ProjectDetail(ctx, in.Project)
	})
	a.methods["rails.set"] = method(func(ctx context.Context, in library.Rail) (any, error) {
		r, err := c.Lib.SetRail(ctx, in)
		c.ProjectChanged("", "rail")
		return r, err
	})
	a.methods["rails.delete"] = method(func(ctx context.Context, in struct {
		ProjectID int64  `json:"projectId"`
		Name      string `json:"name"`
	}) (any, error) {
		err := c.Lib.DeleteRail(ctx, in.ProjectID, in.Name)
		c.ProjectChanged("", "rail")
		return nil, err
	})
	a.methods["projectParts.add"] = method(func(ctx context.Context, in struct {
		Project string `json:"project"`
		Part    string `json:"part"`
		library.ProjectPart
	}) (any, error) {
		return c.AddToProject(ctx, in.Project, in.Part, in.ProjectPart)
	})
	a.methods["projectParts.update"] = method(func(ctx context.Context, in library.ProjectPart) (any, error) {
		err := c.Lib.UpdateProjectPart(ctx, in)
		c.ProjectChanged("", "part")
		return nil, err
	})
	a.methods["projectParts.remove"] = method(func(ctx context.Context, in struct {
		ProjectID int64 `json:"projectId"`
		ID        int64 `json:"id"`
	}) (any, error) {
		err := c.Lib.RemoveProjectPart(ctx, in.ProjectID, in.ID)
		c.ProjectChanged("", "part")
		return nil, err
	})
	a.methods["calc.fields"] = method(func(ctx context.Context, in struct {
		Kind string `json:"kind"`
	}) (any, error) {
		return core.CalcFields(in.Kind), nil
	})
	a.methods["calc.run"] = method(func(ctx context.Context, in core.CalcRequest) (any, error) { return c.Calc(ctx, in) })
	a.methods["calc.list"] = method(func(ctx context.Context, in struct {
		ProjectID int64 `json:"projectId"`
	}) (any, error) {
		return c.Lib.Calculations(ctx, in.ProjectID)
	})
	a.methods["calc.delete"] = method(func(ctx context.Context, in idReq) (any, error) {
		err := c.Lib.DeleteCalculation(ctx, in.ID)
		c.ProjectChanged("", "calculation")
		return nil, err
	})
}
