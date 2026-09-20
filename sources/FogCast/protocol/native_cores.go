package protocol

// Valid distinguishes a complete versioned observation from unknown/malformed
// capability data. An explicitly empty (not null) list is valid.
func (c *NativeCoreAvailability) Valid() bool {
	if c == nil || c.Version != 1 || c.Systems == nil {
		return false
	}
	seen := map[System]bool{}
	for _, system := range c.Systems {
		switch system {
		case SystemMegaDrive, SystemNES, SystemPong, SystemSNES:
		default:
			return false
		}
		if seen[system] {
			return false
		}
		seen[system] = true
	}
	return true
}
