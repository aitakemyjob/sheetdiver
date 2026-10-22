// Package mcpbridge connects a stdio MCP client (Claude Desktop, Codex…)
// to the running SheetDiver app by relaying JSON-RPC messages between
// stdin/stdout and the app's streamable HTTP endpoint
// (http://127.0.0.1:<port>/mcp). It has no UI and no library of its own,
// so it builds into a small console program (cmd/sheetdiver-mcp) as well
// as the app's own "mcp" subcommand.
//
// Only protocol messages go to stdout; everything else is logged to
// stderr, where the client keeps it as the server's log.
package mcpbridge

import (
	"fmt"

	"github.com/aitakemyjob/sheetdiver/internal/config"
)

// PortSource is one place the app's MCP port can come from. A source that
// has no opinion returns ok=false and the next one is asked.
type PortSource interface {
	Port() (port int, ok bool)
}

// FixedPort is a port given on the command line; 0 means "not given".
type FixedPort int

// Port returns the port when one was given.
func (p FixedPort) Port() (int, bool) { return int(p), p > 0 && p <= 65535 }

// SettingsPort is the port saved in the app's settings file (path "" is
// the default config location, or SHEETDIVER_CONFIG).
type SettingsPort struct{ Path string }

// Port reads the settings file. A missing or unreadable file has no
// opinion. The file is only read, never written.
func (s SettingsPort) Port() (int, bool) {
	path := s.Path
	if path == "" {
		p, err := config.DefaultPath()
		if err != nil {
			return 0, false
		}
		path = p
	}
	st, err := config.Open(path)
	if err != nil {
		return 0, false
	}
	return FixedPort(st.Get().Settings.MCPPort).Port()
}

// ResolvePort asks each source in turn and falls back to the app's
// default port (47480).
func ResolvePort(sources ...PortSource) int {
	for _, s := range sources {
		if p, ok := s.Port(); ok {
			return p
		}
	}
	return config.DefaultMCPPort
}

// BaseURL is the app's loopback address on port.
func BaseURL(port int) string { return fmt.Sprintf("http://127.0.0.1:%d", port) }

// Endpoint is the app's MCP endpoint on port.
func Endpoint(port int) string { return BaseURL(port) + "/mcp" }
