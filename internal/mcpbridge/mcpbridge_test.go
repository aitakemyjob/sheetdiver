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
	"sync/atomic"
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

// connect runs b with an in-memory client and returns the connected
// client session, the bridge's result and the client's connect error.
func connect(t *testing.T, b *Bridge) (*mcp.ClientSession, <-chan error, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	client, server := mcp.NewInMemoryTransports()
	b.Local = server
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, client, nil)
	return cs, done, err
}

// echo calls the echo tool through cs.
func echo(t *testing.T, cs *mcp.ClientSession) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func TestBridgeRelaysToTheRunningApp(t *testing.T) {
	port := freePort(t)
	serveOn(t, port, fakeApp(t))
	b := &Bridge{Port: port, Starter: startFunc(func(string) error { t.Error("started a running app"); return nil })}
	cs, done, err := connect(t, b)
	if err != nil {
		t.Fatal(err)
	}
	if got := echo(t, cs); got != "hi" {
		t.Errorf("echo = %q", got)
	}
	cs.Close()
	if err := <-done; err != nil {
		t.Errorf("run: %v", err)
	}
}

// startFunc adapts a function to Starter.
type startFunc func(string) error

func (f startFunc) Start(p string) error { return f(p) }

func TestBridgeStartsTheApp(t *testing.T) {
	port := freePort(t)
	// Something else holds the port until the app is started, then the app
	// answers a little later.
	var up atomic.Bool
	app := fakeApp(t)
	serveOn(t, port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up.Load() {
			http.Error(w, "starting", http.StatusServiceUnavailable)
			return
		}
		app.ServeHTTP(w, r)
	}))
	var started string
	b := &Bridge{
		Port: port,
		App:  Chain{AppPath(""), AppPath(filepath.Join(t.TempDir(), "gone")), fixed("/opt/sheetdiver")},
		Starter: startFunc(func(p string) error {
			started = p
			time.AfterFunc(300*time.Millisecond, func() { up.Store(true) })
			return nil
		}),
		Poll: 50 * time.Millisecond,
	}
	cs, _, err := connect(t, b)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if started != "/opt/sheetdiver" {
		t.Errorf("started %q", started)
	}
	if got := echo(t, cs); got != "hi" {
		t.Errorf("echo = %q", got)
	}
}

// fixed is a locator that always finds path.
type fixed string

func (f fixed) Locate() (string, bool) { return string(f), true }

func TestBridgeExplainsAMissingApp(t *testing.T) {
	cases := []struct {
		name string
		b    Bridge
		want string
	}{
		{"not allowed to start", Bridge{}, "Open the SheetDiver app"},
		{"not found", Bridge{Starter: startFunc(func(string) error { return nil }), App: Chain{}}, "wasn't found"},
		{"start fails", Bridge{Starter: startFunc(func(string) error { return errors.New("access denied") }), App: fixed(`C:\SheetDiver\sheetdiver.exe`)}, "access denied"},
		{"never answers", Bridge{Starter: startFunc(func(string) error { return nil }), App: fixed("x"), StartTimeout: 200 * time.Millisecond, Poll: 50 * time.Millisecond}, "doesn't answer"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.b.Port = freePort(t)
			_, done, err := connect(t, &c.b)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("connect error = %v, want %q", err, c.want)
			}
			if err := <-done; err != nil {
				t.Errorf("run: %v", err)
			}
		})
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

func TestBesideFindsTheApp(t *testing.T) {
	dir := t.TempDir()
	if _, ok := (Beside{Dir: dir}).Locate(); ok {
		t.Error("found an app in an empty folder")
	}
	app := filepath.Join(dir, AppExecutable)
	if err := os.WriteFile(app, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got, ok := (Beside{Dir: dir}).Locate(); !ok || got != app {
		t.Errorf("Beside = %q, %v", got, ok)
	}
	if _, ok := AppPath(dir).Locate(); ok {
		t.Error("a folder isn't an app")
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

func TestRepliesBeforeTheClientsInputCloses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	t.Run("relay", func(t *testing.T) {
		port := freePort(t)
		serveOn(t, port, fakeApp(t))
		for range 5 {
			var out syncBuffer
			if err := Relay(ctx, closedStdio(versionProbe, &out), Endpoint(port)); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), `"id":0,"error"`) {
				t.Fatalf("relayed %q, want the probe's reply", out.String())
			}
		}
	})
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
