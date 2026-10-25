//go:build windows

package fetcher

import (
	"os/exec"
	"time"
)

func isolate(command *exec.Cmd) { command.WaitDelay = time.Second }

func reap(*exec.Cmd) {}
