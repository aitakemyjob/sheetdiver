package mcpstdio

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
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
