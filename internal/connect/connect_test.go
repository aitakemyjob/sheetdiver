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
		if strings.HasPrefix(f.Name, "server/sheetdiver-mcp") {
			want = 0o755 // the bridge stays executable when unpacked
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
	// The same bundle works for any install: no path to the app in it.
	want := []string{"--port", "${user_config.port}"}
	if !slices.Equal(s.MCPConfig.Args, want) {
		t.Errorf("args = %q", s.MCPConfig.Args)
	}
	if _, set := s.MCPConfig.Env["SHEETDIVER_APP"]; set {
		t.Error("the bridge doesn't need the app's path in its environment")
	}
	if m.UserConfig["port"].Type != "number" || len(m.UserConfig) != 1 {
		t.Errorf("user_config = %+v", m.UserConfig)
	}
	if !slices.Equal(m.Compatibility.Platforms, []string{"win32"}) || m.Compatibility.Runtimes != nil {
		t.Errorf("compatibility = %+v", m.Compatibility)
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
	b, _ = Extension{Version: "dev", Command: "/x", Platform: "darwin"}.Build()
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
	b, _ := Extension{Version: "v1.0.0", Command: app, Platform: "darwin"}.Build()
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

// launch runs the launcher with SheetDiver replaced by script, giving it
// stdin, and returns its stdout, stderr and exit code.
func launch(t *testing.T, script string, stdin *os.File) (string, string, int) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("needs node and a shell script")
	}
	dir := t.TempDir()
	app := filepath.Join(dir, "sheetdiver")
	if err := os.WriteFile(app, []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := Extension{Version: "v1.0.0", Command: app, Platform: "darwin"}.Build()
	js := filepath.Join(dir, "index.js")
	if err := os.WriteFile(js, unzip(t, b)["server/index.js"], 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, js)
	cmd.Env = append(os.Environ(), "SHEETDIVER_APP="+app)
	cmd.Stdin = stdin
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

// input is a file holding s, for a process's stdin.
func input(t *testing.T, s string) *os.File {
	t.Helper()
	p := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestLauncherPipesStdioRatherThanInheritingIt(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	// The launcher's stdin is a file; SheetDiver must get the launcher's
	// own pipes, never the launcher's handles.
	// (Node's pipes are socket pairs on Unix.)
	out, _, code := launch(t, "readlink /proc/self/fd/0 /proc/self/fd/1\n", input(t, ""))
	fds := strings.Fields(out)
	if code != 0 || len(fds) != 2 {
		t.Fatalf("readlink: %q, exit %d", out, code)
	}
	for _, fd := range fds {
		if !strings.HasPrefix(fd, "pipe:") && !strings.HasPrefix(fd, "socket:") {
			t.Errorf("SheetDiver's stdio is %q, want the launcher's pipes", fd)
		}
	}
}

func TestLauncherReportsWhySheetDiverStopped(t *testing.T) {
	_, stderr, code := launch(t, "echo 'library is locked' >&2\nexit 7\n", input(t, ""))
	if code != 7 || !strings.Contains(stderr, "library is locked") || !strings.Contains(stderr, "exited with status 7") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestLauncherDeliversRepliesAfterClaudeClosesItsInput(t *testing.T) {
	// Claude sends a request and closes its input at once, as its version
	// probe does; the reply comes a little later and must still arrive.
	out, _, code := launch(t, "read line\nsleep 0.3\necho \"reply to $line\"\n", input(t, "probe\n"))
	if code != 0 || out != "reply to probe\n" {
		t.Errorf("out %q, exit %d", out, code)
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
	if _, ok := bridgeBeside(filepath.Join(dir, "sheetdiver.exe"), "windows"); ok {
		t.Error("found a bridge in an empty folder")
	}
	if err := os.WriteFile(filepath.Join(dir, BridgeName("windows")), []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, ok := bridgeBeside(filepath.Join(dir, "sheetdiver.exe"), "windows"); !ok || p != filepath.Join(dir, BridgeName("windows")) {
		t.Errorf("bridge = %q, %v", p, ok)
	}
}

func TestLinuxExtensionCarriesTheConsoleBridge(t *testing.T) {
	// Claude Desktop starts the bundled bridge itself: no Node launcher,
	// and nothing that depends on the app's GUI libraries or install path.
	t.Setenv("SHEETDIVER_CONFIG", "")
	if _, err := (Extension{Version: "v1.0.0", Platform: "linux"}).Build(); !errors.Is(err, ErrNoBridge) {
		t.Errorf("without the bridge: %v", err)
	}
	bridge := []byte("\x7fELF console bridge")
	b, err := Extension{Version: "v0.1.1", Platform: "linux", Bridge: bridge, Command: "/usr/bin/sheetdiver"}.Build()
	if err != nil {
		t.Fatal(err)
	}
	files := unzip(t, b)
	if !bytes.Equal(files["server/sheetdiver-mcp"], bridge) || files["server/index.js"] != nil {
		t.Errorf("files = %v", slices.Collect(maps.Keys(files)))
	}
	var m manifest
	if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
		t.Fatal(err)
	}
	s := m.Server
	if s.Type != "binary" || s.EntryPoint != "server/sheetdiver-mcp" || s.MCPConfig.Command != "${__dirname}/server/sheetdiver-mcp" || !slices.Equal(s.MCPConfig.Args, []string{"--port", "${user_config.port}"}) {
		t.Errorf("server = %+v", s)
	}
	if !slices.Equal(m.Compatibility.Platforms, []string{"linux"}) || m.Compatibility.Runtimes != nil {
		t.Errorf("compatibility = %+v", m.Compatibility)
	}
	// The bridge is found beside the installed app.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, BridgeName("linux")), bridge, 0o755); err != nil {
		t.Fatal(err)
	}
	if p, ok := bridgeBeside(filepath.Join(dir, "sheetdiver"), "linux"); !ok || p != filepath.Join(dir, "sheetdiver-mcp") {
		t.Errorf("bridge = %q, %v", p, ok)
	}
}
