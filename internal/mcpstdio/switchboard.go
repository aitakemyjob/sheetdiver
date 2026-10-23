package mcpstdio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/mcpbridge"
	"github.com/aitakemyjob/sheetdiver/internal/mcpserver"
)

// errInFlight answers a request that was running when its backend went
// away: the client can simply ask again.
var errInFlight = &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: "SheetDiver was closed or restarted while this request ran; try it again"}

// appClient reaches the app on loopback with a new connection for each
// message. A pooled connection the app dropped when it closed would fail
// a message the app never got as if it had been cut off while running;
// with fresh connections, an app that's gone refuses the connection, and
// the message is safely sent elsewhere.
var appClient = &http.Client{Transport: &http.Transport{DisableKeepAlives: true, Proxy: nil}}

// replayTimeout bounds how long a new backend has to answer the client's
// replayed initialize.
const replayTimeout = 15 * time.Second

// backend serves the client's session: the running app (relayed over
// HTTP), or the library, served in this process while the app is closed.
type backend struct {
	name    string
	library bool
	conn    mcp.Connection
	release func()
	gone    chan struct{}

	// Guarded by the switchboard's mu.
	dead    bool
	pending map[jsonrpc.ID]bool                   // the client's calls it hasn't answered
	replays map[jsonrpc.ID]chan *jsonrpc.Response // replayed calls, answered to the switchboard
}

// switchboard keeps a client's session going however the app comes and
// goes. It sends the client's messages to the app while it runs and to
// the library while it doesn't; when one goes away, the requests it was
// running are answered with an error, and the next message goes to the
// other, which is first given the client's initialize again. The client
// sees one session throughout: it never has to reconnect because the app
// was closed or restarted.
type switchboard struct {
	o      Options
	log    sessionLog
	client mcp.Connection
	lib    *libraryServer

	mu  sync.Mutex
	cur *backend

	// retry takes back messages that never reached a backend that went
	// away, to be sent again; done is closed when the session ends.
	retry chan inbound
	done  chan struct{}

	// Only touched by the client loop (run and what it calls).
	init, initialized *jsonrpc.Request
	replays           int
}

// inbound is a message from the client, and whether it's being sent again.
type inbound struct {
	msg   jsonrpc.Message
	retry bool
}

// unsent reports whether a write failed without the backend getting the
// message, so it's safe to send again: nothing was listening (the app had
// closed), or the app had restarted and didn't know the session.
func unsent(err error) bool {
	var op *net.OpError
	return errors.Is(err, mcp.ErrSessionMissing) || errors.As(err, &op) && op.Op == "dial"
}

