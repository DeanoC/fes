package tenfoot

import (
	"fmt"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/rooms"
)

// Exercise the actual installed-cores Home room with only the controls on a
// sparse pad or keyboard. The compact layout has no hero action row.
func TestHomeDirectionalRoutesAndReturn(t *testing.T) {
	inputs := []struct {
		name                           string
		kind                           InputKind
		up, left, right, confirm, back Command
	}{
		{"keyboard", InputKeyboard, CommandFromKey("up"), CommandFromKey("left"), CommandFromKey("right"), CommandFromKey("return"), CommandFromKey("escape")},
		{"gamepad", InputGamepad, CommandFromButton(ButtonDPadUp), CommandFromButton(ButtonDPadLeft), CommandFromButton(ButtonDPadRight), CommandFromButton(ButtonSouth), CommandFromButton(ButtonEast)},
	}
	for _, size := range [][2]int{{1024, 600}, {470, 350}} {
		for _, input := range inputs {
			for _, route := range []string{"Library", "Systems", "Settings"} {
				t.Run(fmt.Sprintf("%s-%dx%d-%s", input.name, size[0], size[1], route), func(t *testing.T) {
					host := newRoomHost(t)
					app := startHomeLoungeOn(t, host, t.TempDir(), &fakeLocalCores{cores: homeFixtureCores()}, size[0], size[1], true)
					app.NoteInput(input.kind, 1)
					initial := waitFor(t, app, "Home family", homeFamilySettled)
					app.HandleCommand(input.up, time.Now())
					if initial.Room.Hero {
						if !app.Snapshot().BrowseActions.Focused {
							t.Fatal("Up did not enter the hero actions")
						}
						app.HandleCommand(input.up, time.Now())
					}
					// Normalise the header to its first route regardless of which
					// end the compact and hero layouts enter from.
					app.HandleCommand(input.left, time.Now())
					app.HandleCommand(input.left, time.Now())
					if got := app.Snapshot().Room.Destination; got.Kind != rooms.KindLibrary {
						t.Fatalf("directions did not reach Library: %+v", got)
					}
					for n := 0; n < map[string]int{"Library": 0, "Systems": 1, "Settings": 2}[route]; n++ {
						app.HandleCommand(input.right, time.Now())
					}
					if got := app.Snapshot().Room.Destination.Label; got != route {
						t.Fatalf("header route = %q, want %q", got, route)
					}
					app.HandleCommand(input.confirm, time.Now())
					switch route {
					case "Library":
						waitFor(t, app, "Library", func(s Snapshot) bool { return !s.Room.Open && !s.Settings.Open && s.LibraryBackTo == "Home" })
					case "Systems":
						waitFor(t, app, "Systems", func(s Snapshot) bool { return s.Room.Open && s.Room.ID == "example.computers" })
					case "Settings":
						waitFor(t, app, "Settings", func(s Snapshot) bool { return s.Settings.Open })
					}
					app.HandleCommand(input.back, time.Now())
					waitFor(t, app, "back to Home", func(s Snapshot) bool {
						return s.Room.Open && s.Room.ID == installedCoresHomeID && !s.Settings.Open && !s.RoomPicker.Open
					})
					if n := host.launchCount(); n != 0 {
						t.Fatalf("visiting %s launched %d games", route, n)
					}
				})
			}
		}
	}
}

// Release Home exposes only Settings in its header. The development-only
// Library and Systems routes above must not become a prerequisite for it.
func TestReleaseHomeDirectionalSettingsAndReturn(t *testing.T) {
	for _, size := range [][2]int{{1024, 600}, {470, 350}} {
		for _, input := range []struct {
			name                           string
			kind                           InputKind
			up, left, right, confirm, back Command
		}{
			{"keyboard", InputKeyboard, CommandFromKey("up"), CommandFromKey("left"), CommandFromKey("right"), CommandFromKey("return"), CommandFromKey("escape")},
			{"gamepad", InputGamepad, CommandFromButton(ButtonDPadUp), CommandFromButton(ButtonDPadLeft), CommandFromButton(ButtonDPadRight), CommandFromButton(ButtonSouth), CommandFromButton(ButtonEast)},
		} {
			t.Run(fmt.Sprintf("%s-%dx%d", input.name, size[0], size[1]), func(t *testing.T) {
				app := startHomeLounge(t, &fakeLocalCores{cores: homeFixtureCores()}, size[0], size[1])
				app.NoteInput(input.kind, 1)
				initial := waitFor(t, app, "release Home", homeFamilySettled)
				app.HandleCommand(input.up, time.Now())
				if initial.Room.Hero {
					app.HandleCommand(input.up, time.Now())
				}
				for _, command := range []Command{input.left, input.right} {
					app.HandleCommand(command, time.Now())
					if got := app.Snapshot().Room.Destination; got.Kind != rooms.KindAction || got.Label != "Settings" {
						t.Fatalf("release Home header = %+v, want Settings only", got)
					}
				}
				app.HandleCommand(input.confirm, time.Now())
				waitFor(t, app, "Settings", func(s Snapshot) bool { return s.Settings.Open })
				app.HandleCommand(input.back, time.Now())
				waitFor(t, app, "back to release Home", func(s Snapshot) bool {
					return s.Room.Open && s.Room.ID == installedCoresHomeID && !s.Settings.Open && !s.RoomPicker.Open
				})
			})
		}
	}
}
