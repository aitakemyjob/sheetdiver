// Command sheetdiver-mcp is SheetDiver's stdio MCP bridge as a small
// console program, for clients that launch servers over stdio (Claude
// Desktop's extension, Codex, …). It relays to the running SheetDiver app
// and starts the app when it isn't open.
//
// The desktop app is a windowed program; on Windows that can't be a
// client's stdio server, so the installer ships this beside it. Build it
// as a console program (no -H windowsgui):
//
//	CGO_ENABLED=0 GOOS=windows go build -o sheetdiver-mcp.exe ./cmd/sheetdiver-mcp
//
// Usage:
//
//	sheetdiver-mcp [-port N] [-app PATH] [-launch=false]
//
// Stdout carries only MCP messages; logs go to stderr.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/mcpbridge"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

// options are the command line's settings.
type options struct {
	port    int
	app     string
	launch  bool
	version bool
}

// parseArgs reads the command line. Claude Desktop fills --port and
// --launch from the extension's settings, which can come through blank
// or unexpected; those fall back to the defaults (the port from
// SheetDiver's settings, starting the app) instead of a usage error,
// which would end the session without telling Claude why.
func parseArgs(args []string) (options, error) {
	fl := flag.NewFlagSet("sheetdiver-mcp", flag.ContinueOnError)
	fl.SetOutput(os.Stderr)
	port := fl.String("port", "", "the app's MCP port (default: from SheetDiver's settings, else 47480)")
	app := fl.String("app", "", "SheetDiver executable to start when it isn't running (default: found beside this program or from the installer's record)")
	launch := fl.String("launch", "true", "start SheetDiver when it isn't running (true or false)")
	showVersion := fl.Bool("version", false, "print the version")
	// Accept "sheetdiver mcp"'s argument, so either command line works.
	if len(args) > 0 && args[0] == "mcp" {
		args = args[1:]
	}
	if err := fl.Parse(args); err != nil {
		return options{}, err
	}
	o := options{app: *app, launch: true, version: *showVersion}
	// A number setting can arrive as "47480" or "47480.0".
	if p, err := strconv.ParseFloat(*port, 64); err == nil && p == float64(int(p)) {
		o.port = int(p) // out of range is "not given" (mcpbridge.FixedPort)
	} else if *port != "" {
		slog.Warn("sheetdiver-mcp: ignoring --port " + strconv.Quote(*port) + "; using SheetDiver's settings")
	}
	if l, err := strconv.ParseBool(*launch); err == nil {
		o.launch = l
	} else if *launch != "" {
		slog.Warn("sheetdiver-mcp: ignoring --launch " + strconv.Quote(*launch) + "; starting SheetDiver when it's closed")
	}
	return o, nil
}

// main parses the flags and runs the bridge until the client disconnects.
func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	o, err := parseArgs(os.Args[1:])
	if err != nil {
		os.Exit(2)
	}
	if o.version {
		fmt.Fprintln(os.Stderr, "sheetdiver-mcp", version)
		return
	}

	b := &mcpbridge.Bridge{
		Port:  mcpbridge.ResolvePort(mcpbridge.FixedPort(o.port), mcpbridge.SettingsPort{}),
		Local: &mcp.StdioTransport{},
		App:   append(mcpbridge.Chain{mcpbridge.AppPath(o.app)}, mcpbridge.DefaultLocators()...),
	}
	if o.launch {
		b.Starter = mcpbridge.Detached{}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := b.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "sheetdiver-mcp:", err)
		os.Exit(1)
	}
}
