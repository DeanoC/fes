package tenfoot

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/ui/gfx"
)

func TestPreparedNamesStayDisplayOnlyAcrossLibraryDetailsAndSession(t *testing.T) {
	game := availableGame("coleco-hil-1120", "Coleco HIL 1120 123509", "fpga")
	game.CoreID, game.PackageID, game.MediaID = "fes.coleco", strings.Repeat("a", 64), strings.Repeat("b", 64)
	game.MediaRole, game.ROMMediaID = "blob", strings.Repeat("c", 64)
	game.SlotExpansions = []hostclient.SlotExpansion{{Slot: 1, ExpansionID: strings.Repeat("d", 64), Ready: true}}
	custom := game
	custom.ID, custom.CoreID, custom.Title = "custom", "fes.zx81", "My ZX81 HIL790"
	ordinary := game
	ordinary.ID, ordinary.System = "ordinary", "coleco"
	app := NewApp(nil, 1280, 720, 20)
	app.games = []hostclient.Game{game, custom, ordinary}
	app.grid.SetCount(3)
	app.detailOpen = true
	app.session = hostclient.SessionResult{ID: "same-session", State: "active", GameID: game.ID, CorePackage: &hostclient.SessionCorePackage{PackageID: game.PackageID, Generation: 7}}
	beforeSession := app.session
	snap := app.Snapshot()
	if snap.FocusDetail.Title != "ColecoVision" || snap.DetailPlay.Label != "ColecoVision" || snap.Session.Title != "ColecoVision" {
		t.Fatalf("generated title leaked across a display surface: game=%q detail=%q destination=%q session=%q", snap.Games[0].Title, snap.FocusDetail.Title, snap.DetailPlay.Label, snap.Session.Title)
	}
	if snap.Games[1].Title != custom.Title || snap.Games[2].Title != ordinary.Title {
		t.Fatal("custom/game titles were rewritten")
	}
	// Visible painting applies the label without scanning/copying the catalog
	// for every Snapshot. The expected mask proves the actual shelf title.
	recorder := gfx.NewRecorder()
	drawCoverCells(recorder, snap, map[string]gpuTexture{}, map[string]gpuTexture{}, map[string]struct{}{})
	expected, _, _ := gfx.UIText("ColecoVision", 20, gfx.WeightBold, 0)
	painted := false
	for _, call := range recorder.Calls {
		if call.Op == "DrawMask" && call.Mask != nil && expected != nil && call.Mask.W == expected.W && call.Mask.H == expected.H && bytes.Equal(call.Mask.Pix, expected.Pix) {
			painted = true
		}
	}
	if !painted {
		t.Fatal("canonical prepared title was not painted")
	}
	projected := snap.Games[0]
	projected.Title = game.Title
	if !reflect.DeepEqual(projected, game) || !reflect.DeepEqual(app.games[0], game) || !reflect.DeepEqual(app.session, beforeSession) {
		t.Fatal("display names changed stored metadata, selected media or active session identity")
	}
}

func TestAdoptedLocalMachineUsesCanonicalNameAndKeepsClaim(t *testing.T) {
	app := NewApp(nil, 1280, 720, 20)
	status := localcores.RunStatus{CoreID: "fes.pong", PackageID: strings.Repeat("a", 64), Generation: "exact-generation"}
	app.applyStartupClaimLocked(status, localPhaseRunning)
	if app.localTitle != "Pong" || app.localPackageID != status.PackageID || app.localLeaseGeneration != status.Generation {
		t.Fatalf("adopted label changed claim identity: %q %q %q", app.localTitle, app.localPackageID, app.localLeaseGeneration)
	}
}
