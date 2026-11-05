// Package buildmeta defines retained desktop release identity records.
// Unlike Go's linker settings, these records survive -trimpath.
package buildmeta

import (
	"encoding/hex"
	"errors"
	"strings"
)

type Identity struct {
	Role    string
	Version string
	Source  string
}

// Parse reads the exact record inspected by the cross-target archive verifier.
func Parse(record string) (Identity, error) {
	fields := strings.Split(record, "|")
	if len(fields) != 5 || fields[0] != "diffx-build-v1" || fields[4] != "end" {
		return Identity{}, errors.New("invalid build identity")
	}
	if fields[1] != "desktop" && fields[1] != "cli" {
		return Identity{}, errors.New("invalid build role")
	}
	if fields[2] == "" || strings.ContainsAny(fields[2], " \t\r\n") {
		return Identity{}, errors.New("invalid build version")
	}
	source, err := hex.DecodeString(fields[3])
	if err != nil || len(source) != 20 {
		return Identity{}, errors.New("invalid build source")
	}
	return Identity{Role: fields[1], Version: fields[2], Source: fields[3]}, nil
}

// Resolve preserves standalone labels. Desktop packages use the retained
// record as their runtime version authority, rather than separate flag values.
func Resolve(record, role, version, source string) Identity {
	if record == "" {
		return Identity{Role: role, Version: version, Source: source}
	}
	identity, err := Parse(record)
	if err != nil || identity.Role != role {
		return Identity{Role: role, Version: "invalid-build"}
	}
	return identity
}
