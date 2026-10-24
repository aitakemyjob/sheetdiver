package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// answer plays the app: it answers each picture request on the bus with
// what reply makes of it.
func answer(t *testing.T, s *Service, reply func(SnapshotRequest) Snapshot) {
	t.Helper()
	events, cancel := s.Bus.Subscribe()
	t.Cleanup(cancel)
	go func() {
		for ev := range events {
			if req, ok := ev.Data.(SnapshotRequest); ok && ev.Type == EvSnapshot {
				_ = s.DeliverSnapshot(reply(req))
			}
		}
	}()
}

func TestDiagramSnapshot(t *testing.T) {
	s := newService(t)
	answer(t, s, func(req SnapshotRequest) Snapshot {
		if req.Group == "Nowhere" {
			return Snapshot{ID: req.ID, Problem: "no group Nowhere; the groups are Power, MCU"}
		}
		return Snapshot{ID: req.ID, PNG: []byte("\x89PNG " + req.Project + "/" + req.Group), Width: 400, Height: 300}
	})
	got, err := s.DiagramSnapshot(context.Background(), "board", "MCU")
	if err != nil || string(got.PNG) != "\x89PNG board/MCU" || got.Width != 400 {
		t.Fatalf("snapshot = %+v, %v", got, err)
	}
	if _, err := s.DiagramSnapshot(context.Background(), "board", "Nowhere"); err == nil || !strings.Contains(err.Error(), "no group Nowhere") {
		t.Errorf("a problem the app reports: %v", err)
	}
}

func TestDiagramSnapshotUnanswered(t *testing.T) {
	s := newService(t)
	was := snapshotWait
	snapshotWait = 50 * time.Millisecond
	t.Cleanup(func() { snapshotWait = was })
	if _, err := s.DiagramSnapshot(context.Background(), "board", ""); !errors.Is(err, ErrNoSnapshot) {
		t.Errorf("no app: %v", err)
	}
	// An answer nothing waits for is refused, and nothing is left waiting.
	if err := s.DeliverSnapshot(Snapshot{ID: "late", PNG: []byte("x")}); err == nil {
		t.Error("a late answer was accepted")
	}
	if n := len(s.session.snapshots); n != 0 {
		t.Errorf("%d requests left waiting", n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.DiagramSnapshot(ctx, "board", ""); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}
