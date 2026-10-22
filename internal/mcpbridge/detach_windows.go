package mcpbridge

import (
	"os/exec"
	"syscall"
)

// Process creation flags (winbase.h).
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detach starts the app outside the bridge's console and process group,
// so the client stopping the bridge doesn't stop the app.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | detachedProcess}
}
