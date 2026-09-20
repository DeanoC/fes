package metadata

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const NormalizerVersion = 1

var foldCase = cases.Fold()

// NormalizeTitle applies the intentionally conservative title normalizer used
// for provider matching. It does not discard punctuation, reorder words, or
// transliterate characters.
func NormalizeTitle(title string) (string, error) {
	if count := len([]rune(title)); count < 1 || count > 200 {
		return "", fmt.Errorf("title length must be between 1 and 200 Unicode code points")
	}
	normalized := normalizeTitleText(title)
	if normalized == "" {
		return "", fmt.Errorf("title must contain non-whitespace text")
	}
	return normalized, nil
}

func normalizeTitleText(title string) string {
	text := norm.NFC.String(title)
	text = foldCase.String(text)
	text = norm.NFC.String(text)
	var builder strings.Builder
	space := false
	for _, r := range text {
		if unicode.IsSpace(r) {
			space = builder.Len() != 0
			continue
		}
		if space {
			builder.WriteByte(' ')
			space = false
		}
		builder.WriteRune(r)
	}
	return strings.TrimSpace(builder.String())
}

// DecoratedTitle returns the secondary matching form. Only terminal balanced
// region/revision decorations explicitly approved by the architecture are
// removed, and the operation is repeated to support e.g. "Title (USA) [Rev 2]".
func DecoratedTitle(title string) (string, error) {
	normalized, err := NormalizeTitle(title)
	if err != nil {
		return "", err
	}
	for {
		trimmed, removed := removeApprovedSuffix(normalized)
		if !removed {
			return trimmed, nil
		}
		normalized = trimmed
	}
}

func removeApprovedSuffix(title string) (string, bool) {
	title = strings.TrimSpace(title)
	if len(title) < 3 {
		return title, false
	}
	close := title[len(title)-1]
	open := byte(0)
	switch close {
	case ')':
		open = '('
	case ']':
		open = '['
	default:
		return title, false
	}
	depth := 0
	openIndex := -1
	for index := len(title) - 1; index >= 0; index-- {
		switch title[index] {
		case close:
			depth++
		case open:
			depth--
			if depth == 0 {
				openIndex = index
				index = -1
			}
		}
	}
	if openIndex <= 0 {
		return title, false
	}
	inside := strings.TrimSpace(title[openIndex+1 : len(title)-1])
	if !approvedDecoration(inside) {
		return title, false
	}
	return strings.TrimSpace(title[:openIndex]), true
}

func approvedDecoration(value string) bool {
	value = foldCase.String(norm.NFC.String(value))
	switch value {
	case "usa", "europe", "japan", "world":
		return true
	}
	if !strings.HasPrefix(value, "rev ") || len(value) != len("rev ")+1 {
		return false
	}
	character := value[len("rev ")]
	return (character >= 'a' && character <= 'z') ||
		(character >= '0' && character <= '9')
}
