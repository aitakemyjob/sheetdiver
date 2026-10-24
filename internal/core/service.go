// Package core is SheetDiver's application service. The desktop UI (via
// internal/api) and coding agents (via internal/mcpserver) are thin
// adapters over the methods defined here, so both see the same library,
// the same extraction and the same calculators.
package core

import (
	"path/filepath"
	"strings"

	"errors"
	"sync"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/catalog"
	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/events"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
	"github.com/aitakemyjob/sheetdiver/internal/translate"
)

// Event types published on the bus.
const (
	EvConfig   = "config"
	EvActivity = "activity"
	EvFocus    = "focus"
	EvLibrary  = "library"
	EvImport   = "import"
	EvProject  = "project"
	EvContext  = "context"
	// EvSnapshot asks the app for a picture of a block diagram.
	EvSnapshot = "snapshot"
	EvMCP      = "mcp"
	EvCalc     = "calc"
	EvCatalog  = "catalog"
	// EvTranslate reports translation model downloads.
	EvTranslate = "translate"
)

// Errors returned to agents when the user has switched something off.
var (
	ErrAgentDisabled = errors.New("agent access is disabled in SheetDiver settings")
	ErrAgentReadOnly = errors.New("agents are read-only in SheetDiver settings (Settings → Agent access → Allow changes)")
	ErrAgentImport   = errors.New("agents may not import datasheets (Settings → Agent access → Allow imports)")
)

// Service is the application: configuration, library, PDF engine, event
// bus and this session's collaboration state.
type Service struct {
	// Translate manages the offline translation models.
	Translate *translate.Manager
	// Catalog is the optional downloaded parts catalogue (JLCPCB/LCSC).
	Catalog *catalog.Catalog
	Config  *config.Store
	Lib     *library.Store
	Engine  *pdf.Engine
	Bus     *events.Bus
	Version string

	mu      sync.Mutex
	mcp     MCPStatus
	session session
	// importMu serializes saving imports (one writer, and two imports of
	// the same file must not race); slots bounds concurrent extraction.
	importMu sync.Mutex
	// catalogProgress and catalogErr describe the last catalogue download.
	catalogProgress *catalog.Progress
	catalogErr      string
	// translateProgress and translateErr describe the last model download.
	translateProgress *translate.Progress
	translateErr      string
	// catalogCats caches the catalogue categories of one download.
	catalogCats    []catalog.Category
	catalogCatsKey time.Time
	// catalogParams caches parameter summaries by category and flags.
	catalogParams map[string][]catalog.Param
	slots         chan struct{}
	jobs          jobs
	pending       pendingUpdates
}

// MCPStatus says whether the MCP endpoint is listening.
type MCPStatus struct {
	Running bool   `json:"running"`
	URL     string `json:"url"`
	Error   string `json:"error,omitempty"`
}

// New opens the library in the configured folder and creates the service.
func New(cfg *config.Store, version string) (*Service, error) {
	lib, err := library.Open(cfg.LibraryDir())
	if err != nil {
		return nil, err
	}
	tr := translate.NewManager(filepath.Join(cfg.LibraryDir(), "translate"))
	cat, err := catalog.Open(filepath.Join(cfg.LibraryDir(), "catalog"))
	if err != nil {
		_ = lib.Close()
		return nil, err
	}
	engine := pdf.NewEngine(pdf.DefaultCacheDir())
	return &Service{
		Config:    cfg,
		Lib:       lib,
		Catalog:   cat,
		Translate: tr,
		Engine:    engine,
		Bus:       events.NewBus(),
		Version:   version,
		slots:     make(chan struct{}, engine.Workers),
	}, nil
}

// Close releases the library and the PDF engine.
func (s *Service) Close() error {
	_ = s.Engine.Close()
	_ = s.Catalog.Close()
	return s.Lib.Close()
}

// emit publishes an event to all connected UIs.
func (s *Service) emit(typ string, data any) { s.Bus.Publish(typ, data) }

