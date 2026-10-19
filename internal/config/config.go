// Package config persists SheetDiver's user settings: where the library
// lives, the UI theme, and MCP server options.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// DefaultMCPPort is the loopback port for the MCP endpoint and browser UI.
const DefaultMCPPort = 47480

// Settings are the user's preferences.
type Settings struct {
	Theme      string `json:"theme"`
	FontSize   int    `json:"fontSize"`
	MCPPort    int    `json:"mcpPort"`
	MCPEnabled bool   `json:"mcpEnabled"`
	// FollowAgent makes the UI jump to whatever the agent shows.
	FollowAgent bool `json:"followAgent"`
	// AgentWrites lets agents record parameters, figure descriptions,
	// pins, registers, notes and project changes. Off makes MCP read-only.
	AgentWrites bool `json:"agentWrites"`
	// AgentImport lets agents add datasheets from a URL or a local PDF.
	AgentImport bool `json:"agentImport"`
	// LibraryDir overrides the library location (empty: the default).
	LibraryDir string `json:"libraryDir"`
	// CatalogRemindDays is how old the downloaded parts catalogue may get
	// before the UI suggests refreshing it: 0 means the default (14), -1
	// never.
	CatalogRemindDays int `json:"catalogRemindDays"`
}

// DefaultCatalogRemindDays is the default catalogue refresh reminder.
const DefaultCatalogRemindDays = 14

// RemindDays is the catalogue reminder in days (0: never).
func (s Settings) RemindDays() int {
	switch {
	case s.CatalogRemindDays < 0:
		return 0
	case s.CatalogRemindDays == 0:
		return DefaultCatalogRemindDays
	}
	return s.CatalogRemindDays
}

// Config is the whole persisted configuration.
type Config struct {
	Settings Settings `json:"settings"`
	// ActiveProject is the project the UI and agents default to.
	ActiveProject string `json:"activeProject"`
}

// defaults returns the configuration used when no config file exists yet.
func defaults() Config {
	return Config{Settings: Settings{
		Theme:       "deepdive",
		FontSize:    13,
		MCPPort:     DefaultMCPPort,
		MCPEnabled:  true,
		FollowAgent: true,
		AgentWrites: true,
		AgentImport: true,
	}}
}

// Store is a concurrency-safe, file-backed Config.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

// DefaultPath returns the platform config location, e.g.
// ~/.config/sheetdiver/config.json on Linux.
func DefaultPath() (string, error) {
	if p := os.Getenv("SHEETDIVER_CONFIG"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sheetdiver", "config.json"), nil
}

// DefaultLibraryDir is where the library lives unless overridden:
// $SHEETDIVER_LIBRARY, else the platform data directory
// (~/.local/share/sheetdiver, ~/Library/Application Support/SheetDiver,
// %AppData%\SheetDiver).
func DefaultLibraryDir() string {
	if p := os.Getenv("SHEETDIVER_LIBRARY"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "SheetDiver", "library")
	case "windows":
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "SheetDiver", "library")
		}
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "sheetdiver", "library")
	}
	return filepath.Join(home, ".local", "share", "sheetdiver", "library")
}

// Open loads the config at path, filling in defaults for missing settings.
// A missing file is not an error; it is created on the first save.
func Open(path string) (*Store, error) {
	s := &Store{path: path, cfg: defaults()}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &s.cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	d := defaults().Settings
	if s.cfg.Settings.MCPPort == 0 {
		s.cfg.Settings.MCPPort = d.MCPPort
	}
	if s.cfg.Settings.FontSize == 0 {
		s.cfg.Settings.FontSize = d.FontSize
	}
	if s.cfg.Settings.Theme == "" {
		s.cfg.Settings.Theme = d.Theme
	}
	return s, nil
}

// Get returns a snapshot of the configuration.
func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// LibraryDir is the library folder in effect.
func (s *Store) LibraryDir() string {
	if d := s.Get().Settings.LibraryDir; d != "" {
		return d
	}
	return DefaultLibraryDir()
}

// saveLocked writes the config atomically (temp file + rename). Callers
// must hold s.mu.
func (s *Store) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// UpdateSettings validates, clamps and persists application settings.
func (s *Store) UpdateSettings(st Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st.MCPPort <= 0 || st.MCPPort > 65535 {
		st.MCPPort = DefaultMCPPort
	}
	if st.FontSize < 9 || st.FontSize > 28 {
		st.FontSize = 13
	}
	if st.CatalogRemindDays < -1 || st.CatalogRemindDays > 365 {
		st.CatalogRemindDays = 0
	}
	s.cfg.Settings = st
	return s.saveLocked()
}

// SetActiveProject records the default project ("" for none).
func (s *Store) SetActiveProject(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg.ActiveProject = name
	return s.saveLocked()
}
