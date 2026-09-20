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
)

type Status struct {
	ExpiresInMS int64     `json:"expires_in_ms"`
	State       string    `json:"state"`
	Generation  string    `json:"generation"`
	Owner       string    `json:"owner,omitempty"`
	Purpose     string    `json:"purpose,omitempty"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
	Reason      string    `json:"reason,omitempty"`
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

// ForeignSession reports a held grant that hostless launch must not displace.
func ForeignSession(s Status) bool {
	if s.State != "held" && s.State != "revoking" && s.State != "blocked" {
		return false
	}
	return s.Owner != "" && !HostlessSession(s)
}
