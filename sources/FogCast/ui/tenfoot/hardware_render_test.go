package tenfoot

import (
	"context"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
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

func (f hardwareMissingArtwork) Open(name string) (fs.File, error) {
	if strings.HasPrefix(name, "assets/") {
		return nil, fs.ErrNotExist
	}
	return f.FS.Open(name)
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
		for _, scene := range []string{"idle", "running", "no-artwork"} {
			t.Run(fmt.Sprintf("%s-%dx%d", scene, size.w, size.h), func(t *testing.T) {
				data := snapshot
				selectedPack := pack
				if scene == "running" {
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
				app.roomDuringPlay = scene == "running"
				w, h := app.roomContentSizeLocked(hardwareRoomID)
				inst, err := rooms.New(selectedPack, rooms.Options{Width: w, Height: h, Services: hardwareRenderServices{snapshot: data}, Index: index})
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
					for _, op := range s.Room.Frame.Ops {
						if op.Kind == rooms.OpText && op.Text == "16K RAM pack" {
							return true
						}
					}
					return false
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
				snap := app.Snapshot()
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
	inst, err := rooms.New(pack, rooms.Options{Width: 1280, Height: 668, Services: services, Index: index})
	if err != nil {
		t.Fatal(err)
	}
	app.room = inst
	if err := inst.Load(); err != nil {
		t.Fatal(err)
	}
	hasText := func(s Snapshot, label string) bool {
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
	app.openPlayingHardwareRoomLocked()
	app.mu.Unlock()
	waitFor(t, app, "fresh running Stop", func(s Snapshot) bool { return hasText(s, "Stop machine") && !hasText(s, "Start machine") })
	if services.calls < 2 {
		t.Fatal("return reused pre-launch hardware data")
	}
}
