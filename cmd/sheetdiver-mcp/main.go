// Command sheetdiver-mcp is SheetDiver's stdio MCP server as a console
// program, for clients that launch servers over stdio (Claude Desktop's
// extension, Codex, …). It does what "sheetdiver mcp" does: relays to the
// running SheetDiver app, or serves the library itself while the app is
// closed. It has no window and starts nothing, so a client can run copies
// of it freely.
//
// The desktop app is a windowed program; on Windows that can't be a
// client's stdio server, so the installer ships this beside it. Build it
// as a console program (no -H windowsgui):
//
//	CGO_ENABLED=0 GOOS=windows go build -o sheetdiver-mcp.exe ./cmd/sheetdiver-mcp
//
// Usage:
//
//	sheetdiver-mcp [-port N] [-standalone]
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

	"github.com/aitakemyjob/sheetdiver/internal/mcpbridge"
	"github.com/aitakemyjob/sheetdiver/internal/mcpstdio"
)

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

// options are the command line's settings.
type options struct {
	port       int
	standalone bool
	version    bool
}

// parseArgs reads the command line. Claude Desktop fills --port from the
// extension's settings, which can come through blank or unexpected; that
// falls back to the port in SheetDiver's settings instead of a usage
// error, which would end the session without telling Claude why.
func parseArgs(args []string) (options, error) {
	fl := flag.NewFlagSet("sheetdiver-mcp", flag.ContinueOnError)
	fl.SetOutput(os.Stderr)
	port := fl.String("port", "", "the app's MCP port (default: from SheetDiver's settings, else 47480)")
	standalone := fl.Bool("standalone", false, "serve the library directly even when the app is running")
	showVersion := fl.Bool("version", false, "print the version")
	// Earlier extensions passed these; they no longer do anything.
	fl.String("app", "", "ignored")
	fl.String("launch", "", "ignored")
	// Accept "sheetdiver mcp"'s argument, so either command line works.
	if len(args) > 0 && args[0] == "mcp" {
		args = args[1:]
	}
	if err := fl.Parse(args); err != nil {
		return options{}, err
	}
	o := options{standalone: *standalone, version: *showVersion}
	// A number setting can arrive as "47480" or "47480.0".
	if p, err := strconv.ParseFloat(*port, 64); err == nil && p == float64(int(p)) {
		o.port = int(p) // out of range is "not given" (mcpbridge.FixedPort)
	} else if *port != "" {
		slog.Warn("sheetdiver-mcp: ignoring --port " + strconv.Quote(*port) + "; using SheetDiver's settings")
	}
	return o, nil
}

// main parses the flags and serves the client until it disconnects.
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = mcpstdio.Serve(ctx, mcpstdio.Options{
		Port:       mcpbridge.ResolvePort(mcpbridge.FixedPort(o.port), mcpbridge.SettingsPort{}),
		Standalone: o.standalone,
		Version:    version,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "sheetdiver-mcp:", err)
		os.Exit(1)
	}
}
