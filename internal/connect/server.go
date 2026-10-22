package connect

import (
	"io/fs"
	"maps"
	"strconv"

	"github.com/aitakemyjob/sheetdiver/internal/config"
)

// bundleFile is a file in the extension's zip.
type bundleFile struct {
	name string
	data []byte
	mode fs.FileMode
}

// server is how an extension runs SheetDiver's stdio bridge: its
// manifest "server" section, settings, runtime needs and files.
type server interface {
	// section is the manifest's "server" object; env is passed on to it.
	section(env map[string]string) map[string]any
	// userConfig is the manifest's "user_config" (nil: none).
	userConfig() map[string]any
	// runtimes is compatibility.runtimes (nil: none needed).
	runtimes() map[string]string
	// files are the server's files in the bundle.
	files() []bundleFile
}

// server picks how the extension runs the bridge on its platform.
func (e Extension) server() (server, error) {
	if e.platform() == "windows" {
		if len(e.Bridge) == 0 {
			return nil, ErrNoBridge
		}
		return binaryServer{bridge: e.Bridge, app: e.Command}, nil
	}
	return nodeServer{app: e.Command}, nil
}

// binaryServer runs the console bridge carried in the bundle. It's
// Windows' server: the windowed app can't serve stdio there, and it
// doesn't depend on where SheetDiver is installed.
type binaryServer struct {
	bridge []byte
	// app is the installed app, a hint for starting it when it's closed
	// ("" lets the bridge find it from the installer's record).
	app string
}

// bridgeEntry is the bridge's path in the bundle.
const bridgeEntry = "server/" + BridgeName

// section runs the bundled bridge with the user's port and launch settings.
func (s binaryServer) section(env map[string]string) map[string]any {
	args := []string{"--port", "${user_config.port}", "--launch=${user_config.launch}"}
	if s.app != "" {
		args = append(args, "--app", s.app)
	}
	return map[string]any{
		"type":        "binary",
		"entry_point": bridgeEntry,
		"mcp_config": map[string]any{
			"command": "${__dirname}/" + bridgeEntry,
			"args":    args,
			"env":     env,
		},
	}
}

// userConfig offers the port and whether to start a closed app.
func (binaryServer) userConfig() map[string]any {
	return map[string]any{
		"port": map[string]any{
			"type":        "number",
			"title":       "SheetDiver port",
			"description": "The MCP port set in SheetDiver's Settings. 0 reads it from SheetDiver's settings (default " + strconv.Itoa(config.DefaultMCPPort) + ").",
			"default":     0,
			"min":         0,
			"max":         65535,
			"required":    false,
		},
		"launch": map[string]any{
			"type":        "boolean",
			"title":       "Start SheetDiver when it's closed",
			"description": "Open the SheetDiver app when Claude connects and it isn't running.",
			"default":     true,
			"required":    false,
		},
	}
}

// runtimes: the bridge is a native program.
func (binaryServer) runtimes() map[string]string { return nil }

// files is the bridge, executable.
func (s binaryServer) files() []bundleFile {
	return []bundleFile{{name: bridgeEntry, data: s.bridge, mode: 0o755}}
}

// nodeServer runs "sheetdiver mcp" through a small Node launcher, with
// the Node that Claude Desktop supplies. The bridge relays to the running
// app, or serves the library itself while the app is closed.
type nodeServer struct {
	app string
}

// launcher is the Node server: it runs SheetDiver's stdio bridge with
// Claude Desktop's own pipes.
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

// section runs the launcher with the app's path in SHEETDIVER_APP.
func (s nodeServer) section(env map[string]string) map[string]any {
	env = maps.Clone(env)
	env["SHEETDIVER_APP"] = s.app
	return map[string]any{
		"type":        "node",
		"entry_point": "server/index.js",
		"mcp_config": map[string]any{
			"command": "node",
			"args":    []string{"${__dirname}/server/index.js"},
			"env":     env,
		},
	}
}

// userConfig: the launcher has no settings.
func (nodeServer) userConfig() map[string]any { return nil }

// runtimes is the Node the launcher needs.
func (nodeServer) runtimes() map[string]string { return map[string]string{"node": ">=16.0.0"} }

// files is the launcher.
func (nodeServer) files() []bundleFile {
	return []bundleFile{{name: "server/index.js", data: []byte(launcher), mode: 0o644}}
}
