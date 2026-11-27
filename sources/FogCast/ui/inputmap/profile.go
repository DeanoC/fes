// Package inputmap remaps a kit pad's logical controls through the profile
// tenfoot.json saves for that pad.
package inputmap

// PresetSwapAB trades A and B, for players who confirm with the right-hand
// face button: Nintendo A, or Circle on a Japanese PlayStation.
const PresetSwapAB = "swap-ab"

// Menu Settings shortcut modes saved per pad.
const (
	SettingsComboAuto        = ""
	SettingsComboSelectStart = "select-start"
	SettingsComboOff         = "off"
)

// Profile is a pad's saved remap, applied after the pad's mapping. A preset
// applies first and Bindings override it control by control. Bindings are a
// single lookup, not a chain: "a"→"b" and "b"→"a" swaps the two buttons.
//
// Mapping replaces the pad's GameControllerDB mapping with one its player
// made step by step, in GameControllerDB's binding form
// ("a:b0,b:b1,dpup:h0.1,..."). Preset and Bindings then apply on top of it.
type Profile struct {
	Mapping       string            `json:"mapping,omitempty"`
	Preset        string            `json:"preset,omitempty"`
	SettingsCombo string            `json:"settings_combo,omitempty"`
	Bindings      map[string]string `json:"bindings,omitempty"`
}