// run routes the client's messages until it disconnects.
func (s *switchboard) run(ctx context.Context) error {
	s.retry, s.done = make(chan inbound, 16), make(chan struct{})
	defer close(s.done)
	defer s.closeAll()
	type read struct {
		msg jsonrpc.Message
		err error
	}
	reads := make(chan read)
	go func() {
		for {
			msg, err := s.client.Read(ctx)
			select {
			case reads <- read{msg, err}:
			case <-s.done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		select {
		case r := <-reads:
			if r.err != nil {
				if ctx.Err() != nil || errors.Is(r.err, io.EOF) {
					return nil
				}
				return r.err
			}
			s.route(ctx, inbound{msg: r.msg})
		case in := <-s.retry:
			s.route(ctx, in)
		case <-ctx.Done():
			return nil
		}
	}
}

// route sends a message from the client to the live backend. A call runs
// on its own, so a long one doesn't hold up the others; initialize and
// notifications are sent in order.
func (s *switchboard) route(ctx context.Context, in inbound) {
	req, _ := in.msg.(*jsonrpc.Request)
	if req != nil && req.Method == "initialize" {
		s.init = req
	}
	b, err := s.ready(ctx, req)
	if err != nil {
		if !in.retry {
			s.route(ctx, inbound{msg: in.msg, retry: true})
			return
		}
		s.refuse(ctx, in.msg, err)
		return
	}
	if req != nil && req.IsCall() {
		s.mu.Lock()
		b.pending[req.ID] = true
		s.mu.Unlock()
		if req.Method != "initialize" {
			go func() {
				if again, ok := s.send(ctx, b, in); ok {
					select {
					case s.retry <- again:
					case <-s.done:
					}
				}
			}()
			return
		}
	}
	if again, ok := s.send(ctx, b, in); ok {
		s.route(ctx, again)
	}
}

// send writes a message to a backend. If the backend has gone away, a
// message that never reached it is returned to be sent again (once); a
// call that did reach it is answered with an error, as it may have run.
func (s *switchboard) send(ctx context.Context, b *backend, in inbound) (inbound, bool) {
	req, _ := in.msg.(*jsonrpc.Request)
	err := b.conn.Write(ctx, in.msg)
	if err == nil {
		// Kept once delivered, so a replay doesn't send it twice.
		if req != nil && req.Method == "notifications/initialized" {
			s.initialized = req
		}
		return inbound{}, false
	}
	call := req != nil && req.IsCall()
	if call {
		s.mu.Lock()
		delete(b.pending, req.ID)
		s.mu.Unlock()
	}
	s.fail(ctx, b, err)
	if unsent(err) && !in.retry {
		return inbound{msg: in.msg, retry: true}, true
	}
	if call {
		s.log.Printf("%s was cut off: %v", summary(in.msg), err)
		s.answer(ctx, req.ID, errInFlight)
	} else {
		s.log.Printf("couldn't deliver %s: %v", summary(in.msg), err)
	}
	return inbound{}, false
}

// refuse answers a call no backend could take with an error.
func (s *switchboard) refuse(ctx context.Context, msg jsonrpc.Message, err error) {
	s.log.Printf("couldn't deliver %s: %v", summary(msg), err)
	if req, ok := msg.(*jsonrpc.Request); ok && req.IsCall() {
		s.answer(ctx, req.ID, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: fmt.Sprintf("SheetDiver couldn't take this request: %v", err)})
	}
}

// ready returns the backend to send to: the current one, unless it went
// away or the app has come back while the library was serving (switched
// between requests, never during one). A new backend is first given the
// client's initialize, unless msg is that initialize.
func (s *switchboard) ready(ctx context.Context, msg *jsonrpc.Request) (*backend, error) {
	s.mu.Lock()
	b := s.cur
	switchable := b != nil && !b.dead && b.library && len(b.pending) == 0 && !s.o.Standalone
	alive := b != nil && !b.dead
	s.mu.Unlock()
	if alive && (!switchable || !s.appRunning(ctx)) {
		return b, nil
	}
	if alive {
		s.log.Printf("the app is running again: switching to it")
		s.retire(b)
	}
	b, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	s.log.Printf("serving from %s", b.name)
	go s.read(ctx, b)
	s.mu.Lock()
	s.cur = b
	s.mu.Unlock()
	if s.init != nil && (msg == nil || msg.Method != "initialize") {
		if err := s.replay(ctx, b); err != nil {
			s.fail(ctx, b, err)
			return nil, err
		}
	}
	return b, nil
}

// appRunning reports whether the app answers on its port.
func (s *switchboard) appRunning(ctx context.Context) bool {
	return mcpbridge.Running(ctx, mcpbridge.BaseURL(s.o.Port))
}

// dial connects to the app if it's running (and wanted), else the library.
func (s *switchboard) dial(ctx context.Context) (*backend, error) {
	b := &backend{gone: make(chan struct{}), pending: map[jsonrpc.ID]bool{}, replays: map[jsonrpc.ID]chan *jsonrpc.Response{}}
	if !s.o.Standalone && s.appRunning(ctx) {
		endpoint := mcpbridge.Endpoint(s.o.Port)
		conn, err := (&mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: appClient}).Connect(ctx)
		if err != nil {
			return nil, err
		}
		b.name, b.conn, b.release = "the app at "+endpoint, conn, func() { _ = conn.Close() }
		return b, nil
	}
	fmt.Fprintln(s.o.Log, "sheetdiver: app not running on", mcpbridge.BaseURL(s.o.Port), "- serving MCP standalone over stdio")
	if s.lib == nil {
		lib, err := openLibrary(s.o.Version)
		if err != nil {
			return nil, err
		}
		s.lib = lib
	}
	conn, release, err := s.lib.connect(ctx)
	if err != nil {
		return nil, err
	}
	b.name, b.library, b.conn, b.release = "the library (the app isn't running)", true, conn, release
	return b, nil
}

