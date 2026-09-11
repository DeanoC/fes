package kitlease

import "strings"

const hostHIDOwnerPrefix = "fogcast@"

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
