package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultsAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st := s.Get().Settings
	if st.MCPPort != DefaultMCPPort || !st.MCPEnabled || !st.AgentWrites || st.Theme != "deepdive" {
		t.Fatalf("defaults %+v", st)
	}
	st.MCPPort = -1
	st.FontSize = 99
	st.LibraryDir = "/tmp/lib"
	if err := s.UpdateSettings(st); err != nil {
		t.Fatal(err)
	}
	s.SetActiveProject("board")
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Get()
	if got.Settings.MCPPort != DefaultMCPPort || got.Settings.FontSize != 13 || got.ActiveProject != "board" || again.LibraryDir() != "/tmp/lib" {
		t.Errorf("reloaded %+v", got)
	}
}

func TestLibraryDirOverride(t *testing.T) {
	t.Setenv("SHEETDIVER_LIBRARY", "/x/y")
	if DefaultLibraryDir() != "/x/y" {
		t.Error("env override")
	}
}
