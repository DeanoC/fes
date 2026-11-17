package tenfoot

import (
	"strings"

	"github.com/DeanoC/FogCast/remoteinput"
	"github.com/DeanoC/FogCast/ui/controller"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/inputmap"
)

// PadPrompts is how one pad's hints are drawn: the glyph for each button
// word a gamepad hint uses ("A", "LB", "GUIDE"). A word it lacks stays a
// word.
type PadPrompts map[string]string

// NewPadPrompts picks a pad's glyphs. A hint names the control FES acts on,
// so each word gets the glyph of the button that sends that control through
// the pad's saved profile: with A and B swapped, "A" is drawn as the pad's
// right-hand face button.
func NewPadPrompts(family controller.Family, remap *inputmap.Remapper) PadPrompts {
	f := gfx.PromptGeneric
	switch family {
	case controller.FamilyXbox:
		f = gfx.PromptXbox
	case controller.FamilyPlayStation:
		f = gfx.PromptPlayStation
	case controller.FamilyNintendo:
		f = gfx.PromptNintendo
	}
	buttons := []struct {
		word    string
		code    remoteinput.Code
		control gfx.PromptControl
	}{
		{"A", remoteinput.ButtonA, gfx.PromptSouth},
		{"B", remoteinput.ButtonB, gfx.PromptEast},
		{"X", remoteinput.ButtonX, gfx.PromptWest},
		{"Y", remoteinput.ButtonY, gfx.PromptNorth},
		{"LB", remoteinput.ButtonL, gfx.PromptL},
		{"RB", remoteinput.ButtonR, gfx.PromptR},
		{"SELECT", remoteinput.ButtonSelect, gfx.PromptSelect},
		{"START", remoteinput.ButtonStart, gfx.PromptStart},
	}
	// GUIDE is never remapped, and a hint's D-pad means the whole pad.
	p := PadPrompts{
		"GUIDE": string(gfx.PromptRune(f, gfx.PromptGuide)),
		"D-pad": string(gfx.PromptRune(f, gfx.PromptDPad)),
	}
	for _, want := range buttons {
		// The button that still sends its own control wins over one
		// remapped onto it.
		if remap.Target(want.code) == want.code {
			p[want.word] = string(gfx.PromptRune(f, want.control))
			continue
		}
		for _, b := range buttons {
			if remap.Target(b.code) == want.code {
				p[want.word] = string(gfx.PromptRune(f, b.control))
				break
			}
		}
	}
	return p
}

// apply draws a gamepad hint's button words as p's glyphs. Only a word that
// opens a prompt is a button: at the start, after the double space or "·"
// that separates prompts, or after "hold". "LB/RB" becomes both glyphs.
func (p PadPrompts) apply(hint string) string {
	if len(p) == 0 {
		return hint
	}
	words := strings.Split(hint, " ")
	for i, w := range words {
		if i > 0 {
			switch words[i-1] {
			case "", "·", "|", "hold", "Hold":
			default:
				continue
			}
		}
		var glyphs strings.Builder
		for _, part := range strings.Split(w, "/") {
			g, ok := p[part]
			if !ok {
				glyphs.Reset()
				break
			}
			glyphs.WriteString(g)
		}
		if glyphs.Len() > 0 {
			words[i] = glyphs.String()
		}
	}
	return strings.Join(words, " ")
}

// SetPadPrompts records how gamepad id's hints are drawn while it is
// attached.
func (a *App) SetPadPrompts(id int, p PadPrompts) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.padPrompts == nil {
		a.padPrompts = map[int]PadPrompts{}
	}
	a.padPrompts[id] = p
}

// affinityPromptsLocked is the glyph set of the pad that owns the hints.
func (a *App) affinityPromptsLocked() PadPrompts {
	if a.affinity.current.Kind != InputGamepad {
		return nil
	}
	return a.padPrompts[a.affinity.current.ID]
}
