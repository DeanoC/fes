package hostclient

import "strings"

const artworkHandleLen = 64

// NormalizeHandle returns a canonical artwork handle or empty for malformed
// input. Handles are lowercase 64-character hexadecimal values. Host
// transport, kit disk cache, and UI retain share this rule.
func NormalizeHandle(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if len(value) != artworkHandleLen {
		return ""
	}
	for _, r := range value {
		if r < '0' || r > '9' && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return value
}
