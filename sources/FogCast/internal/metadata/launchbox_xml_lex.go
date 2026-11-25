package metadata

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

func validateXMLNameFrame(name []byte) error {
	if len(name) == 0 || len(name) > launchBoxXMLMaxNameBytes || !utf8.Valid(name) || utf8.RuneCount(name) > launchBoxXMLMaxNameRunes {
		return errors.New("launchbox XML name exceeds bound")
	}
	return nil
}

func validateXMLNameToken(name []byte) error {
	if err := validateXMLNameFrame(name); err != nil {
		return err
	}
	end, err := scanXMLName(name)
	if err != nil || end != len(name) {
		return errors.New("launchbox XML name is malformed")
	}
	colonCount := bytes.Count(name, []byte{':'})
	if colonCount > 1 || (colonCount == 1 && (name[0] == ':' || name[len(name)-1] == ':')) {
		return errors.New("launchbox XML qualified name is malformed")
	}
	return nil
}

func scanXMLName(value []byte) (int, error) {
	if len(value) == 0 || !utf8.Valid(value) {
		return 0, errors.New("launchbox XML name is malformed")
	}
	position := 0
	first := true
	for position < len(value) {
		runeValue, size := utf8.DecodeRune(value[position:])
		if runeValue == utf8.RuneError && size == 1 {
			return 0, errors.New("launchbox XML name is malformed")
		}
		if first {
			if !xmlNameStartRune(runeValue) {
				return 0, errors.New("launchbox XML name is malformed")
			}
			first = false
		} else if !xmlNameCharRune(runeValue) {
			break
		}
		position += size
	}
	if first {
		return 0, errors.New("launchbox XML name is empty")
	}
	return position, nil
}

func xmlNameStartRune(value rune) bool {
	return value == ':' || value == '_' ||
		(value >= 'A' && value <= 'Z') || (value >= 'a' && value <= 'z') ||
		(value >= 0xC0 && value <= 0xD6) || (value >= 0xD8 && value <= 0xF6) ||
		(value >= 0xF8 && value <= 0x2FF) || (value >= 0x370 && value <= 0x37D) ||
		(value >= 0x37F && value <= 0x1FFF) || (value >= 0x200C && value <= 0x200D) ||
		(value >= 0x2070 && value <= 0x218F) || (value >= 0x2C00 && value <= 0x2FEF) ||
		(value >= 0x3001 && value <= 0xD7FF) || (value >= 0xF900 && value <= 0xFDCF) ||
		(value >= 0xFDF0 && value <= 0xFFFD) || (value >= 0x10000 && value <= 0xEFFFF)
}

func xmlNameCharRune(value rune) bool {
	return xmlNameStartRune(value) || value == '-' || value == '.' ||
		(value >= '0' && value <= '9') || value == 0xB7 ||
		(value >= 0x300 && value <= 0x36F) || (value >= 0x203F && value <= 0x2040)
}

func validateXMLDeclaration(frame []byte) error {
	if len(frame) > launchBoxXMLMaxDeclarationBytes || !bytes.HasPrefix(frame, []byte("<?xml")) || !bytes.HasSuffix(frame, []byte("?>")) {
		return errors.New("launchbox XML declaration is invalid")
	}
	body := frame[len("<?xml") : len(frame)-2]
	if len(body) == 0 || !xmlSpace(body[0]) {
		return errors.New("launchbox XML declaration has no separating whitespace")
	}
	body = trimXMLSpaceBytes(body)

	position := 0
	seenVersion, seenEncoding, seenStandalone := false, false, false
	order := 0
	for position < len(body) {
		for position < len(body) && xmlSpace(body[position]) {
			position++
		}
		if position == len(body) {
			break
		}
		start := position
		for position < len(body) && body[position] >= 'a' && body[position] <= 'z' {
			position++
		}
		if start == position {
			return errors.New("launchbox XML declaration pseudo-attribute is malformed")
		}
		name := string(body[start:position])
		for position < len(body) && xmlSpace(body[position]) {
			position++
		}
		if position >= len(body) || body[position] != '=' {
			return errors.New("launchbox XML declaration pseudo-attribute has no equals sign")
		}
		position++
		for position < len(body) && xmlSpace(body[position]) {
			position++
		}
		if position >= len(body) || (body[position] != '\'' && body[position] != '"') {
			return errors.New("launchbox XML declaration value is not quoted")
		}
		quote := body[position]
		position++
		valueStart := position
		for position < len(body) && body[position] != quote {
			position++
		}
		if position >= len(body) {
			return errors.New("launchbox XML declaration value is unterminated")
		}
		value := string(body[valueStart:position])
		position++
		if position < len(body) && !xmlSpace(body[position]) {
			return errors.New("launchbox XML declaration pseudo-attributes lack separating whitespace")
		}

		switch name {
		case "version":
			if seenVersion || order != 0 || value != "1.0" {
				return errors.New("launchbox XML version policy rejected")
			}
			seenVersion = true
			order = 1
		case "encoding":
			if !seenVersion || seenEncoding || order > 2 || !asciiEqualFold(value, "utf-8") {
				return errors.New("launchbox XML encoding policy rejected")
			}
			seenEncoding = true
			order = 2
		case "standalone":
			if !seenVersion || seenStandalone || order > 2 || value != "yes" {
				return errors.New("launchbox XML standalone policy rejected")
			}
			seenStandalone = true
			order = 3
		default:
			return errors.New("launchbox XML declaration pseudo-attribute is unsupported")
		}
	}
	if !seenVersion {
		return errors.New("launchbox XML declaration has no version")
	}
	return nil
}

