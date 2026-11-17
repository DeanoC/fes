package tenfoot

import (
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestOverlayFootersRenderTheConnectedPadsGlyphs(t *testing.T) {
	panels := []struct {
		name string
		slot string
		draw func(gfx.Device, Snapshot, map[string]gpuTexture, map[string]struct{})
		set  func(*Snapshot) string
	}{
		{
			name: "tape picker",
			slot: "tape-hint",
			draw: drawTapePicker,
			set: func(snap *Snapshot) string {
				snap.TapePicker = TapePickerSnapshot{Open: true, Hint: "A insert  B back"}
				return snap.TapePicker.Hint
			},
		},
		{
			name: "firmware picker",
			slot: "fw-hint",
			draw: drawFirmwarePicker,
			set: func(snap *Snapshot) string {
				snap.FirmwarePicker = FirmwarePickerSnapshot{Open: true, Hint: "A choose  B back"}
				return snap.FirmwarePicker.Hint
			},
		},
		{
			name: "collection menu",
			slot: "cmenu-hint",
			draw: drawCollectionMenu,
			set: func(snap *Snapshot) string {
				snap.CollectionMenu = CollectionMenuSnapshot{Open: true, Title: "Collections", Rows: []string{"All"}, Hint: "A open  B back"}
				return snap.CollectionMenu.Hint
			},
		},
		{
			name: "launch overlay",
			slot: "launch-ov-hint",
			draw: drawLaunchOverlay,
			set: func(snap *Snapshot) string {
				snap.Room.Open = true
				snap.Launch.Phase = "launching"
				return launchOverlayCopy(*snap).Hint
			},
		},
	}
	families := []struct {
		name   string
		family gfx.PromptFamily
	}{
		{name: "PlayStation", family: gfx.PromptPlayStation},
		{name: "Nintendo", family: gfx.PromptNintendo},
	}

	for _, panel := range panels {
		for _, family := range families {
			t.Run(panel.name+"/"+family.name, func(t *testing.T) {
				app := NewApp(nil, 1280, 720, 100)
				snap := app.Snapshot()
				snap.Grid.Safe = insetsFromPct(1280, 720, DefaultSafeAreaPct)
				snap.Grid.Layout(1280, 720)
				snap.Affinity = InputGamepad
				snap.Prompts = NewPadPrompts(family.family, nil)
				rawHint := panel.set(&snap)

				labels := map[string]gpuTexture{}
				panel.draw(gfx.NewRecorder(), snap, labels, map[string]struct{}{})

				var rendered string
				for key, label := range labels {
					if strings.HasPrefix(key, panel.slot+"\x1f") {
						rendered = label.labelDisplay
						break
					}
				}
				if rendered == "" {
					t.Fatalf("renderer did not draw %s footer", panel.name)
				}

				got := gfx.PromptWords(rendered)
				want := gfx.PromptWords(snap.padHint(rawHint))
				if got != want {
					t.Fatalf("%s footer shows %q, want connected-pad prompts %q", panel.name, got, want)
				}
				if xboxWords := gfx.PromptWords(rawHint); got == xboxWords {
					t.Fatalf("%s footer still shows unconverted button words %q", panel.name, got)
				}
			})
		}
	}
}
