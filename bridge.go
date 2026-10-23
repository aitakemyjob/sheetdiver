package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/aitakemyjob/sheetdiver/internal/mcpbridge"
	"github.com/aitakemyjob/sheetdiver/internal/mcpstdio"
)

// runMCP connects a stdio MCP client (e.g. Claude Desktop) to the running
// SheetDiver app by relaying JSON-RPC messages to its HTTP endpoint. If the
// app is not running, it serves the tools directly over stdio so the agent
// still works (visual tools then have no window to show in).
func runMCP(port int, standalone bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return mcpstdio.Serve(ctx, mcpstdio.Options{
		Port:       mcpbridge.ResolvePort(mcpbridge.FixedPort(port), mcpbridge.SettingsPort{}),
		Standalone: standalone,
		Version:    version,
	})
}
