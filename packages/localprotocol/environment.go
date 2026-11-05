package localprotocol

import (
	"slices"
	"strings"
)

// InvocationEnvironment captures native CLI configuration and execution settings.
// These values never change the running webview's own environment.
func InvocationEnvironment(environment []string) []string {
	var result []string
	for _, entry := range environment {
		key, _, ok := strings.Cut(entry, "=")
		if ok && invocationKey(key) {
			result = append(result, entry)
		}
	}
	slices.Sort(result)
	return result
}

func SessionEnvironment(base, invocation []string) []string {
	if invocation == nil {
		return base
	}
	var result []string
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if !invocationKey(key) {
			result = append(result, entry)
		}
	}
	return append(result, InvocationEnvironment(invocation)...)
}

func invocationKey(key string) bool {
	if strings.HasPrefix(key, "GIT_") || strings.HasPrefix(key, "SSH_") {
		return true
	}
	return slices.Contains([]string{"HOME", "PATH", "TMPDIR", "XDG_STATE_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "DIFFX_CONFIG_PATH", "DIFFX_RUNTIME_DIR", "DIFFX_LOCAL_STATE", "DIFFX_LOCAL_PORT", "DIFFX_LOCAL_WEB_DIR", "DIFFX_LOCAL_RETENTION_DAYS", "DIFFX_LOCAL_HOST"}, key)
}
