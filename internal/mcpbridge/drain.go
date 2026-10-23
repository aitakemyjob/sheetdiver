package mcpbridge

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Drain wraps the client's side of a stdio session so that the client
// closing its input is reported only once every request already read has
// been answered. A client may send a request and close stdin straight
// away (Claude Desktop's version probe does), and still expects the
// reply; without this, the session ends and drops it.
type Drain struct {
	// Transport is the client's side, e.g. &mcp.StdioTransport{}.
	Transport mcp.Transport
	// Timeout bounds the wait for outstanding replies (default 30s).
	Timeout time.Duration
}

// Connect connects the wrapped transport.
func (d Drain) Connect(ctx context.Context) (mcp.Connection, error) {
	c, err := d.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &drainConn{Connection: c, timeout: timeout, pending: map[jsonrpc.ID]struct{}{}}, nil
}

// drainConn tracks the client's requests that haven't been answered yet.
type drainConn struct {
	mcp.Connection
	timeout time.Duration

	mu      sync.Mutex
	pending map[jsonrpc.ID]struct{}
	// answered is closed when the last pending request is answered.
	answered chan struct{}
}

// Read returns the client's next message. On end of input it first waits
// for the replies still owed.
func (c *drainConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if err == nil {
		if req, ok := msg.(*jsonrpc.Request); ok && req.IsCall() {
			c.mu.Lock()
			if len(c.pending) == 0 {
				c.answered = make(chan struct{})
			}
			c.pending[req.ID] = struct{}{}
			c.mu.Unlock()
		}
		return msg, nil
	}
	if errors.Is(err, io.EOF) {
		c.waitAnswered(ctx)
	}
	return nil, err
}

// Write sends a message to the client, noting replies.
func (c *drainConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := c.Connection.Write(ctx, msg)
	if resp, ok := msg.(*jsonrpc.Response); ok {
		c.mu.Lock()
		if _, owed := c.pending[resp.ID]; owed {
			delete(c.pending, resp.ID)
			if len(c.pending) == 0 {
				close(c.answered)
			}
		}
		c.mu.Unlock()
	}
	return err
}

// waitAnswered blocks until no reply is owed, the timeout passes or ctx
// ends.
func (c *drainConn) waitAnswered(ctx context.Context) {
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return
	}
	answered := c.answered
	c.mu.Unlock()
	t := time.NewTimer(c.timeout)
	defer t.Stop()
	select {
	case <-answered:
	case <-t.C:
	case <-ctx.Done():
	}
}
