//go:build !windows

package mcpbridge

import (
	"os/exec"
	"syscall"
)

// detach starts the app in its own session, so the client stopping the
// bridge doesn't stop the app.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
