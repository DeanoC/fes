package inputmap

import (
	"testing"

	"github.com/DeanoC/FogCast/remoteinput"
)

func press(t *testing.T, name string) remoteinput.Event {
	t.Helper()
	e, err := remoteinput.NormalizeGamepad(name, true)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEmptyProfilePassesThrough(t *testing.T) {
	t.Parallel()
	r, err := NewRemapper(Profile{})
	if err != nil {
		t.Fatal(err)
	}
	a := press(t, "a")
	if got := r.Apply(a); got != a {
		t.Fatalf("empty profile mutated %+v -> %+v", a, got)
	}
	axis, _ := remoteinput.NormalizeAxis("left-x", 32767)
	if got := r.Apply(axis); got != axis {
		t.Fatalf("axis mutated")
	}
	var none *Remapper
	if got := none.Apply(a); got != a {
		t.Fatalf("nil remapper mutated %+v", got)
	}
}

func TestSwapABPreset(t *testing.T) {
	t.Parallel()
	r, err := NewRemapper(Profile{Preset: PresetSwapAB})
	if err != nil {
		t.Fatal(err)
	}
	for from, want := range map[string]remoteinput.Code{
		"a": remoteinput.ButtonB, "b": remoteinput.ButtonA, "select": remoteinput.ButtonSelect,
	} {
		if got := r.Apply(press(t, from)); got.Code != want {
			t.Errorf("%s -> %v, want %v", from, got.Code, want)
		}
	}
}

// Bindings override the preset control by control and are looked up once:
// x→a does not then become b through the preset.
func TestBindingsOverrideThePresetInOneLookup(t *testing.T) {
	t.Parallel()
	r, err := NewRemapper(Profile{Preset: PresetSwapAB, Bindings: map[string]string{
		"b": "keypad-1", "X": "a", "r": "keypad-hash",
	}})
	if err != nil {
		t.Fatal(err)
	}
	for from, want := range map[string]remoteinput.Code{
		"a": remoteinput.ButtonB, "b": remoteinput.Keypad0 + 1, "x": remoteinput.ButtonA, "r": remoteinput.KeypadHash,
	} {
		if got := r.Apply(press(t, from)); got.Code != want {
			t.Errorf("%s -> %v, want %v", from, got.Code, want)
		}
	}
}

func TestUnknownPresetOrControlIsRejected(t *testing.T) {
	t.Parallel()
	for _, p := range []Profile{
		{Preset: "swapab"},
		{Bindings: map[string]string{"a": "keypad-10"}},
		{Bindings: map[string]string{"evdev:304": "select"}},
		{Bindings: map[string]string{"left-x": "left-y"}},
	} {
		if _, err := NewRemapper(p); err == nil {
			t.Errorf("%+v compiled", p)
		}
	}
}
