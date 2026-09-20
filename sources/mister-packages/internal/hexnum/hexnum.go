package hexnum

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Uint64 is a YAML scalar that accepts 0x-prefixed hex or decimal.
type Uint64 uint64

func (h *Uint64) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("expected scalar hex or decimal, got %v", value.Kind)
	}
	parsed, err := Parse(value.Value)
	if err != nil {
		return err
	}
	*h = Uint64(parsed)
	return nil
}

func (h Uint64) MarshalYAML() (interface{}, error) {
	return fmt.Sprintf("0x%x", uint64(h)), nil
}

func Parse(text string) (uint64, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, fmt.Errorf("empty number")
	}
	if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
		return strconv.ParseUint(text[2:], 16, 64)
	}
	return strconv.ParseUint(text, 10, 64)
}

func Format(value uint64) string {
	return fmt.Sprintf("0x%x", value)
}
