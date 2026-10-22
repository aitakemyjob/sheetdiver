package mcpbridge

import (
	"context"
	"os/exec"
	"time"
)

// Starter starts the app's executable without waiting for it.
type Starter interface {
	Start(path string) error
}

// Detached starts the app as its own process, with no stdio, so it keeps
// running after the client closes the bridge and can't write into the
// protocol stream.
type Detached struct{}

// Start launches path and lets it go.
func (Detached) Start(path string) error {
	cmd := exec.Command(path)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// WaitRunning polls the app at base until it answers or timeout passes.
func WaitRunning(ctx context.Context, base string, timeout, every time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		if Running(ctx, base) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-tick.C:
		}
	}
}
