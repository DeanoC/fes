package catalog

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode"
)

const maxMediaSetBytes = 1 << 20

// ErrEscapingMediaReference is returned when a cue/gdi names a file outside its tree.
var ErrEscapingMediaReference = errors.New("media set reference is outside the catalog root")

// ErrUnvalidatedMediaSheet is returned when a cue/gdi cannot be read in full.
var ErrUnvalidatedMediaSheet = errors.New("media set sheet cannot be fully validated")

// ReferencedMediaNames returns same-tree companion paths declared by a cue or
// gdi sheet. Absolute, drive-qualified, and parent-escaping names are omitted
// so a hostile FILE line cannot suppress an unrelated basename. Incomplete
// sheets contribute no skip names.
func ReferencedMediaNames(filename string, body io.Reader) []string {
	names, _, err := parseReferencedMedia(filename, body)
	if err != nil {
		return nil
	}
	return names
}

// ParseReferencedMedia returns confined companion paths. It fails if the sheet
// cannot be fully read or names any absolute or parent-escaping file.
func ParseReferencedMedia(filename string, body io.Reader) ([]string, error) {
	names, invalid, err := parseReferencedMedia(filename, body)
	if err != nil {
		return nil, err
	}
	if invalid {
		return nil, ErrEscapingMediaReference
	}
	return names, nil
}

func parseReferencedMedia(filename string, body io.Reader) ([]string, bool, error) {
	extension := strings.ToLower(path.Ext(filename))
	switch extension {
	case ".cue", ".gdi":
	default:
		return nil, false, nil
	}
	data, err := io.ReadAll(io.LimitReader(body, maxMediaSetBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > maxMediaSetBytes {
		return nil, false, ErrUnvalidatedMediaSheet
	}
	var raw []string
	switch extension {
	case ".cue":
		raw, err = parseCueNames(bytes.NewReader(data))
	case ".gdi":
		raw, err = parseGDINames(bytes.NewReader(data))
	}
	if err != nil {
		return nil, false, err
	}
	names := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	invalid := false
	for _, item := range raw {
		name, ok := confinedMediaName(item)
		if !ok {
			invalid = true
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, invalid, nil
}

func confinedMediaName(raw string) (string, bool) {
	cleaned := strings.ReplaceAll(strings.TrimSpace(raw), `\`, "/")
	if cleaned == "" || path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "/") {
		return "", false
	}
	if len(cleaned) > 1 && cleaned[1] == ':' {
		return "", false
	}
	normalized := path.Clean(cleaned)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", false
	}
	return normalized, true
}

func parseCueNames(body io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMediaSetBytes)
	names := make([]string, 0)
	seen := map[string]struct{}{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(strings.ToUpper(line), "FILE") {
			continue
		}
		name := cueFileName(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if err := scanner.Err(); err != nil {
		return nil, ErrUnvalidatedMediaSheet
	}
	return names, nil
}

func cueFileName(line string) string {
	rest := strings.TrimSpace(line[4:])
	if rest == "" {
		return ""
	}
	if rest[0] == '"' {
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return ""
		}
		return strings.ReplaceAll(rest[1:1+end], `\`, "/")
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return ""
	}
	return strings.ReplaceAll(fields[0], `\`, "/")
}

func parseGDINames(body io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxMediaSetBytes)
	names := make([]string, 0)
	seen := map[string]struct{}{}
	first := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if _, err := strconv.Atoi(line); err == nil {
				continue
			}
		}
		name := gdiFileName(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if err := scanner.Err(); err != nil {
		return nil, ErrUnvalidatedMediaSheet
	}
	return names, nil
}

func gdiFileName(line string) string {
	if i := strings.IndexByte(line, '"'); i >= 0 {
		rest := line[i+1:]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			return ""
		}
		return strings.ReplaceAll(rest[:end], `\`, "/")
	}
	fields := strings.FieldsFunc(line, func(r rune) bool { return unicode.IsSpace(r) })
	if len(fields) < 5 {
		return ""
	}
	return strings.ReplaceAll(fields[4], `\`, "/")
}
