// Package localprotocol defines the private, versioned desktop sidecar wire
// contract. Scope identities are opaque and are resolved only by the server.
package localprotocol

import "github.com/flexdinesh/diffx/packages/connection"

const (
	Version        = 1
	Command        = "__desktop-session"
	ConnectCommand = "__connect-session"
)

type DesktopContract struct {
	ProtocolVersion int    `json:"protocolVersion"`
	Build           string `json:"build"`
	Sidecar         string `json:"sidecar"`
}

// ConnectRequest is read from --intent-fd by ConnectCommand. Args excludes the
// public "connect" prefix. Invitation secrets never become child argv.
type ConnectRequest struct {
	Args []string `json:"args"`
}

type Input struct {
	Kind      string   `json:"kind"`
	Selection string   `json:"selection,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	ContextID string   `json:"contextId,omitempty"`
}

// Session is advertised by local instances, including foreground web launches.
type Session struct {
	ProtocolVersion int    `json:"protocolVersion"`
	ScopeID         string `json:"scopeId"`
	Input           Input  `json:"input"`
}

// Readiness is the single stdout JSON record from Command. Owned determines
// whether ending the sidecar lifetime also stops the advertised server.
type Readiness struct {
	Connection      connection.Target `json:"connection"`
	Label           string            `json:"label,omitempty"`
	ProtocolVersion int               `json:"protocolVersion"`
	Socket          string            `json:"socket"`
	InstanceID      string            `json:"instanceId"`
	ViewerURL       string            `json:"viewerUrl,omitempty"`
	Owned           bool              `json:"owned"`
	ScopeID         string            `json:"scopeId"`
	Input           Input             `json:"input"`
}
