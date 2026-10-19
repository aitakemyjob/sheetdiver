package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/mcpserver"
)

// runMCP connects a stdio MCP client (e.g. Claude Desktop) to the running
// SheetDiver app by relaying JSON-RPC messages to its HTTP endpoint. If the
// app is not running, it serves the tools directly over stdio so the agent
// still works (visual tools then have no window to show in).
func runMCP(port int, standalone bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if port == 0 {
		port = config.DefaultMCPPort
		if p, err := config.DefaultPath(); err == nil {
			if st, err := config.Open(p); err == nil {
				port = st.Get().Settings.MCPPort
			}
		}
	}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if !standalone && appRunning(base) {
		return bridge(ctx, base+"/mcp")
	}
	fmt.Fprintln(os.Stderr, "sheetdiver: app not running on", base, "- serving MCP standalone over stdio")
	a, err := bootstrap(port, false)
	if err != nil {
		return err
	}
	defer a.core.Close()
	return mcpserver.New(a.core).Run(ctx)
}

// appRunning reports whether a SheetDiver instance answers /healthz at base.
func appRunning(base string) bool {
	c := http.Client{Timeout: 700 * time.Millisecond}
	resp, err := c.Get(base + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h struct{ App string }
	return json.NewDecoder(resp.Body).Decode(&h) == nil && h.App == "sheetdiver"
}

// bridge relays JSON-RPC messages between stdio and the running app's
// streamable HTTP MCP endpoint until either side closes.
func bridge(ctx context.Context, endpoint string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	local, err := (&mcp.StdioTransport{}).Connect(ctx)
	if err != nil {
		return err
	}
	defer local.Close()
	remote, err := (&mcp.StreamableClientTransport{Endpoint: endpoint}).Connect(ctx)
	if err != nil {
		return err
	}
	defer remote.Close()
	errc := make(chan error, 2)
	pump := func(from, to mcp.Connection) {
		for {
			msg, err := from.Read(ctx)
			if err != nil {
				errc <- err
				return
			}
			if err := to.Write(ctx, msg); err != nil {
				errc <- err
				return
			}
		}
	}
	go pump(local, remote)
	go pump(remote, local)
	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		if ctx.Err() != nil {
			return nil
		}
		// stdin closing is the normal way for a client to end the session.
		if err != nil && err.Error() == "EOF" {
			return nil
		}
		return err
	}
}
