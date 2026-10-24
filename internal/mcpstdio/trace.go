package mcpstdio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// maxLog is how large the session log grows before it's rotated.
const maxLog = 1 << 20

// LogPath is where stdio sessions are logged: SHEETDIVER_MCP_LOG, else
// mcp.log in the user's cache folder (~/.cache/sheetdiver on Linux).
// A client may not keep a server's stderr; this file is there whatever
// the client does.
func LogPath() string {
	if p := os.Getenv("SHEETDIVER_MCP_LOG"); p != "" {
		return p
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "sheetdiver", "mcp.log")
}

// openLog opens the session log for appending, rotating it to mcp.log.1
// once it's large. Without a writable log, sessions aren't logged.
func openLog(path string) io.WriteCloser {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nopCloser{io.Discard}
	}
	if st, err := os.Stat(path); err == nil && st.Size() > maxLog {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nopCloser{io.Discard}
	}
	return f
}

// nopCloser adds a no-op Close to a writer.
type nopCloser struct{ io.Writer }

// Close does nothing.
func (nopCloser) Close() error { return nil }

// sessionLog logs one stdio session, each line tagged with its process.
type sessionLog struct {
	*log.Logger
}

// newSessionLog starts a session's log.
func newSessionLog(w io.Writer) sessionLog {
	return sessionLog{log.New(w, fmt.Sprintf("pid %d ", os.Getpid()), log.LstdFlags|log.Lmicroseconds|log.Lmsgprefix)}
}

// started records how the process was started: its command line, and
// what its stdin and stdout are (a pipe from the client, or something
// else, such as /dev/null when a launcher didn't pass the client's on).
func (s sessionLog) started(version string, port int) {
	s.Printf("start %s %s/%s: %s; stdin %s, stdout %s; app port %d", version, runtime.GOOS, runtime.GOARCH, strings.Join(os.Args, " "), describe(os.Stdin), describe(os.Stdout), port)
}

// describe says what an open file is.
func describe(f *os.File) string {
	if runtime.GOOS == "linux" {
		if to, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", f.Fd())); err == nil {
			return to
		}
	}
	st, err := f.Stat()
	if err != nil {
		return "unusable (" + err.Error() + ")"
	}
	switch m := st.Mode(); {
	case m&os.ModeNamedPipe != 0:
		return "a pipe"
	case m&os.ModeSocket != 0:
		return "a socket"
	case m&os.ModeCharDevice != 0:
		return "a terminal or device"
	case m.IsRegular():
		return "a file"
	default:
		return m.String()
	}
}

// traced wraps the client's side of a session to log each message's
// method and id (never its contents) and how the connection ended.
type traced struct {
	mcp.Transport
	log sessionLog
}

// Connect connects the wrapped transport.
func (t traced) Connect(ctx context.Context) (mcp.Connection, error) {
	c, err := t.Transport.Connect(ctx)
	if err != nil {
		t.log.Printf("can't use stdin/stdout: %v", err)
		return nil, err
	}
	return &tracedConn{Connection: c, log: t.log}, nil
}

// tracedConn logs a connection's traffic.
type tracedConn struct {
	mcp.Connection
	log sessionLog
}

// Read logs each message from the client, and the end of its input.
func (c *tracedConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	switch {
	case errors.Is(err, io.EOF):
		c.log.Printf("client closed its input")
	case err != nil:
		c.log.Printf("reading from the client: %v", err)
	default:
		c.log.Printf("→ %s", summary(msg))
	}
	return msg, err
}

// Write logs each message to the client.
func (c *tracedConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := c.Connection.Write(ctx, msg)
	if err != nil {
		c.log.Printf("← %s: not delivered: %v", summary(msg), err)
	} else {
		c.log.Printf("← %s", summary(msg))
	}
	return err
}

// summary names a message without its contents.
func summary(msg jsonrpc.Message) string {
	switch m := msg.(type) {
	case *jsonrpc.Request:
		if m.IsCall() {
			return fmt.Sprintf("%s (id %v)", m.Method, m.ID.Raw())
		}
		return m.Method
	case *jsonrpc.Response:
		if m.Error != nil {
			return fmt.Sprintf("error for id %v: %v", m.ID.Raw(), m.Error)
		}
		return fmt.Sprintf("result for id %v", m.ID.Raw())
	}
	return fmt.Sprintf("%T", msg)
}
