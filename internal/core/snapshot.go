package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// SnapshotRequest asks the app for a picture of a project's block
// diagram as it draws it: all of it, or one group's frame.
type SnapshotRequest struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Group   string `json:"group,omitempty"`
}

// Snapshot is the app's answer: the picture as PNG, or why it couldn't
// make one (no such group).
type Snapshot struct {
	ID      string `json:"id"`
	PNG     []byte `json:"png,omitempty"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Problem string `json:"problem,omitempty"`
}

// snapshotWait is how long the app has to answer.
var snapshotWait = 8 * time.Second

// ErrNoSnapshot is returned when no app answered a picture request.
var ErrNoSnapshot = errors.New("SheetDiver didn't answer: open the project's Block diagram in the app (set_connections with show: true) and try again")

// DiagramSnapshot asks the app showing a project's block diagram for a
// picture of it (of one group, if named) and waits for the answer.
func (s *Service) DiagramSnapshot(ctx context.Context, project, group string) (Snapshot, error) {
	id, err := snapshotID()
	if err != nil {
		return Snapshot{}, err
	}
	ch := make(chan Snapshot, 1)
	ss := &s.session
	ss.mu.Lock()
	if ss.snapshots == nil {
		ss.snapshots = map[string]chan Snapshot{}
	}
	ss.snapshots[id] = ch
	ss.mu.Unlock()
	defer func() {
		ss.mu.Lock()
		delete(ss.snapshots, id)
		ss.mu.Unlock()
	}()
	s.emit(EvSnapshot, SnapshotRequest{ID: id, Project: project, Group: group})
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-time.After(snapshotWait):
		return Snapshot{}, ErrNoSnapshot
	case got := <-ch:
		if got.Problem != "" {
			return Snapshot{}, fmt.Errorf("the app couldn't draw it: %s", got.Problem)
		}
		if len(got.PNG) == 0 {
			return Snapshot{}, errors.New("the app sent an empty picture")
		}
		return got, nil
	}
}

// DeliverSnapshot hands the app's answer to the request waiting for it;
// an answer nothing waits for (another window answered first, or it came
// too late) is refused.
func (s *Service) DeliverSnapshot(sn Snapshot) error {
	s.session.mu.Lock()
	ch, ok := s.session.snapshots[sn.ID]
	delete(s.session.snapshots, sn.ID)
	s.session.mu.Unlock()
	if !ok {
		return fmt.Errorf("no picture %q is waiting: it was answered already or came too late", sn.ID)
	}
	ch <- sn
	return nil
}

// snapshotID is a random request id.
func snapshotID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
