package mcpbridge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeApp serves /healthz and an MCP endpoint with one "echo" tool, like
// the app does.
func fakeApp(t *testing.T) http.Handler {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "sheetdiver", Version: "test"}, nil)
	type in struct {
		Text string `json:"text"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "echo"}, func(_ context.Context, _ *mcp.CallToolRequest, a in) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: a.Text}}}, nil, nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte(`{"app":"sheetdiver"}`)) })
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	return mux
}

// freePort returns a loopback port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// serveOn serves h on port until the test ends.
func serveOn(t *testing.T, port int, h http.Handler) {
	t.Helper()
	l, err := net.Listen("tcp", BaseURL(port)[len("http://"):])
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h}
	go s.Serve(l) //nolint:errcheck // closed below
	t.Cleanup(func() { s.Close() })
}

func TestRunning(t *testing.T) {
	port := freePort(t)
	serveOn(t, port, fakeApp(t))
	if !Running(context.Background(), BaseURL(port)) {
		t.Fatal("the app isn't seen running")
	}
	if Running(context.Background(), BaseURL(freePort(t))) {
		t.Error("nothing listening is seen running")
	}
}

func TestResolvePort(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "config.json")
	if err := os.WriteFile(settings, []byte(`{"settings":{"mcpPort":51000}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		sources []PortSource
		want    int
	}{
		{"flag wins", []PortSource{FixedPort(50000), SettingsPort{Path: settings}}, 50000},
		{"settings next", []PortSource{FixedPort(0), SettingsPort{Path: settings}}, 51000},
		{"no settings file", []PortSource{FixedPort(0), SettingsPort{Path: filepath.Join(dir, "none.json")}}, 47480},
		{"out of range", []PortSource{FixedPort(70000)}, 47480},
	}
	for _, c := range cases {
		if got := ResolvePort(c.sources...); got != c.want {
			t.Errorf("%s: port = %d, want %d", c.name, got, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "none.json")); err == nil {
		t.Error("reading the port wrote a settings file")
	}
}

// closedStdio is a client that sends lines and closes its input at once,
// as Claude Desktop's version probe does. Replies collect in out.
func closedStdio(lines string, out *syncBuffer) mcp.Transport {
	return &mcp.IOTransport{Reader: io.NopCloser(strings.NewReader(lines)), Writer: nopWriteCloser{out}}
}

// syncBuffer is a bytes.Buffer safe for concurrent writes and reads.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// Write appends p.
func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

// String returns what was written.
func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// nopWriteCloser adds a no-op Close to a writer.
type nopWriteCloser struct{ io.Writer }

// Close does nothing.
func (nopWriteCloser) Close() error { return nil }

// versionProbe is Claude Desktop's version probe: a request the server doesn't
// know, which it must still answer.
const versionProbe = `{"jsonrpc":"2.0","id":0,"method":"server/discover","params":{}}` + "\n"

func TestDrainRepliesBeforeTheClientsInputCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	t.Run("server", func(t *testing.T) {
		srv := mcp.NewServer(&mcp.Implementation{Name: "sheetdiver", Version: "test"}, nil)
		var out syncBuffer
		if err := srv.Run(ctx, Drain{Transport: closedStdio(versionProbe, &out)}); err != nil && !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `"id":0,"error"`) {
			t.Fatalf("served %q, want the probe's reply", out.String())
		}
	})
}
