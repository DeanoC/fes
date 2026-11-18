package tenfoot

import (
	"image"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/shared"
)

func TestOverlayFootersRenderTheConnectedPadsGlyphs(t *testing.T) {
	panels := []struct {
		name, slot, keyFragment string
		draw                    func(gfx.Device, Snapshot, map[string]gpuTexture, map[string]struct{})
		set                     func(*Snapshot) string
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
		{
			name: "view picker",
			slot: "view-hint",
			draw: drawViewPicker,
			set: func(snap *Snapshot) string {
				snap.ViewPicker = true
				snap.PickerRows = []LibraryView{{ID: "all", Label: "All"}}
				return viewPickerFooterHint(snap.Affinity)
			},
		},
		{
			name: "room picker",
			slot: "rooms-hint-",
			draw: drawRoomPicker,
			set: func(snap *Snapshot) string {
				snap.RoomPicker = RoomPickerSnapshot{Open: true, Rows: []RoomPickerRow{{ID: "home", Label: "Home"}}}
				return roomPickerHint(snap.Affinity)
			},
		},
		{
			name: "settings footer",
			slot: "set-status-",
			draw: drawSettings,
			set: func(snap *Snapshot) string {
				hint := "A confirm  B close  Left/Right change"
				snap.Settings = SettingsSnapshot{Open: true, Title: "Settings", Rows: []SettingsRow{{ID: "test", Label: "Test"}}, Hint: hint}
				return hint
			},
		},
		{
			name: "settings default footer",
			slot: "set-status-",
			draw: drawSettings,
			set: func(snap *Snapshot) string {
				snap.Settings = SettingsSnapshot{Open: true, Title: "Settings", Rows: []SettingsRow{{ID: "test", Label: "Test"}}}
				return settingsDefaultHint(snap.Affinity, "close")
			},
		},
		{
			name: "filters footer",
			slot: "flt-status-",
			draw: drawFilters,
			set: func(snap *Snapshot) string {
				hint := "A select  B back"
				snap.Filters = FilterSnapshot{Open: true, Title: "Filters", Rows: []FilterRow{{ID: "genre", Label: "Genre"}}, Hint: hint}
				return hint
			},
		},
		{
			name: "filters default footer",
			slot: "flt-status-",
			draw: drawFilters,
			set: func(snap *Snapshot) string {
				snap.Filters = FilterSnapshot{Open: true, Title: "Filters", Rows: []FilterRow{{ID: "genre", Label: "Genre"}}}
				return filterHintFor(snap.Affinity, false)
			},
		},
		{
			name: "OSK hint",
			slot: "osk-hint",
			draw: drawOSK,
			set: func(snap *Snapshot) string {
				hint := "A done  B close"
				snap.OSK = shared.OSKSnapshot{Open: true, Rows: [][]shared.OSKKey{{{ID: "a", Label: "A", Kind: shared.OSKInsert, Text: "a"}}}, Hint: hint}
				return hint
			},
		},
		{
			name: "OSK default hint",
			slot: "osk-hint",
			draw: drawOSK,
			set: func(snap *Snapshot) string {
				snap.OSK = shared.OSKSnapshot{Open: true, Rows: [][]shared.OSKKey{{{ID: "a", Label: "A", Kind: shared.OSKInsert, Text: "a"}}}}
				return oskHintFor(snap.Affinity, snap.OSK.Page)
			},
		},
		{
			name: "room choice hint",
			slot: "rchoice-hint",
			draw: drawRoomChoice,
			set: func(snap *Snapshot) string {
				hint := "A choose  Y details  B close"
				snap.Room.Choice = RoomChoiceSnapshot{Open: true, Hint: hint}
				return hint
			},
		},
		{
			name: "room choice default hint",
			slot: "rchoice-hint",
			draw: drawRoomChoice,
			set: func(snap *Snapshot) string {
				snap.Room.Choice = RoomChoiceSnapshot{Open: true}
				return roomChoiceHint(snap.Affinity)
			},
		},
		{
			name: "room error hint",
			slot: "room-err-hint",
			draw: drawRoomError,
			set: func(snap *Snapshot) string {
				snap.Room.Err = "room failed"
				return roomErrorHint(snap.Room, snap.Affinity)
			},
		},
		{
			name:        "firmware card hint",
			keyFragment: "|18|r|",
			draw:        drawFirmwareCard,
			set: func(snap *Snapshot) string {
				snap.FirmwareCard = FirmwareCardSnapshot{Open: true, LicenceView: true}
				return firmwareCardHint(snap.Affinity, snap.FirmwareCard)
			},
		},
		{
			name: "surround hint",
			slot: "surround-hint",
			draw: func(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, _ map[string]struct{}) {
				drawSurround(dev, snap, labels)
			},
			set: func(snap *Snapshot) string {
				snap.Surround.Picture = image.Rect(400, 100, 880, 620)
				snap.Session.HardwareRoom = true
				snap.Session.Title = "ZX81"
				return playingRoomLabel(snap.Session) + " · " + hardwareRoomWord(snap.Affinity)
			},
		},
		{
			name: "hardware room button prompt",
			slot: "np-hardware",
			draw: drawHardwareRoomButton,
			set: func(snap *Snapshot) string {
				snap.GPUParked = true
				snap.Session.HardwareRoom = true
				snap.Session.Title = "ZX81"
				return playingRoomLabel(snap.Session) + " · " + hardwareRoomWord(snap.Affinity)
			},
		},
		{
			name:        "hero action prompt",
			keyFragment: "Play",
			draw: func(dev gfx.Device, snap Snapshot, labels map[string]gpuTexture, used map[string]struct{}) {
				heroButton(snap, dev, labels, used, 20, 20, heroButtonH, "A", "Play", true)
			},
			set: func(*Snapshot) string { return "A  Play" },
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
				keys := make([]string, 0, len(labels))
				for key, label := range labels {
					keys = append(keys, key)
					if panel.slot != "" && strings.HasPrefix(key, panel.slot) {
						rendered = label.labelDisplay
						break
					}
					if panel.keyFragment != "" && strings.Contains(key, panel.keyFragment) {
						parts := strings.SplitN(key, "|", 3)
						if len(parts) >= 2 {
							rendered = parts[1]
							break
						}
					}
				}
				if rendered == "" {
					t.Fatalf("renderer did not draw %s footer; labels=%v", panel.name, keys)
				}

				got := strings.Join(strings.Fields(gfx.PromptWords(rendered)), " ")
				want := strings.Join(strings.Fields(gfx.PromptWords(snap.padHint(rawHint))), " ")
				if got != want {
					t.Fatalf("%s footer shows %q, want connected-pad prompts %q", panel.name, got, want)
				}
				if xboxWords := strings.Join(strings.Fields(gfx.PromptWords(rawHint)), " "); got == xboxWords {
					t.Fatalf("%s footer still shows unconverted button words %q", panel.name, got)
				}
			})
		}
	}
}

func TestHeroTextPromptsUseTheConnectedPadGlyphs(t *testing.T) {
	families := []struct {
		name   string
		family gfx.PromptFamily
	}{
		{name: "PlayStation", family: gfx.PromptPlayStation},
		{name: "Nintendo", family: gfx.PromptNintendo},
	}
	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			snap := Snapshot{Affinity: InputGamepad, Prompts: NewPadPrompts(family.family, nil)}
			if got, want := waitAckCopy(snap), "Still checking. Press "+snap.padHint("A")+" again once it has finished."; got != want {
				t.Fatalf("checking prompt = %q, want %q", got, want)
			}
			if got, want := heroStopCopyFor(snap), "Stop: hold "+snap.padHint("SELECT")+" + "+snap.padHint("START")+" for one second."; got != want {
				t.Fatalf("stop prompt = %q, want %q", got, want)
			}
		})
	}
}
