// Package catalog defines the durable, host-owned game catalog model.
package catalog

import (
	"crypto/sha256"
	"fmt"
	"path"
	"strings"

	"github.com/DeanoC/FogCast/protocol"
)

type Root struct {
	ID     string
	System protocol.System
	Path   string
}

type ScanReport struct {
	Roots []RootReport
}

type SourceKind string

const (
	SourceKindRaw SourceKind = "raw"
	SourceKindZIP SourceKind = "zip"
)

type SourceState string

const (
	SourceStateAvailable SourceState = "available"
	SourceStateInvalid   SourceState = "invalid"
	SourceStateMissing   SourceState = "missing"
)

type Fingerprint struct {
	SourceSize    int64
	ModifiedNS    int64
	ZIPMember     string
	ZIPSize       int64
	ZIPCRC32      uint32
	ZIPEntryCount int
}

type Content struct {
	SHA256    string
	Size      int64
	Extension string
}

type Game struct {
	ID, Title, LibraryID, RelativePath, Reason            string
	CanonicalTitle, Region, Revision, DumpFlags, GroupKey string
	Genre, Year, SearchAliases                            string
	System                                                protocol.System
	Kind                                                  SourceKind
	State                                                 SourceState
	RootOnline                                            bool
	Fingerprint                                           Fingerprint
	Content                                               *Content
	FirstSeenNS                                           int64
	VariantCount                                          int
}

// NormalizeRelativePath returns a clean, slash-separated path that remains
// relative to its catalog root.
func NormalizeRelativePath(relativePath string) (string, error) {
	normalized := strings.ReplaceAll(relativePath, `\`, "/")
	if normalized == "" || path.IsAbs(normalized) {
		return "", fmt.Errorf("catalog relative path %q must be non-empty and relative", relativePath)
	}
	normalized = path.Clean(normalized)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("catalog relative path %q escapes its root", relativePath)
	}
	return normalized, nil
}

// GameID returns the stable game ID for an already-normalized slash-relative
// path. Call NormalizeRelativePath before using untrusted source paths.
func GameID(system protocol.System, libraryID, relativePath, title string) string {
	identity := string(system) + "\x00" + libraryID + "\x00" + relativePath
	digest := sha256.Sum256([]byte(identity))
	return string(system) + "-" + titleSlug(title) + "-" + fmt.Sprintf("%x", digest[:6])
}

func titleSlug(title string) string {
	var builder strings.Builder
	pendingHyphen := false
	for _, character := range title {
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			if pendingHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(character)
			pendingHyphen = false
			continue
		}
		pendingHyphen = builder.Len() > 0
	}
	slug := builder.String()
	if len(slug) > 48 {
		slug = slug[:48]
	}
	if slug == "" {
		return "game"
	}
	return slug
}
