// Package inputmap remaps a kit pad's logical controls through the profile
// tenfoot.json saves for that pad.
package inputmap

// PresetSwapAB trades A and B, for players who confirm with the right-hand
// face button: Nintendo A, or Circle on a Japanese PlayStation.
const PresetSwapAB = "swap-ab"

// Profile is a pad's saved remap, applied after the pad's mapping. A preset
// applies first and Bindings override it control by control. Bindings are a
// single lookup, not a chain: "a"→"b" and "b"→"a" swaps the two buttons.
type Profile struct {
	Preset   string            `json:"preset,omitempty"`
	Bindings map[string]string `json:"bindings,omitempty"`
}
