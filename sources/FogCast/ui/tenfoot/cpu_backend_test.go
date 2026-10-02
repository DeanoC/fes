package tenfoot

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
	"github.com/DeanoC/FogCast/ui/shared"
)

func cpuSceneApp() *App {
	app := NewApp(nil, 1280, 720, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // A command used by the pixel test cannot contact a real host.
	app.ctx = ctx
	app.attractDisabled = true
	app.status = "Ready"
	for i := 0; i < 48; i++ {
		id := fmt.Sprintf("game-%02d", i)
		app.games = append(app.games, availableGame(id, "A game with a cover", "sms"))
		img := image.NewRGBA(image.Rect(0, 0, 160, 224))
		for p := 0; p < len(img.Pix); p += 4 {
			img.Pix[p], img.Pix[p+1], img.Pix[p+2], img.Pix[p+3] = uint8(i*5), 80, 120, 255
		}
		app.covers[id] = &coverSlot{phase: coverReady, image: img}
		app.details[id] = shared.FocusDetail{Title: "A game with a cover"}
	}
	app.grid.SetCount(len(app.games))
	return app
}

// Includes model update, Snapshot and the real warmed renderer; no network,
// decoding or physical presentation. ARM runs show the remaining idle cost.
func BenchmarkCPUBackend(b *testing.B) {
	for _, scene := range []string{"Static", "Navigation", "Settings", "SettingsNavigation", "Room", "Attract"} {
		for _, cached := range []bool{false, true} {
			backend := "Immediate"
			if cached {
				backend = "Cached"
			}
			b.Run(scene+"/"+backend, func(b *testing.B) {
				app := cpuSceneApp()
				if scene == "Settings" || scene == "SettingsNavigation" {
					app.settingsOpen, app.settingsHydrated = true, true
				}
				software, _ := gfx.NewSoftware(1280, 720)
				defer software.Close()
				var dev gfx.Device = software
				if cached {
					dev = gfx.NewFrameCache(software)
				}
				textures, labels := map[string]gpuTexture{}, map[string]gpuTexture{}
				defer destroyTextures(dev, textures)
				defer destroyTextures(dev, labels)
				now := time.Now()
				frame := func(i int) {
					if scene == "Navigation" {
						app.grid.Focus = i % min(12, app.grid.Columns*app.grid.VisibleRows)
					}
					if scene == "SettingsNavigation" {
						app.settingsIndex = i % settingsRowFixedCount
					}
					app.Tick(now)
					snap := app.Snapshot()
					if scene == "Attract" {
						snap.Attract = AttractSnapshot{Active: true, Title: "A game with a cover", Image: app.covers["game-00"].image}
					}
					if scene == "Room" {
						snap.Room = RoomSnapshot{Open: true, Width: 1280, Height: 720, Frame: rooms.Frame{HasClear: true, Clear: gfx.RGB(10, 20, 30), Ops: []rooms.Op{{Kind: rooms.OpRect, X: 100, Y: 100, W: 200, H: 200, Color: gfx.RGB(70, 80, 90)}, {Kind: rooms.OpText, X: 100, Y: 350, Text: "A scripted room", Size: 24}}}}
					}
					presentFrame(dev, snap, textures, labels, false)
				}
				// Warm resource creation, blend state and repeated-resize admission.
				for i := 0; i < 3; i++ {
					frame(0)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					frame(i)
				}
			})
		}
	}
}

func TestCachedCPURendererTracksVisibleAppChanges(t *testing.T) {
	app := cpuSceneApp()
	immediate, _ := gfx.NewSoftware(1280, 720)
	defer immediate.Close()
	software, _ := gfx.NewSoftware(1280, 720)
	defer software.Close()
	cache := gfx.NewFrameCache(software)
	plainTextures, plainLabels := map[string]gpuTexture{}, map[string]gpuTexture{}
	textures, labels := map[string]gpuTexture{}, map[string]gpuTexture{}
	defer destroyTextures(immediate, plainTextures)
	defer destroyTextures(immediate, plainLabels)
	defer destroyTextures(cache, textures)
	defer destroyTextures(cache, labels)
	var decorate func(*Snapshot)
	check := func() {
		snap := app.Snapshot()
		if decorate != nil {
			decorate(&snap)
		}
		presentFrame(immediate, snap, plainTextures, plainLabels, false)
		presentFrame(cache, snap, textures, labels, false)
		if !bytes.Equal(immediate.Framebuffer().Pix, software.Framebuffer().Pix) {
			t.Fatal("cached scene differs from immediate scene")
		}
	}
	check()
	check()
	app.HandleCommand(CmdRight, time.Now())
	check()
	app.HandleCommand(CmdSettings, time.Now())
	check()
	check()
	for i := 0; i < settingsRowFixedCount; i++ {
		app.settingsIndex = i
		check()
	}
	decorate = func(snap *Snapshot) {
		snap.OSK = shared.OSKSnapshot{Open: true, Hint: "type  Enter done  Esc close", Page: shared.OSKPageLetters}
		snap.DebugHUD = DebugHUDSnapshot{Enabled: true, Lines: []string{"A debug overlay outside the panel"}}
	}
	check()
	check()
	decorate = func(snap *Snapshot) {
		snap.OSK = shared.OSKSnapshot{Open: true, Hint: "type", Page: shared.OSKPageLetters}
	}
	check()
	decorate = nil
	check()
	app.status = "Status while settings open"
	check()
	app.covers[app.games[0].ID].image = image.NewRGBA(image.Rect(0, 0, 160, 224))
	check()
	app.SetLayout(LayoutList)
	check()
	app.SetLayout(LayoutShelf)
	check()
	decorate = func(snap *Snapshot) {
		snap.Settings.Rows[0].Value = "Changed setting"
		snap.Settings.Status = "Changed settings status"
		snap.Settings.Rows = snap.Settings.Rows[:3]
		snap.Room = RoomSnapshot{Open: true, Width: 1280, Height: 720, Frame: rooms.Frame{HasClear: true, Clear: gfx.RGB(1, 2, 3), Ops: []rooms.Op{{Kind: rooms.OpRect, X: float32(snap.Settings.Index * 10), Y: 100, W: 40, H: 40, Color: gfx.RGB(255, 0, 0)}}}}
	}
	check()
	check()
	app.settingsIndex = 1
	check()
	decorate = nil
	check()
	app.HandleCommand(CmdBack, time.Now())
	check()
	// Same slot receives asynchronously decoded new artwork.
	app.covers[app.games[0].ID].image = image.NewRGBA(image.Rect(0, 0, 160, 224))
	check()
	app.status = "New connection status"
	check()
	app.SetLayout(LayoutList)
	check()
	app.SetLayout(LayoutShelf)
	check()
	frameImage := app.covers["game-01"].image
	frameSeq := 1
	decorate = func(snap *Snapshot) {
		snap.Attract = AttractSnapshot{Active: true, Image: frameImage, FrameSeq: frameSeq, Title: "Video frames"}
	}
	check()
	check()
	frameImage = image.NewRGBA(frameImage.Bounds())
	frameSeq++
	check()
	check()
	x := float32(100)
	decorate = func(snap *Snapshot) {
		snap.Room = RoomSnapshot{Open: true, Width: 1280, Height: 720, Frame: rooms.Frame{HasClear: true, Clear: gfx.RGB(1, 2, 3), Ops: []rooms.Op{{Kind: rooms.OpRect, X: x, Y: 100, W: 40, H: 40, Color: gfx.RGB(255, 0, 0)}}}}
	}
	check()
	check()
	x = 200
	check()
	check()
	decorate = nil
	check()
}