// SetMCPStatus records whether the MCP server is listening.
func (s *Service) SetMCPStatus(st MCPStatus) {
	s.mu.Lock()
	s.mcp = st
	s.mu.Unlock()
	s.emit(EvMCP, st)
}

// MCPStatus returns the MCP server's status.
func (s *Service) MCPStatus() MCPStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mcp
}

// UpdateSettings persists settings and notifies the UI. Changing the
// library folder takes effect on restart.
func (s *Service) UpdateSettings(st config.Settings) error {
	err := s.Config.UpdateSettings(st)
	s.emit(EvConfig, s.Config.Get())
	return err
}

// AgentRead checks that agents may use SheetDiver at all.
func (s *Service) AgentRead() error {
	if !s.Config.Get().Settings.MCPEnabled {
		return ErrAgentDisabled
	}
	return nil
}

// AgentWrite checks that agents may change the library.
func (s *Service) AgentWrite() error {
	if err := s.AgentRead(); err != nil {
		return err
	}
	if !s.Config.Get().Settings.AgentWrites {
		return ErrAgentReadOnly
	}
	return nil
}

// AgentImport checks that agents may import datasheets.
func (s *Service) AgentImport() error {
	if err := s.AgentRead(); err != nil {
		return err
	}
	if !s.Config.Get().Settings.AgentImport {
		return ErrAgentImport
	}
	return nil
}

// ---- session: activity, focus and what the user is looking at --------------

// Activity is one agent tool call, for the activity feed.
type Activity struct {
	ID         int       `json:"id"`
	Time       time.Time `json:"time"`
	Tool       string    `json:"tool"`
	Summary    string    `json:"summary"`
	Error      string    `json:"error,omitempty"`
	DurationMS int64     `json:"durationMs"`
}

// Focus asks the UI to show something: a part, a page region, a figure,
// a project or a calculation.
type Focus struct {
	// Kind is part, page, figure, section, project, calc, search,
	// category (Query names the category), compare (Parts), catalog
	// (Query, Subcategory and Filters, or LCSC) or walkthrough (Steps).
	Kind        string    `json:"kind"`
	Part        string    `json:"part,omitempty"`
	DatasheetID int64     `json:"datasheetId,omitempty"`
	Page        int       `json:"page,omitempty"`
	Box         *pdf.Rect `json:"box,omitempty"`
	FigureID    int64     `json:"figureId,omitempty"`
	SectionID   int64     `json:"sectionId,omitempty"`
	Project     string    `json:"project,omitempty"`
	Tab         string    `json:"tab,omitempty"`
	Query       string    `json:"query,omitempty"`
	Calc        any       `json:"calc,omitempty"`
	Message     string    `json:"message,omitempty"`
	// Parts and Keys are compared side by side; Notes says something
	// about each part (by part number).
	Parts []string          `json:"parts,omitempty"`
	Keys  []string          `json:"keys,omitempty"`
	Notes map[string]string `json:"notes,omitempty"`
	// LCSC lists catalogue parts to show; Subcategory and Filters narrow a
	// catalogue search.
	LCSC        []string             `json:"lcsc,omitempty"`
	Subcategory string               `json:"subcategory,omitempty"`
	Filters     []catalog.AttrFilter `json:"filters,omitempty"`
	// Highlight marks one thing in the view: the recommended part, an
	// LCSC code, a rail or a BOM entry (designator or part number).
	Highlight string `json:"highlight,omitempty"`
	// Explain is the agent's explanation (markdown), shown beside the
	// view. Links like [p3](#page/TPS63900/3) open what they name.
	Explain string `json:"explain,omitempty"`
	// Title and Steps make a walkthrough: steps the user clicks through.
	Title string  `json:"title,omitempty"`
	Steps []Focus `json:"steps,omitempty"`
}

