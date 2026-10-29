package review

import (
	"crypto/sha256"
	"encoding/hex"
)

// PatchDigest identifies a file's stored patch text so review marks carry
// between snapshots only while the reviewed change is identical.
func PatchDigest(patch FilePatch) string {
	digest := sha256.Sum256([]byte(patch.Patch))
	return hex.EncodeToString(digest[:])
}
