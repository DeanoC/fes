package protocol

import "strings"

// CanonicalEditionQuery lowercases, strips punctuation, and collapses spaces
// so "Super Mario Bros." and "super mario bros" share a preference key.
func CanonicalEditionQuery(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	b := make([]rune, 0, len(s))
	lastSpace := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b = append(b, r)
			lastSpace = false
		case r == ' ' || r == '\t':
			if !lastSpace && len(b) > 0 {
				b = append(b, ' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(string(b))
}

// CanonicalEditionPlatform lowercases a room destination platform id.
func CanonicalEditionPlatform(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// EditionKey is the durable household key for one query+platform match set.
func EditionKey(query, platform string) string {
	q := CanonicalEditionQuery(query)
	if q == "" {
		return ""
	}
	return q + "|" + CanonicalEditionPlatform(platform)
}
