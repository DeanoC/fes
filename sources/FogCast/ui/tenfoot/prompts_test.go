package tenfoot

import (
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

func promptWords(p PadPrompts, hint string) string { return gfx.PromptWords(p.apply(hint)) }

func TestPadPromptsFollowTheFamily(t *testing.T) {
	t.Parallel()
	hint := "LB/RB platform  X sort  hold X filter  Y search  hold A view  SELECT layout  hold B home  GUIDE settings"
	for family, want := range map[gfx.PromptFamily]string{
		gfx.PromptXbox:        "LBRB platform  X sort  hold X filter  Y search  hold A view  VIEW layout  hold B home  GUIDE settings",
		gfx.PromptPlayStation: "L1R1 platform  Square sort  hold Square filter  Triangle search  hold Cross view  CREATE layout  hold Circle home  PS settings",
		gfx.PromptNintendo:    "LR platform  Y sort  hold Y filter  X search  hold B view  - layout  hold A home  HOME settings",
		gfx.PromptGeneric:     "LR platform  left button sort  hold left button filter  top button search  hold bottom button view  SELECT layout  hold right button home  GUIDE settings",
	} {
		if got := promptWords(NewPadPrompts(family, nil), hint); got != want {
			t.Errorf("%v:\n got %q\nwant %q", family, got, want)
		}
	}
}

// A hint names the control; the glyph is the button that sends it.
func TestPadPromptsFollowTheSavedProfile(t *testing.T) {
	t.Parallel()
	swap, err := inputmap.NewRemapper(inputmap.Profile{Preset: inputmap.PresetSwapAB})
	if err != nil {
		t.Fatal(err)
	}
	if got := promptWords(NewPadPrompts(gfx.PromptNintendo, swap), "A select  B back"); got != "A select  B back" {
		t.Errorf("Nintendo pad with A and B swapped shows %q", got)
	}
	if got := promptWords(NewPadPrompts(gfx.PromptPlayStation, swap), "A select  B back"); got != "Circle select  Cross back" {
		t.Errorf("PlayStation pad with A and B swapped shows %q", got)
	}
	// X sends A as well; the button that still sends A keeps the prompt.
	// Nothing sends X any more, so that prompt stays a word.
	both, err := inputmap.NewRemapper(inputmap.Profile{Bindings: map[string]string{"x": "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := promptWords(NewPadPrompts(gfx.PromptPlayStation, both), "A select  X sort"); got != "Cross select  X sort" {
		t.Errorf("x bound to a shows %q", got)
	}
	// Only Y sends A.
	moved, err := inputmap.NewRemapper(inputmap.Profile{Bindings: map[string]string{"a": "c", "y": "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := promptWords(NewPadPrompts(gfx.PromptXbox, moved), "A select"); got != "Y select" {
		t.Errorf("y bound to a shows %q", got)
	}
}

func TestPadPromptsLeaveProseAlone(t *testing.T) {
	t.Parallel()
	p := NewPadPrompts(gfx.PromptPlayStation, nil)
	for hint, want := range map[string]string{
		"A choose  B return to session  ·  Stop to play another cartridge": "Cross choose  Circle return to session  ·  Stop to play another cartridge",
		"D-pad move  ·  A select  ·  B back":                               "D-pad move  ·  Cross select  ·  Circle back",
		"Press A button on another pad":                                    "Press A button on another pad",
		"A choose  ·  Arrows / D-pad move":                                 "Cross choose  ·  Arrows / D-pad move",
		"LEFT/RIGHT screenshots  START quit":                               "LEFT/RIGHT screenshots  OPTIONS quit",
	} {
		if got := promptWords(p, hint); got != want {
			t.Errorf("%q\n got %q\nwant %q", hint, got, want)
		}
	}
	if got := PadPrompts(nil).apply("A select"); got != "A select" {
		t.Errorf("no prompts changed the hint to %q", got)
	}
}

func TestHeaderHintUsesTheAffinityPadsGlyphs(t *testing.T) {
	t.Parallel()
	app := NewApp(nil, 1280, 720, 100)
	app.SetPadPrompts(7, NewPadPrompts(gfx.PromptPlayStation, nil))
	app.AttachInput(InputGamepad, 7)
	words := app.Snapshot()
	words.Prompts = nil
	snap := app.Snapshot()
	if snap.HeaderHint() == words.HeaderHint() || gfx.PromptWords(snap.HeaderHint()) == snap.HeaderHint() {
		t.Fatalf("pad hint %q has no glyphs", snap.HeaderHint())
	}
	app.AttachInput(InputKeyboard, 1)
	if snap := app.Snapshot(); snap.Prompts != nil || snap.HeaderHint() != gfx.PromptWords(snap.HeaderHint()) {
		t.Fatalf("keyboard hint %q has glyphs", snap.HeaderHint())
	}
	app.NoteInput(InputGamepad, 7)
	app.DetachInput(InputGamepad, 7)
	app.AttachInput(InputGamepad, 7)
	if snap := app.Snapshot(); snap.Prompts != nil {
		t.Fatal("a detached pad's glyphs outlived it")
	}
}
