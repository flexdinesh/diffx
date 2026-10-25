//go:build !windows

package fetcher

import (
	"os/exec"
	"syscall"
	"time"
)

// isolate gives each Git invocation its own process group so cancellation
// also stops remote helpers and index-pack.
func isolate(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = time.Second
	command.Cancel = func() error { return syscall.Kill(-command.Process.Pid, syscall.SIGKILL) }
}

// reap stops helpers that outlive their leader, before the mirror is reused or evicted.
func reap(command *exec.Cmd) {
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}
