package mcpbridge

import (
	"context"
	"errors"
	"io"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeAppNotRunning is the JSON-RPC error code for requests made while
// the app can't be reached (in the implementation-defined server range).
const CodeAppNotRunning = -32001

// Refuse answers every request from local with an error saying why the
// app can't be reached, until the client closes the connection. Clients
// show that message instead of a bare "server disconnected".
func Refuse(ctx context.Context, local mcp.Transport, reason string) error {
	conn, err := local.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	for {
		msg, err := conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		req, ok := msg.(*jsonrpc.Request)
		if !ok || !req.IsCall() {
			continue // notifications and stray responses need no answer
		}
		resp := &jsonrpc.Response{ID: req.ID, Error: &jsonrpc.Error{Code: CodeAppNotRunning, Message: reason}}
		if err := conn.Write(ctx, resp); err != nil {
			return err
		}
	}
}
