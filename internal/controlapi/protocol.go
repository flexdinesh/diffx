// Package controlapi implements the private CLI-to-local-server API. It has no
// credentials of its own: composition mounts it only on the owner-only local
// socket, whose filesystem permissions and peer user check are its authority.
package controlapi

import "github.com/flexdinesh/diffx/internal/config"

// ProtocolVersion 6 moved control to the local socket and replaced the
// status URLs with the socket path and optional viewer URL.
const ProtocolVersion = 6

type Settings = config.ServerSettings

type Status struct {
	State           string `json:"state"`
	InstanceID      string `json:"instanceId"`
	StateID         string `json:"stateId"`
	PID             int    `json:"pid"`
	Version         string `json:"version"`
	ProtocolVersion int    `json:"protocolVersion"`
	// Socket is the owner-only API, MCP and control endpoint.
	Socket string `json:"socket"`
	// ViewerURL is the loopback browser endpoint; empty when headless.
	ViewerURL    string   `json:"viewerUrl"`
	Settings     Settings `json:"settings"`
	Observations int      `json:"observations"`
}

// Problem is a local server rejection, distinct from an interrupted transport.
type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Code   string `json:"code,omitempty"`
	Detail string `json:"detail"`
}

func (problem *Problem) Error() string { return problem.Detail }
