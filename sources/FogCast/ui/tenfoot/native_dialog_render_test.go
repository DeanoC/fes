package tenfoot

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/ui/gfx"
)

// The production panel geometry is also used by pointer routing. Keep the
// focused row and both footer lines inside the supported safe area.
func TestNativeDialogRender(t *testing.T) {
	for _, size := range [][2]int{{1280, 720}, {1024, 600}} {
		for _, scene := range []string{"setup", "files", "settings", "home"} {
			t.Run(fmt.Sprintf("%s-%dx%d", scene, size[0], size[1]), func(t *testing.T) {
				app := libraryRenderApp(t, size[0], size[1], true)
				snap := app.Snapshot()
				snap.Grid.Safe = insetsFromPct(size[0], size[1], DefaultSafeAreaPct)
				snap.Grid.Layout(size[0], size[1])
				var panel listPanel
				var ok bool
				switch scene {
				case "setup", "files":
					picker := FirmwarePickerSnapshot{Open: true, Title: "Set up ZX Spectrum +3", Index: 7,
						Status: "Skipped 1 package that could not be checked. Choose a usable installed version, or refresh systems to retry.",
						Hint:   "Enter choose  Esc back"}
					for i := 0; i < 12; i++ {
						picker.Rows = append(picker.Rows, FirmwarePickerRow{Name: fmt.Sprintf("ZX Spectrum +3 1.%d · installed · required firmware", i), Selectable: true})
					}
					if scene == "files" {
						picker.Title = "Choose firmware"
						picker.Status = "Choose the required 64 KiB ROM. The selected file is imported to the library before this setup is added."
						picker.Path = "/media/library/firmware/Sinclair/ZX Spectrum +3/"
						for i := range picker.Rows {
							picker.Rows[i] = FirmwarePickerRow{Name: fmt.Sprintf("ZX Spectrum +3 firmware revision %02d.rom", i), Kind: firmwarePickerKindFile, Size: 65536, Selectable: true}
						}
					}
					snap.CoreLibrary = picker
					overlay := snap
					overlay.FirmwarePicker = picker
					panel, ok = firmwarePickerPanel(overlay)
				case "settings":
					app.settingsOpen = true
					snap.Settings = app.settingsSnapshotLocked()
					snap.Settings.Open = true
					snap.Settings.Index = 10
					snap.Settings.Hint = "Choose a setting. Changes apply to the next start."
					panel, ok = settingsPanel(snap)
				case "home":
					snap.RoomPicker = RoomPickerSnapshot{Open: true, Index: 5}
					for i := 0; i < 10; i++ {
						snap.RoomPicker.Rows = append(snap.RoomPicker.Rows, RoomPickerRow{ID: fmt.Sprint(i), Label: "ZX Spectrum workbench", Detail: "Choose a variant, prepared title, media and expansion sockets."})
					}
					panel, ok = roomPickerPanel(snap)
				}
				if !ok || panel.X-4 < snap.Grid.contentLeft() || panel.Y-4 < snap.Grid.contentTop() || panel.X+panel.W+4 > snap.Grid.Width-snap.Grid.Safe.Right || panel.Y+panel.H+4 > snap.Grid.Height-snap.Grid.Safe.Bottom {
					t.Fatalf("panel escapes the safe area: %+v safe %+v", panel, snap.Grid.Safe)
				}
				dev, err := gfx.NewSoftware(size[0], size[1])
				if err != nil {
					t.Fatal(err)
				}
				presentFrame(dev, snap, map[string]gpuTexture{}, map[string]gpuTexture{}, false)
				if out := strings.TrimSpace(os.Getenv("FES_ROOM_SCREENSHOT_DIR")); out != "" {
					if err := os.MkdirAll(out, 0o755); err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(out, fmt.Sprintf("native-%s-%dx%d.png", scene, size[0], size[1]))
					file, err := os.Create(path)
					if err != nil {
						t.Fatal(err)
					}
					if err := png.Encode(file, dev.Snapshot()); err != nil {
						t.Fatal(err)
					}
					if err := file.Close(); err != nil {
						t.Fatal(err)
					}
					t.Log(path)
				}
			})
		}
	}
}
