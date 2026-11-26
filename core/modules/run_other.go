//go:build !unix

package modules

import "os/exec"

// isolate falls back to killing the child alone where process groups are not
// available; Kombinat targets POSIX hosts (Windows through WSL).
func isolate(cmd *exec.Cmd) {}
