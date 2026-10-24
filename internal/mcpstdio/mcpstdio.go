// Package mcpstdio serves SheetDiver's MCP tools to a client that launches
// servers over stdio (Claude Desktop, Codex…). It relays to the running
// app when there is one, so the agent's actions show in its window, and
// otherwise opens the library and serves it directly, switching between
// the two as the app is closed and started again without the client
// having to reconnect. Both answer at once
// and change nothing until asked, so a client can start several copies
// (Claude Desktop starts a short-lived one to probe the protocol version)
// without side effects.
//
// The app's "mcp" subcommand and the console program cmd/sheetdiver-mcp
// both run it. Only protocol messages go to stdout; logs go to stderr.
package mcpstdio

import (
	"context"
	"io"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/mcpbridge"
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
	// Trace receives the session's log: how it was started, each
	// message's method and id, and how it ended (default: the file at
	// LogPath).
	Trace io.Writer
}

// Serve runs one session until the client disconnects, logging it.
func Serve(ctx context.Context, o Options) (err error) {
	if o.Local == nil {
		o.Local = &mcp.StdioTransport{}
	}
	if o.Log == nil {
		o.Log = os.Stderr
	}
	if o.Trace == nil {
		f := openLog(LogPath())
		defer f.Close()
		o.Trace = f
	}
	l := newSessionLog(o.Trace)
	l.started(o.Version, o.Port)
	defer func() {
		if p := recover(); p != nil {
			l.Printf("crashed: %v", p)
			panic(p)
		}
		switch {
		case ctx.Err() != nil:
			l.Printf("stopped: %v", context.Cause(ctx))
		case err != nil:
			l.Printf("ended: %v", err)
		default:
			l.Printf("ended")
		}
	}()
	client, err := mcpbridge.Drain{Transport: traced{Transport: o.Local, log: l}}.Connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	return (&switchboard{o: o, log: l, client: client}).run(ctx)
}
