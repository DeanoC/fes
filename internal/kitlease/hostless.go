package kitlease

// HostlessOwner is the named kit-local owner used when the host is absent.
// Hostless mode is this owner on the existing lease API, not a FIFO/SSH bypass.
const (
	HostlessOwner   = "kit-hostless"
	HostlessPurpose = "offline-cache-hit-launch"
)

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
