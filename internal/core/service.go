// Package core is SheetDiver's application service. The desktop UI (via
// internal/api) and coding agents (via internal/mcpserver) are thin
// adapters over the methods defined here, so both see the same library,
// the same extraction and the same calculators.
package core

import (
	"errors"
	"sync"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/events"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
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
	EvMCP      = "mcp"
	EvCalc     = "calc"
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
	Config  *config.Store
	Lib     *library.Store
	Engine  *pdf.Engine
	Bus     *events.Bus
	Version string

	mu      sync.Mutex
	mcp     MCPStatus
	session session
	// importMu serializes imports (one PDF engine, one writer).
	importMu sync.Mutex
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
	return &Service{
		Config:  cfg,
		Lib:     lib,
		Engine:  pdf.NewEngine(pdf.DefaultCacheDir()),
		Bus:     events.NewBus(),
		Version: version,
	}, nil
}

// Close releases the library and the PDF engine.
func (s *Service) Close() error {
	_ = s.Engine.Close()
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
	// Kind is part, page, figure, section, project, calc or search.
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
}

// UIContext is what the user is looking at, reported by the UI so agents
// can answer "what about this graph?" without copying anything.
type UIContext struct {
	View        string    `json:"view"`
	Part        string    `json:"part,omitempty"`
	DatasheetID int64     `json:"datasheetId,omitempty"`
	Page        int       `json:"page,omitempty"`
	FigureID    int64     `json:"figureId,omitempty"`
	Project     string    `json:"project,omitempty"`
	Selection   string    `json:"selection,omitempty"`
	Updated     time.Time `json:"updated"`
}

// session is in-memory state for this run.
type session struct {
	mu       sync.Mutex
	seq      int
	activity []Activity
	ctx      UIContext
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
