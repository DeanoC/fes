package tenfoot

import (
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/ui/gfx"
	"github.com/DeanoC/FogCast/ui/menudisplay"
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
