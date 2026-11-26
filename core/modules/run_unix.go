//go:build unix

package modules

import (
	"os/exec"
	"syscall"
)

// isolate runs the child in its own process group, and a timeout kills the
// whole group, so a module cannot leave orphans behind.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
