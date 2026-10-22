package mcpbridge

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Bridge runs one client session: it checks for the app, starts it when
// allowed, then relays to it, or answers with why it can't.
type Bridge struct {
	// Port is the app's MCP port.
	Port int
	// Local is the client's side, normally &mcp.StdioTransport{}.
	Local mcp.Transport
	// App finds the app's executable when it isn't running.
	App Locator
	// Starter starts the app; nil leaves a closed app closed.
	Starter Starter
	// StartTimeout is how long a started app has to answer (default 30s).
	StartTimeout time.Duration
	// Poll is how often a starting app is checked (default 250ms).
	Poll time.Duration
	// Log receives diagnostics (default slog.Default, which writes to
	// stderr; stdout carries only protocol messages).
	Log *slog.Logger
}

// State is where a session is.
type State int

// A session goes Probing → Relaying when the app is up, or
// Probing → Starting → Relaying | Refusing when it isn't, and ends Done.
const (
	Probing State = iota
	Starting
	Relaying
	Refusing
	Done
)

// String names the state for logs.
func (s State) String() string {
	return [...]string{"probing", "starting", "relaying", "refusing", "done"}[s]
}

// session is one run's mutable state.
type session struct {
	*Bridge
	// reason is why the app can't be reached, for Refusing.
	reason string
}

// transition handles one state and returns the next.
type transition func(ctx context.Context, s *session) (State, error)

// transitions is the state machine.
var transitions = map[State]transition{
	Probing:  probe,
	Starting: start,
	Relaying: relay,
	Refusing: refuse,
}

// Run drives the session until the client disconnects.
func (b *Bridge) Run(ctx context.Context) error {
	s := &session{Bridge: b}
	if s.Log == nil {
		s.Log = slog.Default()
	}
	for st := Probing; st != Done; {
		next, err := transitions[st](ctx, s)
		if err != nil {
			return err
		}
		s.Log.Debug("mcp bridge", "from", st, "to", next)
		st = next
	}
	return nil
}

// probe relays straight away when the app is up.
func probe(ctx context.Context, s *session) (State, error) {
	if Running(ctx, BaseURL(s.Port)) {
		return Relaying, nil
	}
	if s.Starter == nil {
		s.reason = fmt.Sprintf("SheetDiver isn't running (nothing answers on 127.0.0.1:%d). Open the SheetDiver app, then reconnect.", s.Port)
		return Refusing, nil
	}
	return Starting, nil
}

// start finds and starts the app, then waits for it to answer.
func start(ctx context.Context, s *session) (State, error) {
	var path string
	ok := false
	if s.App != nil {
		path, ok = s.App.Locate()
	}
	if !ok {
		s.reason = "SheetDiver isn't running and the app wasn't found on this computer. Open the SheetDiver app, then reconnect."
		return Refusing, nil
	}
	s.Log.Info("starting SheetDiver", "path", path)
	if err := s.Starter.Start(path); err != nil {
		s.reason = fmt.Sprintf("SheetDiver isn't running and couldn't be started (%s: %v). Open the SheetDiver app, then reconnect.", path, err)
		return Refusing, nil
	}
	if WaitRunning(ctx, BaseURL(s.Port), orDefault(s.StartTimeout, 30*time.Second), orDefault(s.Poll, 250*time.Millisecond)) {
		return Relaying, nil
	}
	s.reason = fmt.Sprintf("SheetDiver was started but doesn't answer on 127.0.0.1:%d. Check the MCP port in SheetDiver's Settings, then reconnect.", s.Port)
	return Refusing, nil
}

// relay passes messages to the app until the client disconnects.
func relay(ctx context.Context, s *session) (State, error) {
	return Done, Relay(ctx, s.Local, Endpoint(s.Port))
}

// refuse answers every request with the reason until the client
// disconnects.
func refuse(ctx context.Context, s *session) (State, error) {
	s.Log.Warn("sheetdiver: " + s.reason)
	return Done, Refuse(ctx, s.Local, s.reason)
}

// orDefault returns d when v is unset.
func orDefault(v, d time.Duration) time.Duration {
	if v <= 0 {
		return d
	}
	return v
}
