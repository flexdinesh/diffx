package review

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"path"
	"strings"
)

// RemoteRepositoryKey is the initial repository identity enrolled for a
// sanitized remote URL. Collectors and the server's GitHub integration share
// it, so pull requests group with checkouts cloned from the same URL.
func RemoteRepositoryKey(remote string) string {
	digest := sha256.Sum256([]byte("remote\x00" + remote))
	return hex.EncodeToString(digest[:])
}

func RemoteRepositoryName(remote, fallback string) string {
	if remote == "" {
		return fallback
	}
	if parsed, err := url.Parse(remote); err == nil && parsed.Scheme != "" && (parsed.Host != "" || parsed.Scheme == "file") {
		remote = parsed.Path
	} else if _, suffix, ok := strings.Cut(remote, ":"); ok {
		remote = suffix
	}
	name := strings.TrimSuffix(path.Base(strings.TrimRight(remote, "/")), ".git")
	if name == "" || name == "." || name == "/" {
		return fallback
	}
	return name
}