// UIContext is what the user is looking at, reported by the UI so agents
// can answer "what about this graph?" without copying anything.
type UIContext struct {
	View        string `json:"view"`
	Part        string `json:"part,omitempty"`
	DatasheetID int64  `json:"datasheetId,omitempty"`
	Page        int    `json:"page,omitempty"`
	FigureID    int64  `json:"figureId,omitempty"`
	Project     string `json:"project,omitempty"`
	Selection   string `json:"selection,omitempty"`
	// Problem is what the app failed to show there (a block diagram that
	// couldn't be laid out), for the agent to see.
	Problem string    `json:"problem,omitempty"`
	Updated time.Time `json:"updated"`
}

// session is in-memory state for this run.
type session struct {
	mu       sync.Mutex
	seq      int
	activity []Activity
	ctx      UIContext
	// drawn is where the app last drew each project's parts, by project
	// (lower case); a change to the project's connections forgets it.
	drawn map[string]Drawn
	// snapshots are the pictures of block diagrams asked of the app and
	// not yet answered, by request id.
	snapshots map[string]chan Snapshot
}

// DrawnCell is where the app drew a part or passive: its group, the
// column from the left and the place in it from the top, from 0.
type DrawnCell struct {
	Node   string `json:"node"`
	Group  string `json:"group,omitempty"`
	Column int    `json:"column"`
	Row    int    `json:"row"`
}

// LayoutProblem is something that looks wrong on the drawn block
// diagram (a wire looping round a part, parts on top of each other), as
// the app's layout checks name it.
type LayoutProblem struct {
	Check string `json:"check"`
	Group string `json:"group,omitempty"`
	Text  string `json:"text"`
}

// Drawn is the app's report of a project's block diagram as drawn.
type Drawn struct {
	Project  string          `json:"project"`
	Cells    []DrawnCell     `json:"cells"`
	Problems []LayoutProblem `json:"problems"`
	At       time.Time       `json:"at"`
}

// SetDrawn records where the app drew a project's parts.
func (s *Service) SetDrawn(d Drawn) {
	d.At = time.Now()
	s.session.mu.Lock()
	defer s.session.mu.Unlock()
	if s.session.drawn == nil {
		s.session.drawn = map[string]Drawn{}
	}
	s.session.drawn[strings.ToLower(d.Project)] = d
}

// DrawnLayout is where the app last drew a project's parts, if it has
// since the project's connections last changed.
func (s *Service) DrawnLayout(project string) (Drawn, bool) {
	s.session.mu.Lock()
	defer s.session.mu.Unlock()
	d, ok := s.session.drawn[strings.ToLower(project)]
	return d, ok
}

// forgetDrawn drops a project's drawn layout: it's out of date.
func (s *Service) forgetDrawn(project string) {
	s.session.mu.Lock()
	defer s.session.mu.Unlock()
	delete(s.session.drawn, strings.ToLower(project))
}

// maxActivity bounds the activity log.
const maxActivity = 500

// RecordActivity logs an agent tool call and notifies the UI.
func (s *Service) RecordActivity(a Activity) {
	ss := &s.session
	ss.mu.Lock()
	ss.seq++
	a.ID = ss.seq
	if a.Time.IsZero() {
		a.Time = time.Now()
	}
	ss.activity = append(ss.activity, a)
	if len(ss.activity) > maxActivity {
		ss.activity = ss.activity[len(ss.activity)-maxActivity:]
	}
	ss.mu.Unlock()
	s.emit(EvActivity, a)
}

// Activity returns the activity log, oldest first.
func (s *Service) Activity() []Activity {
	s.session.mu.Lock()
	defer s.session.mu.Unlock()
	return append([]Activity{}, s.session.activity...)
}

// Show asks every UI to display something.
func (s *Service) Show(f Focus) { s.emit(EvFocus, f) }

// SetUIContext records what the user is looking at.
func (s *Service) SetUIContext(c UIContext) {
	c.Updated = time.Now()
	s.session.mu.Lock()
	s.session.ctx = c
	s.session.mu.Unlock()
	s.emit(EvContext, c)
}

// UIContext returns what the user is looking at.
func (s *Service) UIContext() UIContext {
	s.session.mu.Lock()
	defer s.session.mu.Unlock()
	return s.session.ctx
}
