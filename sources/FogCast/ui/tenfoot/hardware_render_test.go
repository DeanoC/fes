package tenfoot

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/rooms"
)

type hardwareRenderServices struct {
	roomServices
	snapshot hostclient.HardwareSnapshot
}

func (s hardwareRenderServices) Hardware(context.Context) (hostclient.HardwareSnapshot, error) {
	return s.snapshot, nil
}

type hardwareMissingArtwork struct{ fs.FS }

func assertWorkshopLabelFits(t *testing.T, f rooms.Frame, shown string, extras ...string) {
	t.Helper()
	var op rooms.Op
	var found bool
	for _, candidate := range f.Ops {
		if candidate.Kind == rooms.OpText && candidate.Text == shown {
			op = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing %q", shown)
	}
	for _, label := range append([]string{shown}, extras...) {
		for _, weight := range []gfx.Weight{gfx.WeightRegular, gfx.WeightBold} {
			if width := gfx.MeasureTextWeight(label, op.Size, weight); width > op.MaxW {
				t.Fatalf("%q size %d is %dpx, button max %d", label, op.Size, width, op.MaxW)
			}
		}
	}
}

func (f hardwareMissingArtwork) Open(name string) (fs.File, error) {
	if strings.HasPrefix(name, "assets/") {
		return nil, fs.ErrNotExist
	}
	return f.FS.Open(name)
}

// These tests verify rendering and refresh behavior, not execution speed.
// Race instrumentation and shared CI scheduling can exceed production deadlines.
func hardwareRenderTestBudget() rooms.Budget {
	return rooms.Budget{Load: 2 * time.Second, Frame: 2 * time.Second, Input: 2 * time.Second}
}

// TestHardwareRoomNativeRender uses the embedded production Lua, real display
// list replay and native software renderer. Fixtures are host data, not hardware
// acceptance. Set FES_ROOM_SCREENSHOT_DIR to retain the rendered PNGs for review.
func TestHardwareRoomNativeRender(t *testing.T) {
	index := rooms.NewIndex(rooms.Examples())
	pack, ok := index.Find(hardwareRoomID)
	if !ok || !pack.Valid() {
		t.Fatal("embedded hardware room is missing")
	}
	pkg, expansion := strings.Repeat("a", 64), strings.Repeat("b", 64)
	snapshot := hostclient.HardwareSnapshot{
		Machines: []hostclient.HardwareMachine{{
			GameID: "fpga-zx81", Title: "My ZX81", CoreID: "fes.zx81", PackageID: pkg,
			PackageReady: true, FirmwareReady: true, Ready: true,
			Socket:           hostclient.HardwareSocket{ID: "rear", Label: "Rear expansion socket", Supported: true},
			DraftExpansionID: expansion,
			Choices: []hostclient.HardwareExpansion{
				{ExpansionID: expansion, Label: "16K RAM pack", Description: "Adds 16K of memory for larger ZX81 programs. Its sealed package fits this machine's rear connector.", Ready: true},
				{ExpansionID: strings.Repeat("c", 64), Label: "Sound expansion", Description: "A fixture card with host-authored feature copy. The room makes no historical compatibility claim.", Ready: true},
				{ExpansionID: strings.Repeat("d", 64), Label: "Expansion unavailable", Description: "The host has retained this selection for inspection.", UnavailableReason: "Install its matching package before fitting it."},
			},
		}},
		Session: &hostclient.SessionResult{State: "idle"},
	}
	for _, size := range []struct{ w, h int }{{1280, 720}, {1024, 600}} {
		for _, scene := range []string{"idle", "running", "no-artwork", "preview", "overlay"} {
			t.Run(fmt.Sprintf("%s-%dx%d", scene, size.w, size.h), func(t *testing.T) {
				data := snapshot
				selectedPack := pack
				if scene == "running" || scene == "overlay" {
					data.Session = &hostclient.SessionResult{
						ID: "fixture-session", Target: "dev", State: "active", GameID: "fpga-zx81",
						CorePackage: &hostclient.SessionCorePackage{
							PackageID: pkg, Generation: 9, Composition: &hostclient.SessionComposition{PackageID: pkg},
							ABI:              hostclient.SessionCoreABI{ID: "fes.simple-computer", Major: 1},
							ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: "fes.media.blob", Major: 1}},
						},
					}
				}
				if scene == "no-artwork" {
					selectedPack.FS = hardwareMissingArtwork{pack.FS}
				}
				app := NewApp(nil, size.w, size.h, 20)
				app.session = *data.Session
				app.roomDuringPlay = scene == "running" || scene == "overlay"
				if scene == "overlay" {
					// The kit session plane with launcher focus over a live machine.
					display, err := gfx.NewMenuDisplayWithPresenter(&countingSessionMenu{})
					if err != nil {
						t.Fatal(err)
					}
					defer display.Close()
					app.menuDisplay, app.sessionDisplay = display, protocol.SessionDisplayFocused
				}
				w, h := app.roomContentSizeLocked(hardwareRoomID)
				inst, err := rooms.New(selectedPack, rooms.Options{Width: w, Height: h, Services: hardwareRenderServices{snapshot: data}, Index: index, Budget: hardwareRenderTestBudget()})
				if err != nil {
					t.Fatal(err)
				}
				defer inst.Close()
				app.room = inst
				if err := inst.Load(); err != nil {
					t.Fatal(err)
				}
				waitFor(t, app, "hardware shelf", func(s Snapshot) bool {
					if s.Room.Err != "" {
						t.Fatalf("room failed: %s", s.Room.Err)
					}
					var shelf, tools bool
					for _, op := range s.Room.Frame.Ops {
						if op.Kind != rooms.OpText {
							continue
						}
						if op.Text == "16K RAM pack" {
							shelf = true
						}
						if op.Text == "Import expansion" {
							tools = true
						}
					}
					return shelf && tools
				})
				// Async image load is independent of the host read. Give its result
				// a frame to arrive, while the no-artwork case proves the fallback.
				if scene != "no-artwork" {
					deadline := time.Now().Add(time.Second)
					for len(inst.Images()) == 0 && time.Now().Before(deadline) {
						time.Sleep(time.Millisecond)
						app.Tick(time.Now())
					}
				}
				if scene == "preview" {
					// Inspect an unsaved card. The saved card stays marked
					// while the outlined preview appears beside it.
					app.mu.Lock()
					inst.Hover("card:2")
					inst.Input("select")
					app.mu.Unlock()
					waitFor(t, app, "preview", func(s Snapshot) bool {
						var saved, preview, caption bool
						for _, op := range s.Room.Frame.Ops {
							if op.Kind != rooms.OpText {
								continue
							}
							switch op.Text {
							case "Next start":
								saved = true
							case "Preview":
								preview = true
							}
							if strings.Contains(op.Text, "(not saved)") {
								caption = true
							}
						}
						return saved && preview && caption
					})
				}
				snap := app.Snapshot()
				assertWorkshopLabelFits(t, snap.Room.Frame, "Import expansion")
				assertWorkshopLabelFits(t, snap.Room.Frame, "Refresh", "Checking...")
				for _, hit := range snap.Room.Frame.Hits {
					if hit.X < 0 || hit.Y < 0 || hit.X+hit.W > float32(snap.Room.Width)+1 || hit.Y+hit.H > float32(snap.Room.Height)+1 {
						t.Fatalf("control %q outside safe room area: %+v", hit.ID, hit)
					}
				}
				dev, err := gfx.NewSoftware(size.w, size.h)
				if err != nil {
					t.Fatal(err)
				}
				presentFrame(dev, snap, map[string]gpuTexture{}, map[string]gpuTexture{}, false)
				assertRoomPlanePixels(t, dev.Snapshot(), snap.Room, scene == "overlay")
				if output := strings.TrimSpace(os.Getenv("FES_ROOM_SCREENSHOT_DIR")); output != "" {
					if err := os.MkdirAll(output, 0o755); err != nil {
						t.Fatal(err)
					}
					name := filepath.Join(output, fmt.Sprintf("hardware-%s-%dx%d.png", scene, size.w, size.h))
					file, err := os.Create(name)
					if err != nil {
						t.Fatal(err)
					}
					err = png.Encode(file, dev.Snapshot())
					closeErr := file.Close()
					if err != nil {
						t.Fatal(err)
					}
					if closeErr != nil {
						t.Fatal(closeErr)
					}
					t.Log(name)
				}
			})
		}
	}
}

