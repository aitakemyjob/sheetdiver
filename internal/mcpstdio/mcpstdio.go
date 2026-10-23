// Package mcpstdio serves SheetDiver's MCP tools to a client that launches
// servers over stdio (Claude Desktop, Codex…). It relays to the running
// app when there is one, so the agent's actions show in its window, and
// otherwise opens the library and serves it directly. Both answer at once
// and change nothing until asked, so a client can start several copies
// (Claude Desktop starts a short-lived one to probe the protocol version)
// without side effects.
//
// The app's "mcp" subcommand and the console program cmd/sheetdiver-mcp
// both run it. Only protocol messages go to stdout; logs go to stderr.
package mcpstdio

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/mcpbridge"
	"github.com/aitakemyjob/sheetdiver/internal/mcpserver"
)

// Options configure a stdio session.
type Options struct {
	// Port is the app's MCP port.
	Port int
	// Standalone serves the library even when the app is running.
	Standalone bool
	// Version is SheetDiver's version, reported to the client.
	Version string
	// Local is the client's side (default: this process's stdin/stdout).
	Local mcp.Transport
	// Log receives diagnostics (default: stderr).
	Log io.Writer
}

// session is one way of serving the client.
type session interface {
	serve(ctx context.Context, local mcp.Transport) error
}

// Serve runs one session until the client disconnects.
func Serve(ctx context.Context, o Options) error {
	if o.Local == nil {
		o.Local = &mcp.StdioTransport{}
	}
	if o.Log == nil {
		o.Log = os.Stderr
	}
	return choose(ctx, o).serve(ctx, o.Local)
}

// choose relays to the running app unless asked to serve standalone.
func choose(ctx context.Context, o Options) session {
	if !o.Standalone && mcpbridge.Running(ctx, mcpbridge.BaseURL(o.Port)) {
		return relay{endpoint: mcpbridge.Endpoint(o.Port)}
	}
	fmt.Fprintln(o.Log, "sheetdiver: app not running on", mcpbridge.BaseURL(o.Port), "- serving MCP standalone over stdio")
	return library{version: o.Version}
}

// relay passes the session to the running app.
type relay struct {
	endpoint string
}

// serve relays until the client disconnects.
func (r relay) serve(ctx context.Context, local mcp.Transport) error {
	return mcpbridge.Relay(ctx, local, r.endpoint)
}

// library serves the library directly, while the app is closed.
type library struct {
	version string
}

// serve opens the library and serves its tools until the client
// disconnects, answering what it already asked first.
func (l library) serve(ctx context.Context, local mcp.Transport) error {
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}
	store, err := config.Open(path)
	if err != nil {
		return err
	}
	svc, err := core.New(store, l.version)
	if err != nil {
		return err
	}
	defer svc.Close() //nolint:errcheck // nothing to do about it on the way out
	return mcpserver.New(svc).Run(ctx, mcpbridge.Drain{Transport: local})
}
