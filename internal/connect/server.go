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
	if carriesBridge(e.platform()) {
		if len(e.Bridge) == 0 {
			return nil, ErrNoBridge
		}
		return binaryServer{bridge: e.Bridge, entry: "server/" + BridgeName(e.platform())}, nil
	}
	return nodeServer{app: e.Command}, nil
}

// binaryServer runs the console bridge carried in the bundle, which
// Claude Desktop starts itself: no Node, no GUI toolkit, and nothing that
// depends on where SheetDiver is installed. Like "sheetdiver mcp", it
// relays to the running app or serves the library itself.
type binaryServer struct {
	bridge []byte
	// entry is the bridge's path in the bundle.
	entry string
}

// section runs the bundled bridge with the user's port setting.
func (s binaryServer) section(env map[string]string) map[string]any {
	args := []string{"--port", "${user_config.port}"}
	return map[string]any{
		"type":        "binary",
		"entry_point": s.entry,
		"mcp_config": map[string]any{
			"command": "${__dirname}/" + s.entry,
			"args":    args,
			"env":     env,
		},
	}
}

// userConfig offers the port.
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
	}
}

// runtimes: the bridge is a native program.
func (binaryServer) runtimes() map[string]string { return nil }

// files is the bridge, executable.
func (s binaryServer) files() []bundleFile {
	return []bundleFile{{name: s.entry, data: s.bridge, mode: 0o755}}
}

// nodeServer runs "sheetdiver mcp" through a small Node launcher, with
// the Node that Claude Desktop supplies: macOS's server, where the
// bundle can't carry an unsigned program. The bridge relays to the running
// app, or serves the library itself while the app is closed.
type nodeServer struct {
	app string
}

// launcher is the Node server: it runs SheetDiver's stdio bridge and
// passes Claude Desktop's messages through.
//
// Stdio is piped through the launcher, not inherited: the Node that
// Claude Desktop runs extensions with may not hand its own stdio on to a
// child, which then finds its input closed and exits without a word
// ("Version negotiation failed: the connection closed during the
// server/discover probe"). Every way SheetDiver can fail is written to
// stderr, which Claude Desktop keeps in the server's log.
const launcher = `// SheetDiver for Claude Desktop: starts "sheetdiver mcp" (the stdio
// bridge to the SheetDiver app) at the path saved in the extension.
'use strict';
const { spawn } = require('node:child_process');

const command = process.env.SHEETDIVER_APP;
const log = (msg) => process.stderr.write('sheetdiver: ' + msg + '\n');

const child = spawn(command, ['mcp'], { stdio: ['pipe', 'pipe', 'pipe'], windowsHide: true });
process.stdin.pipe(child.stdin);
child.stdout.pipe(process.stdout);
child.stderr.pipe(process.stderr);
// Claude ends the session by closing our input: pass that on, so
// SheetDiver answers what it was asked and exits.
process.stdin.on('end', () => child.stdin.end());
// Writing to a SheetDiver that has exited: 'close' below reports why.
child.stdin.on('error', () => {});

child.on('error', (err) => {
  log('cannot start ' + command + ': ' + err.message +
    '. If SheetDiver moved, save the extension again (Connect an agent > Claude Desktop) and reinstall it (Settings > Extensions > Advanced settings > Install Extension).');
  process.exit(1);
});
// 'close' comes once SheetDiver's output has all been read.
child.on('close', (code, signal) => {
  if (signal) log(command + ' mcp was stopped by ' + signal);
  else if (code !== 0) log(command + ' mcp exited with status ' + code);
  // Exit once its last replies have reached Claude.
  process.stdout.write('', () => process.exit(signal ? 1 : (code ?? 0)));
});
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
