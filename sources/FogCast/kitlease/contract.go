// Package kitlease defines the public wire contract for target-kit ownership.
// It contains no manager, cleanup, timer, token, or target implementation.
package kitlease

import (
	"strings"
	"time"
)

const hostHIDOwnerPrefix = "fogcast@"

// HostlessOwner is the named kit-local owner used when the host is absent.
// Hostless mode is this owner on the existing lease API, not a FIFO/SSH bypass.
const (
	HostlessOwner   = "kit-hostless"
	HostlessPurpose = "offline-cache-hit-launch"
	// LocalCorePurpose is a kit-local installed-core session. The host
	// still observes this grant as foreign. It is not a takeover.
	LocalCorePurpose = "kit-local-core"
)

type Status struct {
	TargetReachable bool      `json:"target_reachable,omitempty"`
	TargetReady     bool      `json:"target_ready,omitempty"`
	ExpiresInMS     int64     `json:"expires_in_ms"`
	State           string    `json:"state"`
	Generation      string    `json:"generation"`
	Owner           string    `json:"owner,omitempty"`
	Purpose         string    `json:"purpose,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
	Reason          string    `json:"reason,omitempty"`
}

type ClaimRequest struct {
	RequestID string `json:"request_id"`
	Owner     string `json:"owner"`
	Purpose   string `json:"purpose"`
}

type TakeoverRequest struct {
	ClaimRequest
	ExpectedGeneration string `json:"expected_generation"`
	Reason             string `json:"reason"`
}

type Grant struct {
	Status Status `json:"status"`
	Token  string `json:"token"`
}

// ForeignHID reports a kit grant this FogCast host or launcher session must
// not drive. Busy (held by another session), blocked, recovery-required,
// hostless, and any non-host owner fail closed. Free or unlabeled grants
// are not observed as foreign.
func ForeignHID(s Status) bool {
	switch s.State {
	case "busy", "blocked", "recovery-required":
		return true
	case "held", "revoking":
		if s.Owner == "" {
			return false
		}
		return !strings.HasPrefix(s.Owner, hostHIDOwnerPrefix)
	default:
		return false
	}
}

// HostlessSession reports whether the current grant is the offline cache-hit owner.
func HostlessSession(s Status) bool {
	return s.State == "held" && s.Owner == HostlessOwner && s.Purpose == HostlessPurpose
}

// LocalCoreSession reports whether the current grant is the kit-local
// installed-core owner. A host session must treat it as in use.
func LocalCoreSession(s Status) bool {
	return s.State == "held" && s.Owner == HostlessOwner && s.Purpose == LocalCorePurpose
}

// HostlessAllows is the hostless mutation allowlist. The offline cache-hit
// purpose may stop and deliver input. The kit-local core purpose may use
// only the local-control routes. Any other purpose is denied.
func HostlessAllows(s Status, path string) bool {
	if s.Owner != HostlessOwner || s.State != "held" {
		return false
	}
	switch s.Purpose {
	case HostlessPurpose:
		switch path {
		case "/v1/stop", "/v1/input/attach", "/v1/input/detach", "/v1/input/stream":
			return true
		}
	case LocalCorePurpose:
		switch path {
		case "/v1/local/cores", "/v1/local/stop":
			return true
		}
		return localCoreLaunchPath(path)
	}
	return false
}

func localCoreLaunchPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/v1/local/cores/")
	if !ok {
		return false
	}
	id, ok := strings.CutSuffix(rest, "/launch")
	if !ok || len(id) != 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ForeignSession reports a held grant that hostless launch must not displace.
func ForeignSession(s Status) bool {
	if s.State != "held" && s.State != "revoking" && s.State != "blocked" {
		return false
	}
	return s.Owner != "" && !HostlessSession(s)
}
