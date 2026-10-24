package tenfoot

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

func TestCurrentMediaLabel(t *testing.T) {
	iface := protocol.C64DiskInterface()
	pkg := &hostclient.SessionCorePackage{PackageID: "package", Generation: 9, ABI: hostclient.SessionCoreABI{ID: "fes.computer", Major: 1}, ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: iface.ID, Major: 1}}, MediaUnits: []protocol.MediaUnitStatus{{Unit: 0, Interface: iface, MinBytes: 174848, MaxBytes: 174848, ChunkBytes: 512, State: protocol.MediaUnitReady}}}
	session := hostclient.SessionResult{CorePackage: pkg, MediaName: "disk two.d64"}
	if got := currentMediaLabel(session); got != "Inserted: disk two.d64" {
		t.Fatal(got)
	}
	pkg.MediaUnits[0].State = protocol.MediaUnitEmpty
	if got := currentMediaLabel(session); got != "No media inserted" {
		t.Fatal(got)
	}
	pkg.MediaUnits[0].State = protocol.MediaUnitLoading
	if got := currentMediaLabel(session); got != "Media loading…" {
		t.Fatal(got)
	}
	session.MediaName = ""
	pkg.MediaUnits[0].State = protocol.MediaUnitReady
	if got := currentMediaLabel(session); got != "Inserted media name unavailable" {
		t.Fatal(got)
	}
}

func TestUncertainMediaInsertClearsDisplayedName(t *testing.T) {
	prior := hostclient.SessionResult{ID: "session", Target: "dev", State: "active", CorePackage: &hostclient.SessionCorePackage{PackageID: "package", Generation: 9}, MediaName: "old.d64"}
	app := &App{session: prior, tapePickerGen: 1, tapePickerOpen: true}
	app.finishTapeArm(1, prior, hostclient.SessionResult{}, true, errors.New("reply lost"))
	if app.session.MediaName != "" || app.tapePickerStatus != "reply lost" || !app.tapePickerOpen {
		t.Fatalf("name=%q status=%q open=%v", app.session.MediaName, app.tapePickerStatus, app.tapePickerOpen)
	}
}

func TestPreflightMediaInsertErrorKeepsDisplayedName(t *testing.T) {
	prior := hostclient.SessionResult{ID: "session", Target: "dev", State: "active", CorePackage: &hostclient.SessionCorePackage{PackageID: "package", Generation: 9}, MediaName: "old.d64"}
	app := &App{session: prior, tapePickerGen: 1, tapePickerOpen: true}
	_, dispatched, err := importAndArmTape(context.Background(), &Client{}, prior, filepath.Join(t.TempDir(), "missing.d64"))
	if err == nil || dispatched {
		t.Fatalf("preflight result dispatched=%v error=%v", dispatched, err)
	}
	app.finishTapeArm(1, prior, hostclient.SessionResult{}, dispatched, err)
	if app.session.MediaName != "old.d64" || app.tapePickerStatus == "" || !app.tapePickerOpen {
		t.Fatalf("name=%q status=%q open=%v", app.session.MediaName, app.tapePickerStatus, app.tapePickerOpen)
	}
}
