package connect

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// unzip reads an extension's files.
func unzip(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		want := fs.FileMode(0o644)
		if strings.HasSuffix(f.Name, ".exe") {
			want = 0o755
		}
		if f.Mode().Perm() != want {
			t.Errorf("%s has mode %v, want %v", f.Name, f.Mode(), want)
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], _ = io.ReadAll(r)
		r.Close()
	}
	return out
}

// manifest is the part of manifest.json the tests check.
type manifest struct {
	ManifestVersion string `json:"manifest_version"`
	Name, Version   string
	Icon            string
	Server          struct {
		Type       string
		EntryPoint string `json:"entry_point"`
		MCPConfig  struct {
			Command string
			Args    []string
			Env     map[string]string
		} `json:"mcp_config"`
	}
	UserConfig    map[string]struct{ Type string } `json:"user_config"`
	Compatibility struct {
		Platforms []string
		Runtimes  map[string]string
	}
}

func TestWindowsExtensionCarriesTheConsoleBridge(t *testing.T) {
	t.Setenv("SHEETDIVER_CONFIG", "")
	bridge := []byte("MZ console bridge")
	if _, err := (Extension{Version: "v1.0.0", Platform: "windows"}).Build(); !errors.Is(err, ErrNoBridge) {
		t.Errorf("without the bridge: %v", err)
	}
	b, err := Extension{Version: "v0.1.0", Platform: "windows", Bridge: bridge, Command: `C:\Program Files\SheetDiver\sheetdiver.exe`}.Build()
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	if !bytes.Equal(files["server/sheetdiver-mcp.exe"], bridge) || files["server/index.js"] != nil {
		t.Errorf("files = %v", slices.Collect(maps.Keys(files)))
	}
	var m manifest
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	s := m.Server
	if s.Type != "binary" || s.EntryPoint != "server/sheetdiver-mcp.exe" || s.MCPConfig.Command != "${__dirname}/server/sheetdiver-mcp.exe" {
		t.Errorf("server = %+v", s)
	}
	want := []string{"--port", "${user_config.port}", "--launch=${user_config.launch}", "--app", `C:\Program Files\SheetDiver\sheetdiver.exe`}
	if !slices.Equal(s.MCPConfig.Args, want) {
		t.Errorf("args = %q", s.MCPConfig.Args)
	}
	if _, set := s.MCPConfig.Env["SHEETDIVER_APP"]; set {
		t.Error("the bridge doesn't need the app's path in its environment")
	}
	if m.UserConfig["port"].Type != "number" || m.UserConfig["launch"].Type != "boolean" {
		t.Errorf("user_config = %+v", m.UserConfig)
	}
	if !slices.Equal(m.Compatibility.Platforms, []string{"win32"}) || m.Compatibility.Runtimes != nil {
		t.Errorf("compatibility = %+v", m.Compatibility)
	}

	// A release's extension works with any install.
	b, _ = Extension{Version: "v0.1.0", Platform: "windows", Bridge: bridge}.Build()
	_ = json.Unmarshal(unzip(t, b)["manifest.json"], &m)
	if slices.Contains(m.Server.MCPConfig.Args, "--app") {
		t.Errorf("args = %q", m.Server.MCPConfig.Args)
	}
}

