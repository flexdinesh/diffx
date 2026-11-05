package app

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
)

// macOS 13.3 ships the required WebKit generation. Runtime acceptance must run
// on a Mac; this host check alone does not establish rendering compatibility.
func checkRuntime() error {
	out, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return fmt.Errorf("read macOS version: %w", err)
	}
	parts := strings.Split(strings.TrimSpace(out), ".")
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return err
	}
	minor := 0
	if len(parts) > 1 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil {
			return err
		}
	}
	if major < 13 || major == 13 && minor < 3 {
		return fmt.Errorf("Diffx Desktop requires macOS 13.3+ for the Safari 16.4 web target")
	}
	return nil
}
