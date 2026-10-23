package mcpstdio

import (
	"bytes"
	"context"
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

// probe is a client that sends Claude Desktop's version probe and closes
// its input straight away, as Claude Desktop does. Replies go to out.
func probe(out *syncBuffer) mcp.Transport {
	const req = `{"jsonrpc":"2.0","id":0,"method":"server/discover","params":{}}` + "\n"
	return &mcp.IOTransport{Reader: io.NopCloser(strings.NewReader(req)), Writer: nopWriteCloser{out}}
}

// listen returns a loopback listener and its port.
func listen(t *testing.T) (net.Listener, int) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return l, l.Addr().(*net.TCPAddr).Port
}

// isolate points the config and library at a temporary folder.
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHEETDIVER_CONFIG", filepath.Join(dir, "config.json"))
	t.Setenv("SHEETDIVER_LIBRARY", filepath.Join(dir, "lib"))
	t.Setenv("SHEETDIVER_MCP_LOG", filepath.Join(dir, "logs", "mcp.log"))
}

func TestServesTheLibraryWhileTheAppIsClosed(t *testing.T) {
	isolate(t)
	l, port := listen(t)
	l.Close() // nothing answers on the port
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out, log syncBuffer
	if err := Serve(ctx, Options{Port: port, Version: "test", Local: probe(&out), Log: &log}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":0,"error"`) {
		t.Errorf("replied %q, want the probe answered", out.String())
	}
	if !strings.Contains(log.String(), "serving MCP standalone") {
		t.Errorf("logged %q", log.String())
	}
}

func TestRelaysToTheRunningApp(t *testing.T) {
	isolate(t)
	l, port := listen(t)
	// A stand-in for the app: its health check and an MCP endpoint.
	srv := mcp.NewServer(&mcp.Implementation{Name: "app", Version: "test"}, nil)
	mux := http.NewServeMux()
	var probed sync.WaitGroup
	probed.Add(1)
	var once sync.Once
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"app":"sheetdiver"}`)) //nolint:errcheck // test server
	})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(probed.Done)
		h.ServeHTTP(w, r)
	}))
	hs := &http.Server{Handler: mux}
	go hs.Serve(l) //nolint:errcheck // closed below
	defer hs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out, log syncBuffer
	if err := Serve(ctx, Options{Port: port, Local: probe(&out), Log: &log}); err != nil {
		t.Fatal(err)
	}
	probed.Wait() // the probe went to the app
	if !strings.Contains(out.String(), `"id":0,"error"`) || log.String() != "" {
		t.Errorf("replied %q, logged %q", out.String(), log.String())
	}
}

func TestLogsEachSession(t *testing.T) {
	// The session log says how a session went whatever the client does
	// with stderr: how it was started, each message, and how it ended.
	isolate(t)
	l, port := listen(t)
	l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out, log syncBuffer
	if err := Serve(ctx, Options{Port: port, Version: "v9", Local: probe(&out), Log: &log}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(LogPath())
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{"start v9", "stdin ", "app port", "the app isn't running: serving the library", "→ server/discover (id 0)", "← error for id 0", "client closed its input", "ended"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "params") {
		t.Errorf("the log has messages' contents:\n%s", got)
	}
}

func TestLogRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxLog+1), 0o644); err != nil {
		t.Fatal(err)
	}
	w := openLog(path)
	w.Write([]byte("new\n")) //nolint:errcheck // checked below
	w.Close()
	if b, _ := os.ReadFile(path); string(b) != "new\n" {
		t.Errorf("log = %q", b)
	}
	if st, err := os.Stat(path + ".1"); err != nil || st.Size() != maxLog+1 {
		t.Errorf("rotated: %v", err)
	}
}

// probeThenInitialize is how Claude Desktop probes in place: the stateless
// protocol's server/discover, then the classic initialize and tools/list
// on the same connection, then it closes its input.
const probeThenInitialize = `{"jsonrpc":"2.0","id":0,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"claude-ai","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"claude-ai","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
`

func TestInitializeAfterAProbeOnTheSameConnection(t *testing.T) {
	// Answering the probe in the new protocol made the standalone server
	// refuse the initialize that follows ("duplicate initialize"), so a
	// client probing in place couldn't connect.
	isolate(t)
	l, port := listen(t)
	l.Close() // the app is closed: the library is served directly
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out, log syncBuffer
	local := &mcp.IOTransport{Reader: io.NopCloser(strings.NewReader(probeThenInitialize)), Writer: nopWriteCloser{&out}}
	if err := Serve(ctx, Options{Port: port, Version: "test", Local: local, Log: &log}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, `"id":1,"result"`) || strings.Contains(got, "duplicate") {
		t.Errorf("initialize after the probe: %s", got)
	}
	if !strings.Contains(got, `"id":2,"result"`) || !strings.Contains(got, `"name":"search"`) {
		t.Errorf("tools/list after it: %.300s", got)
	}
	if !strings.Contains(got, `"id":0,"error"`) && !strings.Contains(got, `"supportedVersions":["2025-11-25"`) {
		t.Errorf("the probe should be told 2025-11-25 is the newest: %.300s", got)
	}
}
