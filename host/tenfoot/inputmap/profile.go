// Package inputmap is the shared remap and multi-device merge layer for kit
// controllers, linuxinput readers, and the tenfoot command adapter.
package inputmap

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	NameIdentity = "identity"
	NameSwapAB   = "swap-ab"
)

// Profile maps physical or already-normalized controls onto logical
// remoteinput codes. Bindings are a single lookup, not a chain: "a"→"b" and
// "b"→"a" swaps the two buttons.
type Profile struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Bindings    map[string]string `json:"bindings"`
}

// Identity is the built-in pass-through profile. Kit A still launches,
// Select+Start still stops, and d-pad/stick still navigate the 4×3 grid.
func Identity() Profile {
	return Profile{
		Name:        NameIdentity,
		Description: "Pass-through mapping; A launches, Select+Start stops, d-pad and stick navigate.",
		Bindings:    map[string]string{},
	}
}

// SwapAB is the named example profile: A and B swap after device normalization.
func SwapAB() Profile {
	return Profile{
		Name:        NameSwapAB,
		Description: "Swap A and B after device normalization.",
		Bindings:    map[string]string{"a": "b", "b": "a"},
	}
}

// Builtin returns a built-in profile. Empty and "identity" are the pass-through.
func Builtin(name string) (Profile, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", NameIdentity:
		return Identity(), true
	case NameSwapAB, "swap_ab":
		return SwapAB(), true
	default:
		return Profile{}, false
	}
}

// Resolve loads a built-in name or a JSON file path. Empty selects identity.
func Resolve(spec string) (Profile, error) {
	spec = strings.TrimSpace(spec)
	if p, ok := Builtin(spec); ok {
		return p, nil
	}
	return Load(spec)
}

// Load reads a JSON profile from path.
func Load(path string) (Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return Profile{}, fmt.Errorf("inputmap: open profile: %w", err)
	}
	defer f.Close()
	p, err := Decode(f)
	if err != nil {
		return Profile{}, err
	}
	if strings.TrimSpace(p.Name) == "" {
		p.Name = strings.TrimSpace(path)
	}
	return p, nil
}

// Decode reads one JSON profile from r.
func Decode(r io.Reader) (Profile, error) {
	var p Profile
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Profile{}, fmt.Errorf("inputmap: invalid profile: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Profile{}, errors.New("inputmap: invalid profile")
	}
	if p.Bindings == nil {
		p.Bindings = map[string]string{}
	}
	if err := validateBindings(p.Bindings); err != nil {
		return Profile{}, err
	}
	return p, nil
}
