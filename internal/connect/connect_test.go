package connect

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], _ = io.ReadAll(r)
		r.Close()
	}
	return out
}

func TestExtension(t *testing.T) {
	t.Setenv("SHEETDIVER_LIBRARY", "/data/lib")
	t.Setenv("SHEETDIVER_CONFIG", "")
	b, err := Extension{Version: "v0.1.0-rc.6", Command: `C:\Program Files\SheetDiver\sheetdiver.exe`, Platform: "windows"}.Build()
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	for _, name := range []string{"manifest.json", "server/index.js", "icon.png"} {
		if len(files[name]) == 0 {
			t.Errorf("%s is missing", name)
		}
	}
	var m struct {
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
		Compatibility struct{ Platforms []string }
	}
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
	if env["SHEETDIVER_APP"] != `C:\Program Files\SheetDiver\sheetdiver.exe` || env["SHEETDIVER_LIBRARY"] != "/data/lib" {
		t.Errorf("env = %v", env)
	}
	if _, set := env["SHEETDIVER_CONFIG"]; set {
		t.Error("unset overrides aren't passed on")
	}
	if len(m.Compatibility.Platforms) != 1 || m.Compatibility.Platforms[0] != "win32" {
		t.Errorf("platforms = %v", m.Compatibility.Platforms)
	}
	// A dev build still has a semantic version.
	b, _ = Extension{Version: "dev", Command: "/x"}.Build()
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
	b, _ := Extension{Version: "v1.0.0", Command: app}.Build()
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
