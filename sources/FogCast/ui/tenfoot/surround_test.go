package tenfoot

import (
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func surroundPixels(t *testing.T, picture image.Rectangle) *image.RGBA {
	t.Helper()
	dev, err := gfx.NewSoftware(menudisplay.Width, menudisplay.Height)
	if err != nil {
		t.Fatal(err)
	}
	snap := Snapshot{Surround: SurroundSnapshot{Active: true, Picture: picture}}
	snap.Session.Title, snap.Session.System, snap.Session.HardwareRoom = "Mazogs", "ZX81", true
	labels := map[string]gpuTexture{}
	drawSurround(dev, snap, labels)
	destroyTextures(dev, labels)
	return dev.Framebuffer()
}

func rgba(img *image.RGBA, x, y int) [4]uint8 {
	i := img.PixOffset(x, y)
	return [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
}

// The plane blends premultiplied pixels: alpha 0 leaves the machine pixel
// exactly, so the picture must be fully clear and the cabinet opaque.
func TestSurroundCutsTheMachinePictureOutOfAnOpaqueCabinet(t *testing.T) {
	picture := image.Rect(312, 90, 968, 630)
	img := surroundPixels(t, picture)
	for _, p := range []image.Point{{320 + surroundShade, 98 + surroundShade}, {640, 360}, {967 - surroundShade, 629 - surroundShade}} {
		if got := rgba(img, p.X, p.Y); got != [4]uint8{} {
			t.Fatalf("picture pixel %v = %v", p, got)
		}
	}
	// The tube edge darkens the machine picture: premultiplied black.
	if got := rgba(img, 640, picture.Min.Y); got[0] != 0 || got[1] != 0 || got[2] != 0 || got[3] != 112 {
		t.Fatalf("edge shade = %v", got)
	}
	if got := rgba(img, 640, picture.Min.Y+surroundShade); got != [4]uint8{} {
		t.Fatalf("shade reached past its band: %v", got)
	}
	if got := rgba(img, picture.Min.X-surroundBezel/2, 360); got != [4]uint8{8, 8, 10, 255} {
		t.Fatalf("bezel = %v", got)
	}
	for _, p := range []image.Point{{4, 4}, {1275, 715}, {picture.Min.X - surroundBezel, picture.Min.Y - surroundBezel}} {
		if got := rgba(img, p.X, p.Y); got[3] != 255 {
			t.Fatalf("cabinet pixel %v = %v", p, got)
		}
	}
}

// Before the runtime reports where the picture is, or when it fills the
// frame, the surround is fully transparent and the machine shows unchanged.
func TestSurroundWithoutAPictureLeavesTheMachineUncovered(t *testing.T) {
	for _, picture := range []image.Rectangle{{}, image.Rect(0, 0, menudisplay.Width, menudisplay.Height)} {
		img := surroundPixels(t, picture)
		for i := 0; i < len(img.Pix); i += 4 {
			if img.Pix[i]|img.Pix[i+1]|img.Pix[i+2]|img.Pix[i+3] != 0 {
				t.Fatalf("picture %v left a covered pixel at byte %d", picture, i)
			}
		}
	}
}

type revisionPainter struct {
	*gfx.Software
	revisions []uint64
}

func (p *revisionPainter) PresentRevision(revision uint64) {
	p.revisions = append(p.revisions, revision)
}

// A static surround is one complete scene, so FrameCache reuses it instead of
// rasterising the cabinet on every 30 Hz tick.
func TestUnchangedSurroundIsReusedByTheFrameCache(t *testing.T) {
	software, err := gfx.NewSoftware(menudisplay.Width, menudisplay.Height)
	if err != nil {
		t.Fatal(err)
	}
	painter := &revisionPainter{Software: software}
	cache := gfx.NewFrameCache(painter)
	snap := Snapshot{Surround: SurroundSnapshot{Active: true, Picture: image.Rect(312, 90, 968, 630)}}
	snap.Session.Title, snap.Session.System, snap.Session.HardwareRoom = "Mazogs", "ZX81", true
	labels := map[string]gpuTexture{}
	for range 4 {
		drawSurround(cache, snap, labels)
	}
	r := painter.revisions
	if len(r) != 4 || r[3] != r[2] || r[2] != r[1] {
		t.Fatalf("unchanged surround was rasterised again: revisions %v", r)
	}
}

// A failed Stop keeps its parked retry page instead of the cabinet.
func TestSurroundGivesWayToStopAndItsRetry(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	t.Cleanup(app.Stop)
	display, err := gfx.NewMenuDisplayWithPresenter(&countingSessionMenu{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(display.Close)
	app.SetMenuDisplay(display)
	app.mu.Lock()
	defer app.mu.Unlock()
	app.gpuParked, app.sessionDisplay = true, protocol.SessionDisplayShown
	if !app.surroundShownLocked() {
		t.Fatal("shown plane drew no surround")
	}
	app.stopPhase = "stopping"
	if app.surroundShownLocked() {
		t.Fatal("surround hid the stopping page")
	}
	app.stopPhase, app.retryStopLock = "error", true
	if app.surroundShownLocked() {
		t.Fatal("surround hid the failed Stop's retry page")
	}
}

func TestSurroundSettingTogglesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenfoot.json")
	app := NewApp(nil, 1280, 720, 20)
	t.Cleanup(app.Stop)
	app.SetPrefsPath(path)
	app.SetSurround(true)
	now := time.Now()
	app.HandleCommand(CmdSettings, now)
	focusSettingsRow(t, app, "surround", now)
	if row := app.Snapshot().Settings.Rows[settingsRowSurround]; row.Value != "On" {
		t.Fatalf("row = %#v", row)
	}
	app.HandleCommand(CmdSelect, now)
	if row := app.Snapshot().Settings.Rows[settingsRowSurround]; row.Value != "Off" {
		t.Fatalf("toggled row = %#v", row)
	}
	got, err := loadTenfootPrefs(path)
	if err != nil || got.Surround == nil || *got.Surround {
		t.Fatalf("prefs = %#v err=%v", got, err)
	}
}

const overlayRoomScript = `
function load() end
function draw()
  gfx.clear(room.overlay and {r=0,g=0,b=0,a=160} or "#122738")
  gfx.text("Hardware room", 20, 20, {size=24})
end
`

// The hardware room opened over a running machine on the kit plane sees
// room.overlay and clears translucently; leaving it restores the surround.
func TestKitRoomOverTheMachineIsAnOverlay(t *testing.T) {
	_, app, _ := newSurroundKit(t, computerTapeDisplaySession())
	app.SetRooms(rooms.NewIndex([]rooms.Pack{testRoomPack(t, hardwareRoomID, overlayRoomScript)}), "")
	waitFor(t, app, "surround", func(s Snapshot) bool { return s.Surround.Active })
	app.HandleCommand(CmdHome, time.Now())
	waitFor(t, app, "room overlay", func(s Snapshot) bool {
		return s.Room.DuringPlay && s.Room.Frame.HasClear && s.Room.Frame.Clear == gfx.RGBA(0, 0, 0, 160)
	})
	app.mu.Lock()
	app.sessionDisplayUncertain = true
	uncertain := app.roomOverlayLocked()
	app.sessionDisplayUncertain = false
	app.mu.Unlock()
	if uncertain {
		t.Fatal("an uncertain plane drew the room translucently")
	}
	app.HandleCommand(CmdBack, time.Now())
	waitFor(t, app, "surround after the overlay", func(s Snapshot) bool { return !s.Room.DuringPlay && s.Surround.Active })
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.roomOverlayLocked() {
		t.Fatal("overlay outlived the room visit")
	}
}

// Host windows have no session plane: a room during play stays opaque.
func TestHostRoomDuringPlayIsNeverAnOverlay(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	t.Cleanup(app.Stop)
	app.mu.Lock()
	defer app.mu.Unlock()
	app.session.State, app.roomDuringPlay, app.sessionDisplay = "active", true, protocol.SessionDisplayFocused
	if app.roomOverlayLocked() {
		t.Fatal("host window drew the room translucently")
	}
}
