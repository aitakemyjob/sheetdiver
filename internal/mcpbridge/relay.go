package mcpbridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Running reports whether a SheetDiver app answers /healthz at base.
func Running(ctx context.Context, base string) bool {
	ctx, cancel := context.WithTimeout(ctx, 700*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var h struct{ App string }
	return json.NewDecoder(resp.Body).Decode(&h) == nil && h.App == "sheetdiver"
}

// Relay passes JSON-RPC messages between local (the client's stdio) and
// the app's streamable HTTP endpoint until either side closes. The HTTP
// transport handles the protocol's details: JSON and SSE replies, the
// Mcp-Session-Id header and 202 Accepted for notifications. When the
// client closes its input, the replies it is still owed are delivered
// first.
func Relay(ctx context.Context, local mcp.Transport, endpoint string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	in, err := Drain{Transport: local}.Connect(ctx)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := (&mcp.StreamableClientTransport{Endpoint: endpoint}).Connect(ctx)
	if err != nil {
		return err
	}
	defer out.Close()
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
	go pump(in, out)
	go pump(out, in)
	select {
	case <-ctx.Done():
		return nil
	case err := <-errc:
		// stdin closing is the normal way for a client to end the session.
		if ctx.Err() != nil || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}
