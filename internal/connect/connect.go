// Package connect builds what agents need to connect to SheetDiver: the
// command that starts its stdio MCP bridge, and a Claude Desktop extension
// (.mcpb) that runs it.
package connect

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
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

// Extension describes the Claude Desktop extension to build.
type Extension struct {
	// Version is the app's version ("v0.1.0", or "dev").
	Version string
	// Command is the SheetDiver executable the extension runs.
	Command string
	// Platform is runtime.GOOS of the machine it's for (default: this one).
	Platform string
}

// launcher is the extension's server: it runs SheetDiver's stdio bridge
// with Claude Desktop's own pipes. The bridge relays to the running app,
// or serves the library itself while the app is closed. Claude Desktop
// supplies Node for "node" extensions, so nothing else is needed.
const launcher = `// SheetDiver for Claude Desktop: starts "sheetdiver mcp" (the stdio
// bridge to the SheetDiver app) at the path saved in the extension.
const { spawn } = require('node:child_process');

const command = process.env.SHEETDIVER_APP;
const child = spawn(command, ['mcp'], { stdio: 'inherit', windowsHide: true });
child.on('error', (err) => {
  console.error('sheetdiver: cannot start ' + command + ': ' + err.message +
    '. If SheetDiver moved, save the extension again (Connect an agent > Claude Desktop) and reinstall it (Settings > Extensions > Advanced settings > Install Extension).');
  process.exit(1);
});
child.on('exit', (code, signal) => process.exit(code ?? (signal ? 1 : 0)));
for (const sig of ['SIGINT', 'SIGTERM']) process.on(sig, () => child.kill(sig));
`

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

// Manifest is the extension's manifest.json.
func (e Extension) Manifest() map[string]any {
	plat := e.Platform
	if plat == "" {
		plat = runtime.GOOS
	}
	env := map[string]string{"SHEETDIVER_APP": e.Command}
	// An app started on another library or config: the extension uses the
	// same one when it serves the library itself.
	for _, k := range []string{"SHEETDIVER_CONFIG", "SHEETDIVER_LIBRARY"} {
		if v := os.Getenv(k); v != "" {
			env[k] = v
		}
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
		"server": map[string]any{
			"type":        "node",
			"entry_point": "server/index.js",
			"mcp_config": map[string]any{
				"command": "node",
				"args":    []string{"${__dirname}/server/index.js"},
				"env":     env,
			},
		},
		"compatibility": map[string]any{
			"platforms": []string{platforms[plat]},
			"runtimes":  map[string]string{"node": ">=16.0.0"},
		},
	}
	return m
}

// Build writes the extension: a zip of manifest.json, the launcher and
// the icon, which Claude Desktop installs when it's opened.
func (e Extension) Build() ([]byte, error) {
	man, err := json.MarshalIndent(e.Manifest(), "", "  ")
	if err != nil {
		return nil, err
	}
	files := []struct {
		name string
		data []byte
	}{{"manifest.json", man}, {"server/index.js", []byte(launcher)}, {"icon.png", icon}}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		// Unix permissions, as the mcpb CLI's packer writes them.
		h := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		h.SetMode(0o644)
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