// replay gives a new backend the client's initialize (under an id of its
// own, its answer kept from the client) and initialized notification.
func (s *switchboard) replay(ctx context.Context, b *backend) error {
	s.replays++
	id, err := jsonrpc.MakeID(fmt.Sprintf("sheetdiver-replay-%d", s.replays))
	if err != nil {
		return err
	}
	answered := make(chan *jsonrpc.Response, 1)
	s.mu.Lock()
	b.replays[id] = answered
	s.mu.Unlock()
	if err := b.conn.Write(ctx, &jsonrpc.Request{ID: id, Method: s.init.Method, Params: s.init.Params}); err != nil {
		return err
	}
	select {
	case r := <-answered:
		if r.Error != nil {
			return fmt.Errorf("initialize: %w", r.Error)
		}
	case <-b.gone:
		return errors.New("it went away during initialize")
	case <-time.After(replayTimeout):
		return errors.New("it didn't answer initialize")
	case <-ctx.Done():
		return ctx.Err()
	}
	if s.initialized != nil {
		return b.conn.Write(ctx, s.initialized)
	}
	return nil
}

// read passes a backend's messages to the client until it goes away.
func (s *switchboard) read(ctx context.Context, b *backend) {
	for {
		msg, err := b.conn.Read(ctx)
		if err != nil {
			s.fail(ctx, b, err)
			return
		}
		if r, ok := msg.(*jsonrpc.Response); ok {
			s.mu.Lock()
			answered, replayed := b.replays[r.ID]
			delete(b.replays, r.ID)
			delete(b.pending, r.ID)
			s.mu.Unlock()
			if replayed {
				answered <- r
				continue
			}
		}
		if err := s.client.Write(ctx, msg); err != nil {
			s.log.Printf("couldn't pass %s to the client: %v", summary(msg), err)
		}
	}
}

// fail retires a backend that went away, answering the requests it was
// running with an error, so the client can ask again.
func (s *switchboard) fail(ctx context.Context, b *backend, cause error) {
	s.mu.Lock()
	if b.dead {
		s.mu.Unlock()
		return
	}
	b.dead = true
	close(b.gone)
	var ids []jsonrpc.ID
	for id := range b.pending {
		ids = append(ids, id)
	}
	b.pending = map[jsonrpc.ID]bool{}
	if s.cur == b {
		s.cur = nil
	}
	s.mu.Unlock()
	s.log.Printf("%s went away (%v); %d request(s) it was running answered with an error", b.name, cause, len(ids))
	b.release()
	for _, id := range ids {
		s.answer(ctx, id, errInFlight)
	}
}

// retire closes a backend that's no longer wanted (and has nothing
// running).
func (s *switchboard) retire(b *backend) {
	s.mu.Lock()
	if b.dead {
		s.mu.Unlock()
		return
	}
	b.dead = true
	close(b.gone)
	if s.cur == b {
		s.cur = nil
	}
	s.mu.Unlock()
	b.release()
}

// answer sends the client an error for one of its requests.
func (s *switchboard) answer(ctx context.Context, id jsonrpc.ID, e *jsonrpc.Error) {
	if err := s.client.Write(ctx, &jsonrpc.Response{ID: id, Error: e}); err != nil {
		s.log.Printf("couldn't answer id %v: %v", id.Raw(), err)
	}
}

// closeAll closes the current backend and the library, at the end.
func (s *switchboard) closeAll() {
	s.mu.Lock()
	b := s.cur
	s.mu.Unlock()
	if b != nil {
		s.retire(b)
	}
	if s.lib != nil {
		s.lib.close()
	}
}

// libraryServer serves the library in this process, for the sessions of
// a switchboard while the app is closed.
type libraryServer struct {
	svc *core.Service
	srv *mcp.Server
}

// openLibrary opens the library and its MCP server.
func openLibrary(version string) (*libraryServer, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	store, err := config.Open(path)
	if err != nil {
		return nil, err
	}
	svc, err := core.New(store, version)
	if err != nil {
		return nil, err
	}
	return &libraryServer{svc: svc, srv: mcpserver.New(svc).MCP()}, nil
}

// connect starts a session with the library's server, in memory.
func (l *libraryServer) connect(ctx context.Context) (mcp.Connection, func(), error) {
	client, server := mcp.NewInMemoryTransports()
	ss, err := l.srv.Connect(ctx, server, nil)
	if err != nil {
		return nil, nil, err
	}
	conn, err := client.Connect(ctx)
	if err != nil {
		_ = ss.Close()
		return nil, nil, err
	}
	return conn, func() { _ = conn.Close(); _ = ss.Close() }, nil
}

// close closes the library.
func (l *libraryServer) close() {
	_ = l.svc.Close()
}
