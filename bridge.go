package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/mcpbridge"
	"github.com/aitakemyjob/sheetdiver/internal/mcpserver"
)

// runMCP connects a stdio MCP client (e.g. Claude Desktop) to the running
// SheetDiver app by relaying JSON-RPC messages to its HTTP endpoint. If the
// app is not running, it serves the tools directly over stdio so the agent
// still works (visual tools then have no window to show in).
func runMCP(port int, standalone bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	port = mcpbridge.ResolvePort(mcpbridge.FixedPort(port), mcpbridge.SettingsPort{})
	if !standalone && mcpbridge.Running(ctx, mcpbridge.BaseURL(port)) {
		return mcpbridge.Relay(ctx, &mcp.StdioTransport{}, mcpbridge.Endpoint(port))
	}
	fmt.Fprintln(os.Stderr, "sheetdiver: app not running on", mcpbridge.BaseURL(port), "- serving MCP standalone over stdio")
	a, err := bootstrap(port, false)
	if err != nil {
		return err
	}
	defer a.core.Close()
	return mcpserver.New(a.core).Run(ctx)
}