func TestExtension(t *testing.T) {
	t.Setenv("SHEETDIVER_LIBRARY", "/data/lib")
	t.Setenv("SHEETDIVER_CONFIG", "")
	b, err := Extension{Version: "v0.1.0-rc.6", Command: "/Applications/SheetDiver.app/Contents/MacOS/sheetdiver", Platform: "darwin"}.Build()
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	for _, name := range []string{"manifest.json", "server/index.js", "icon.png"} {
		if len(files[name]) == 0 {
			t.Errorf("%s is missing", name)
		}
	}
	var m manifest
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m.ManifestVersion != "0.3" || m.Name != "sheetdiver" || m.Version != "0.1.0-rc.6" || m.Icon != "icon.png" {
		t.Errorf("manifest = %+v", m)
	}
	if _, ok := files[m.Server.EntryPoint]; !ok || m.Server.Type != "node" || m.Server.MCPConfig.Command != "node" || m.Server.MCPConfig.Args[0] != "${__dirname}/server/index.js" {
		t.Errorf("server = %+v", m.Server)
	}
	env := m.Server.MCPConfig.Env
	if env["SHEETDIVER_APP"] != "/Applications/SheetDiver.app/Contents/MacOS/sheetdiver" || env["SHEETDIVER_LIBRARY"] != "/data/lib" {
		t.Errorf("env = %v", env)
	}
	if _, set := env["SHEETDIVER_CONFIG"]; set {
		t.Error("unset overrides aren't passed on")
	}
	if !slices.Equal(m.Compatibility.Platforms, []string{"darwin"}) || m.Compatibility.Runtimes["node"] == "" || m.UserConfig != nil {
		t.Errorf("compatibility = %+v, user_config = %v", m.Compatibility, m.UserConfig)
	}
	// A dev build still has a semantic version.
	b, _ = Extension{Version: "dev", Command: "/x", Platform: "linux"}.Build()
	files = unzip(t, b)
	if !strings.Contains(string(files["manifest.json"]), `"version": "0.0.0-dev"`) {
		t.Errorf("dev version: %s", files["manifest.json"])
	}
}

func TestLauncherRunsTheBridgeOnClaudesPipes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs node and a shell script")
	}
	dir := t.TempDir()
	// A stand-in for "sheetdiver mcp" that echoes what Claude sends.
	app := filepath.Join(dir, "sheetdiver")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n[ \"$1\" = mcp ] || exit 3\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := Extension{Version: "v1.0.0", Command: app, Platform: "linux"}.Build()
	js := filepath.Join(dir, "index.js")
	if err := os.WriteFile(js, unzip(t, b)["server/index.js"], 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, js)
	cmd.Env = append(os.Environ(), "SHEETDIVER_APP="+app)
	cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n")
	out, err := cmd.Output()
	if err != nil || !strings.Contains(string(out), `"method":"ping"`) {
		t.Errorf("relayed %q, %v", out, err)
	}

	// A moved app says what to do.
	cmd = exec.Command(node, js)
	cmd.Env = append(os.Environ(), "SHEETDIVER_APP="+filepath.Join(dir, "gone"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil || !strings.Contains(stderr.String(), "save the extension again") {
		t.Errorf("missing app: %v %q", err, stderr.String())
	}
}

func TestCommand(t *testing.T) {
	t.Setenv("APPIMAGE", "/home/me/SheetDiver.AppImage")
	if got := Command(); got != "/home/me/SheetDiver.AppImage" {
		t.Errorf("AppImage = %q", got)
	}
	t.Setenv("APPIMAGE", "")
	if got := Command(); !filepath.IsAbs(got) {
		t.Errorf("command = %q, want this executable's path", got)
	}
}

func TestStdioCommand(t *testing.T) {
	got := StdioCommand()
	if runtime.GOOS == "windows" {
		t.Skip("depends on the installed bridge")
	}
	if got.Command != Command() || !slices.Equal(got.Args, []string{"mcp"}) {
		t.Errorf("stdio = %+v", got)
	}
}

func TestForThisAppFindsTheBridge(t *testing.T) {
	dir := t.TempDir()
	if _, ok := bridgeBeside(filepath.Join(dir, "sheetdiver.exe")); ok {
		t.Error("found a bridge in an empty folder")
	}
	if err := os.WriteFile(filepath.Join(dir, BridgeName), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, ok := bridgeBeside(filepath.Join(dir, "sheetdiver.exe")); !ok || p != filepath.Join(dir, BridgeName) {
		t.Errorf("bridge = %q, %v", p, ok)
	}
}
