//go:build desktopautomation && !mygo_noinspector

package main

// The plugin reads MYGO_AUTOMATION during its package initialization. The
// acceptance launcher supplies it before starting this test-only executable.
import _ "github.com/egoist/mygo/plugins/automation"
