// Package connect builds what agents need to connect to SheetDiver: the
// command that starts its stdio MCP bridge, and a Claude Desktop extension
// (.mcpb) that runs it.
package connect

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// icon is the extension's icon (made by scripts/brand).
//
//go:embed icon.png
var icon []byte

// Command is the path of this SheetDiver executable, for stdio clients to
// run with the "mcp" argument. A bare "sheetdiver" only works when it's on
// PATH, which the Windows and macOS installs aren't.
func Command() string {
	// An AppImage runs from a temporary mount; the image itself stays put.
	if p := os.Getenv("APPIMAGE"); p != "" {
		return p
	}
	p, err := os.Executable()
	if err != nil {
		return "sheetdiver"
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

// BridgeName is the file name of the console bridge (cmd/sheetdiver-mcp)
// that the Windows install puts beside the app.
const BridgeName = "sheetdiver-mcp.exe"

// Stdio is how a client launches SheetDiver's stdio MCP server.
type Stdio struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// StdioCommand is the stdio server for this install. On Windows it is the
// console bridge beside the app: the app is a windowed program, which
// can't serve a client's stdio there. Elsewhere it is this executable
// with "mcp".
func StdioCommand() Stdio {
	if runtime.GOOS == "windows" {
		if p, ok := bridgeBeside(Command()); ok {
			return Stdio{Command: p, Args: []string{}}
		}
	}
	return Stdio{Command: Command(), Args: []string{"mcp"}}
}

// bridgeBeside returns the console bridge next to app, when it's there.
func bridgeBeside(app string) (string, bool) {
	p := filepath.Join(filepath.Dir(app), BridgeName)
	st, err := os.Stat(p)
	return p, err == nil && !st.IsDir()
}

// ErrNoBridge means a Windows extension was asked for without the console
// bridge to put in it.
var ErrNoBridge = errors.New(BridgeName + " isn't installed beside SheetDiver; reinstall SheetDiver to get it")

// Extension describes the Claude Desktop extension to build.
type Extension struct {
	// Version is the app's version ("v0.1.0", or "dev").
	Version string
	// Command is the SheetDiver executable the extension runs. Windows'
	// extension carries its own bridge and doesn't use it, so it may be ""
	// there (a release's extension, for any install).
	Command string
	// Platform is runtime.GOOS of the machine it's for (default: this one).
	Platform string
	// Bridge is the console bridge executable the Windows extension
	// carries (cmd/sheetdiver-mcp, built for Windows).
	Bridge []byte
}

// ForThisApp describes the extension for this install, with the console
// bridge read from beside the app on Windows.
func ForThisApp(version string) (Extension, error) {
	e := Extension{Version: version, Command: Command()}
	if e.platform() != "windows" {
		return e, nil
	}
	p, ok := bridgeBeside(e.Command)
	if !ok {
		return e, ErrNoBridge
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return e, err
	}
	e.Bridge = b
	return e, nil
}

// platform is the GOOS the extension is for.
func (e Extension) platform() string {
	if e.Platform == "" {
		return runtime.GOOS
	}
	return e.Platform
}

// platforms maps Go's GOOS to the platform names extensions use.
var platforms = map[string]string{"darwin": "darwin", "windows": "win32", "linux": "linux"}

// semver turns the app's version into the plain semantic version a
// manifest wants ("v0.1.0-rc.6" → "0.1.0-rc.6"; "dev" → "0.0.0-dev").
func semver(v string) string {
	v = strings.TrimPrefix(v, "v")
	if v == "" || v[0] < '0' || v[0] > '9' {
		return "0.0.0-dev"
	}
	return v
}

// overrides is the library and config this app was started on, for the
// extension to use the same ones.
func overrides() map[string]string {
	env := map[string]string{}
	for _, k := range []string{"SHEETDIVER_CONFIG", "SHEETDIVER_LIBRARY"} {
		if v := os.Getenv(k); v != "" {
			env[k] = v
		}
	}
	return env
}

// Manifest is the extension's manifest.json.
func (e Extension) Manifest() (map[string]any, error) {
	srv, err := e.server()
	if err != nil {
		return nil, err
	}
	compat := map[string]any{"platforms": []string{platforms[e.platform()]}}
	if r := srv.runtimes(); r != nil {
		compat["runtimes"] = r
	}
	m := map[string]any{
		"manifest_version": "0.3",
		"name":             "sheetdiver",
		"display_name":     "SheetDiver",
		"version":          semver(e.Version),
		"description":      "Your datasheet library: parts, parameters, figures, projects and the parts catalogue.",
		"long_description": "Connects Claude to the SheetDiver app on this computer. Claude can search your datasheets, read parameters, figures and sections, check projects, and show you what it finds in the SheetDiver window. Everything stays on this computer: the extension runs SheetDiver's own bridge to the app (127.0.0.1).",
		"author":           map[string]string{"name": "SheetDiver", "url": "https://github.com/aitakemyjob/sheetdiver"},
		"homepage":         "https://github.com/aitakemyjob/sheetdiver",
		"repository":       map[string]string{"type": "git", "url": "https://github.com/aitakemyjob/sheetdiver"},
		"license":          "MIT",
		"icon":             "icon.png",
		"keywords":         []string{"datasheets", "electronics", "components", "hardware"},
		"server":           srv.section(overrides()),
		"compatibility":    compat,
	}
	if uc := srv.userConfig(); uc != nil {
		m["user_config"] = uc
	}
	return m, nil
}

// ManifestJSON is manifest.json's contents.
func (e Extension) ManifestJSON() ([]byte, error) {
	m, err := e.Manifest()
	if err != nil {
		return nil, err
	}
	return json.MarshalIndent(m, "", "  ")
}

// Build writes the extension: a zip of manifest.json, the server and the
// icon, which Claude Desktop installs when it's opened.
func (e Extension) Build() ([]byte, error) {
	srv, err := e.server()
	if err != nil {
		return nil, err
	}
	man, err := e.ManifestJSON()
	if err != nil {
		return nil, err
	}
	files := append([]bundleFile{{name: "manifest.json", data: man, mode: 0o644}}, srv.files()...)
	files = append(files, bundleFile{name: "icon.png", data: icon, mode: 0o644})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		// Unix permissions, as the mcpb CLI's packer writes them.
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		h.SetMode(f.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