func validateXMLReferences(value []byte) error {
	for position := 0; position < len(value); position++ {
		if value[position] != '&' {
			continue
		}
		relativeEnd := bytes.IndexByte(value[position+1:], ';')
		if relativeEnd < 0 {
			return errors.New("launchbox XML entity reference is unterminated")
		}
		length := relativeEnd + 2
		if length > launchBoxXMLMaxEntityBytes {
			return errors.New("launchbox XML entity reference exceeds bound")
		}
		if _, err := decodeXMLReference(value[position : position+length]); err != nil {
			return err
		}
		position += length - 1
	}
	return nil
}

func decodeXMLReferences(value []byte) (string, error) {
	if err := validateXMLReferences(value); err != nil {
		return "", err
	}
	var output strings.Builder
	output.Grow(len(value))
	for position := 0; position < len(value); {
		if value[position] != '&' {
			runeValue, size := utf8.DecodeRune(value[position:])
			if runeValue == utf8.RuneError && size == 1 {
				return "", errors.New("launchbox XML reference value is not UTF-8")
			}
			output.WriteRune(runeValue)
			position += size
			continue
		}
		relativeEnd := bytes.IndexByte(value[position+1:], ';')
		runeValue, err := decodeXMLReference(value[position : position+relativeEnd+2])
		if err != nil {
			return "", err
		}
		output.WriteRune(runeValue)
		position += relativeEnd + 2
	}
	return output.String(), nil
}

func decodeXMLReference(reference []byte) (rune, error) {
	if len(reference) < 3 || reference[0] != '&' || reference[len(reference)-1] != ';' {
		return 0, errors.New("launchbox XML entity reference is malformed")
	}
	switch string(reference) {
	case "&amp;":
		return '&', nil
	case "&apos;":
		return '\'', nil
	case "&gt;":
		return '>', nil
	case "&lt;":
		return '<', nil
	case "&quot;":
		return '"', nil
	}
	body := string(reference[1 : len(reference)-1])
	if len(body) < 2 || body[0] != '#' {
		return 0, errors.New("launchbox XML custom entity is not allowed")
	}
	base := 10
	digits := body[1:]
	if digits[0] == 'x' || digits[0] == 'X' {
		base = 16
		digits = digits[1:]
	}
	if digits == "" {
		return 0, errors.New("launchbox XML numeric entity is empty")
	}
	value, err := strconv.ParseUint(digits, base, 32)
	if err != nil || !xmlRuneValid(rune(value)) {
		return 0, errors.New("launchbox XML numeric entity is invalid")
	}
	return rune(value), nil
}

func xmlRuneValid(value rune) bool {
	return value == 0x9 || value == 0xA || value == 0xD ||
		(value >= 0x20 && value <= 0xD7FF) ||
		(value >= 0xE000 && value <= 0xFFFD) ||
		(value >= 0x10000 && value <= 0x10FFFF)
}

func xmlCharactersValid(value []byte) bool {
	for len(value) > 0 {
		runeValue, size := utf8.DecodeRune(value)
		if (runeValue == utf8.RuneError && size == 1) || !xmlRuneValid(runeValue) {
			return false
		}
		value = value[size:]
	}
	return true
}

func xmlSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func xmlSpaceRune(value rune) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func trimXMLSpaceBytes(value []byte) []byte {
	start, end := 0, len(value)
	for start < end && xmlSpace(value[start]) {
		start++
	}
	for end > start && xmlSpace(value[end-1]) {
		end--
	}
	return value[start:end]
}

func asciiEqualFold(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		l, r := left[index], right[index]
		if l >= 'A' && l <= 'Z' {
			l += 'a' - 'A'
		}
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if l != r {
			return false
		}
	}
	return true
}

type launchBoxTextBuilder struct {
	bytes    []byte
	maxBytes int
	maxRunes int
	runes    int
}

func (b *launchBoxTextBuilder) Append(value []byte) error {
	if !utf8.Valid(value) || !xmlCharactersValid(value) {
		return errors.New("launchbox XML selected value is not valid XML text")
	}
	if len(value) > b.maxBytes-len(b.bytes) {
		return errors.New("launchbox XML selected value exceeds byte bound")
	}
	runeCount := utf8.RuneCount(value)
	if runeCount > b.maxRunes-b.runes {
		return errors.New("launchbox XML selected value exceeds rune bound")
	}
	b.bytes = append(b.bytes, value...)
	b.runes += runeCount
	return nil
}

func (b *launchBoxTextBuilder) String() string { return string(b.bytes) }

func xmlWhitespaceOnly(value []byte) bool {
	for _, character := range value {
		if !xmlSpace(character) {
			return false
		}
	}
	return true
}

func xmlBoundaryTrim(value string) string {
	start, end := 0, len(value)
	for start < end {
		runeValue, size := utf8.DecodeRuneInString(value[start:])
		if !xmlSpaceRune(runeValue) {
			break
		}
		start += size
	}
	for end > start {
		runeValue, size := utf8.DecodeLastRuneInString(value[:end])
		if !xmlSpaceRune(runeValue) {
			break
		}
		end -= size
	}
	return value[start:end]
}