// The service changes only after the first async result has reached Lua.
type hardwareRefreshServices struct {
	roomServices
	data  hostclient.HardwareSnapshot
	calls int
}

func (s *hardwareRefreshServices) Hardware(context.Context) (hostclient.HardwareSnapshot, error) {
	s.calls++
	data := s.data
	if s.calls == 1 {
		data.Session = &hostclient.SessionResult{State: "idle"}
	}
	return data, nil
}

func TestHardwareRoomReturnRefreshesProductionLua(t *testing.T) {
	index := rooms.NewIndex(rooms.Examples())
	pack, _ := index.Find(hardwareRoomID)
	current := hardwareBoundSession()
	current.CorePackage.Composition = &hostclient.SessionComposition{PackageID: current.CorePackage.PackageID}
	services := &hardwareRefreshServices{data: hostclient.HardwareSnapshot{
		Machines: []hostclient.HardwareMachine{{GameID: current.GameID, Title: "ZX81", PackageID: current.CorePackage.PackageID, Ready: true, Socket: hostclient.HardwareSocket{ID: "rear", Supported: true}}},
		Session:  &current,
	}}
	app := NewApp(nil, 1280, 720, 20)
	app.roomsIndex = index
	inst, err := rooms.New(pack, rooms.Options{Width: 1280, Height: 668, Services: services, Index: index, Budget: hardwareRenderTestBudget()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inst.Close)
	app.room = inst
	if err := inst.Load(); err != nil {
		t.Fatal(err)
	}
	hasText := func(s Snapshot, label string) bool {
		if s.Room.Err != "" {
			t.Fatalf("room failed: %s", s.Room.Err)
		}
		for _, op := range s.Room.Frame.Ops {
			if op.Kind == rooms.OpText && op.Text == label {
				return true
			}
		}
		return false
	}
	waitFor(t, app, "initial idle Start", func(s Snapshot) bool { return hasText(s, "Start machine") && hasText(s, "Next start: Empty socket") })
	app.mu.Lock()
	app.applySessionLocked(current)
	app.rememberPlayingSetupLocked("fes.zx81")
	app.openPlayingHardwareRoomLocked()
	app.mu.Unlock()
	waitFor(t, app, "fresh running Stop", func(s Snapshot) bool { return hasText(s, "Stop machine") && !hasText(s, "Start machine") })
	if services.calls < 2 {
		t.Fatal("return reused pre-launch hardware data")
	}
}

// assertRoomPlanePixels checks what the kit plane receives. Every pixel must
// be premultiplied (r,g,b <= a), or the FPGA blend adds light to the machine.
// An opaque room covers the machine everywhere. An overlay stores the scrim
// premultiplied, leaves the machine card area clear of surfaces, and draws
// its controls as solid surfaces.
func assertRoomPlanePixels(t *testing.T, img *image.RGBA, room RoomSnapshot, overlay bool) {
	t.Helper()
	var opaque, translucent int
	for i := 0; i < len(img.Pix); i += 4 {
		r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
		if r > a || g > a || b > a {
			t.Fatalf("pixel %d is not premultiplied: %d,%d,%d,%d", i/4, r, g, b, a)
		}
		if a == 255 {
			opaque++
		} else {
			translucent++
		}
	}
	total := opaque + translucent
	if !overlay {
		if translucent != 0 {
			t.Fatalf("opaque room left %d translucent pixels", translucent)
		}
		return
	}
	if translucent < total/2 || opaque < total/20 {
		t.Fatalf("overlay coverage: %d translucent, %d opaque of %d", translucent, opaque, total)
	}
	// The workshop's 1120x630 layout, scaled into the room content box.
	scale := min(float64(room.Width)/1120, float64(room.Height)/630)
	at := func(x, y float64) color.RGBA {
		px := float64(room.OffsetX) + (float64(room.Width)-1120*scale)/2 + x*scale
		py := float64(room.OffsetY) + (float64(room.Height)-630*scale)/2 + y*scale
		return img.RGBAAt(int(px), int(py))
	}
	scrim := color.RGBA{15, 19, 18, 200} // #141918 at alpha 200, premultiplied
	if got := img.RGBAAt(2, 2); got != scrim {
		t.Fatalf("margin pixel %+v, want premultiplied scrim %+v", got, scrim)
	}
	if got := at(100, 300); got != scrim {
		t.Fatalf("machine card area %+v, want the scrim over the machine", got)
	}
	// A shelf card's fill sits directly on the scrim.
	if got := at(240, 538); got.A != 255 {
		t.Fatalf("shelf card surface %+v is not solid", got)
	}
}
