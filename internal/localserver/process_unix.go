//go:build !windows

package localserver

import (
	"os/exec"
	"syscall"
)

func detach(command *exec.Cmd) { command.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
