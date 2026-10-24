package mcpstdio

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// app is a stand-in for the SheetDiver app on a port: its health check
// and an MCP endpoint with a "where" tool that answers its name, and a
// "wait" tool that answers once release is closed.
type app struct {
	hs      *http.Server
	release chan struct{}
	called  chan struct{}
}

// startApp starts an app named name on port.
func startApp(t *testing.T, port int, name string) *app {
	t.Helper()
	a := &app{release: make(chan struct{}), called: make(chan struct{}, 1)}
	srv := mcp.NewServer(&mcp.Implementation{Name: "sheetdiver", Version: "test"}, nil)
	mcp.AddTool(srv, &mcp.Tool{Name: "where"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: name}}}, nil, nil
	})
	mcp.AddTool(srv, &mcp.Tool{Name: "wait"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		a.called <- struct{}{}
		select {
		case <-a.release:
		case <-ctx.Done():
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "done"}}}, nil, nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"app":"sheetdiver"}`)) //nolint:errcheck // test server
	})
	mux.Handle("/mcp", mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	l, err := listenOn(port)
	if err != nil {
		t.Fatal(err)
	}
	a.hs = &http.Server{Handler: mux}
	go a.hs.Serve(l) //nolint:errcheck // closed by stop
	t.Cleanup(a.stop)
	return a
}

// stop closes the app, as quitting it does.
func (a *app) stop() { a.hs.Close() }

// session starts a client session through Serve on port, as Claude
// Desktop does through the extension.
func session(t *testing.T, ctx context.Context, port int, standalone bool) (*mcp.ClientSession, *syncBuffer) {
	t.Helper()
	client, server := mcp.NewInMemoryTransports()
	var log syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{Port: port, Standalone: standalone, Version: "test", Local: server, Log: &log, Trace: &log})
	}()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, client, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cs.Close()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	return cs, &log
}

// where calls the where tool, returning its answer or the error.
func where(ctx context.Context, cs *mcp.ClientSession) string {
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "where"})
	if err != nil {
		return "error: " + err.Error()
	}
	if res.IsError {
		return "tool error"
	}
	return res.Content[0].(*mcp.TextContent).Text
}

// tools lists the session's tools' names.
func tools(t *testing.T, ctx context.Context, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		return []string{"error: " + err.Error()}
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestSessionOutlivesTheApp(t *testing.T) {
	// Closing the app and starting it again used to end the session, and
	// the client had to be restarted (in Claude Desktop, the extension
	// disabled and enabled) to use SheetDiver again.
	isolate(t)
	l, port := listen(t)
	l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	first := startApp(t, port, "first")
	cs, log := session(t, ctx, port, false)
	if got := where(ctx, cs); got != "first" {
		t.Fatalf("where = %q, want the running app", got)
	}

	first.stop()
	if got := tools(t, ctx, cs); !slices.Contains(got, "search") {
		t.Fatalf("with the app closed, tools = %v, want the library's\n%s", got, log.String())
	}

	startApp(t, port, "second")
	if got := tools(t, ctx, cs); !slices.Contains(got, "where") {
		t.Fatalf("with the app started again, tools = %v, want the app's", got)
	}
	if got := where(ctx, cs); got != "second" {
		t.Fatalf("where = %q, want the restarted app", got)
	}
	for _, want := range []string{"serving from the app", "went away", "serving from the library", "the app is running again"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("log lacks %q:\n%s", want, log.String())
		}
	}
}

func TestRestartedAppIsGivenTheSession(t *testing.T) {
	// The app restarted between two requests: the next one reaches the
	// new app, which is first given the client's initialize.
	isolate(t)
	l, port := listen(t)
	l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	first := startApp(t, port, "first")
	cs, _ := session(t, ctx, port, false)
	if got := where(ctx, cs); got != "first" {
		t.Fatalf("where = %q", got)
	}
	first.stop()
	startApp(t, port, "second")
	if got := where(ctx, cs); got != "second" {
		t.Fatalf("where = %q, want the restarted app", got)
	}
}

func TestRequestRunningWhenTheAppClosesIsAnswered(t *testing.T) {
	// A request the app was running when it closed is answered with an
	// error, not left waiting; the next is served.
	isolate(t)
	l, port := listen(t)
	l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	a := startApp(t, port, "first")
	cs, _ := session(t, ctx, port, false)
	errc := make(chan error, 1)
	go func() {
		_, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "wait"})
		errc <- err
	}()
	<-a.called
	// A long request doesn't hold up the others.
	if got := where(ctx, cs); got != "first" {
		t.Fatalf("where, while wait runs = %q", got)
	}
	a.stop()
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "try it again") {
			t.Fatalf("wait: %v, want the try-again error", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the running request was never answered")
	}
	if got := tools(t, ctx, cs); !slices.Contains(got, "search") {
		t.Fatalf("tools = %v, want the library's", got)
	}
}

func TestStandaloneStaysWithTheLibrary(t *testing.T) {
	isolate(t)
	l, port := listen(t)
	l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	startApp(t, port, "app")
	cs, _ := session(t, ctx, port, true)
	if got := tools(t, ctx, cs); slices.Contains(got, "where") || !slices.Contains(got, "search") {
		t.Fatalf("tools = %v, want the library's", got)
	}
}

// listenOn listens on a loopback port, retrying briefly while the last
// app to use it lets go.
func listenOn(port int) (net.Listener, error) {
	var err error
	for range 50 {
		var l net.Listener
		if l, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			return l, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, err
}
