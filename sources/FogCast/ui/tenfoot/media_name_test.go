package tenfoot

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	legacy := hostclient.SessionResult{
		CorePackage:  &hostclient.SessionCorePackage{ABI: hostclient.SessionCoreABI{ID: "fes.simple-computer", Major: 1}},
		MediaEjected: true,
	}
	if got := currentMediaLabel(legacy); got != "No media inserted" {
		t.Fatalf("confirmed legacy eject label = %q", got)
	}
	legacy.MediaEjected = false
	if got := currentMediaLabel(legacy); got != "Inserted media name unavailable" {
		t.Fatalf("unknown legacy media label = %q", got)
	}
}

func TestUncertainMediaInsertClearsDisplayedName(t *testing.T) {
	prior := hostclient.SessionResult{ID: "session", Target: "dev", State: "active", CorePackage: &hostclient.SessionCorePackage{PackageID: "package", Generation: 9}, MediaName: "old.d64", MediaEjected: true}
	app := &App{session: prior, tapePickerGen: 1, tapePickerOpen: true}
	app.finishTapeArm(1, prior, hostclient.SessionResult{}, true, errors.New("reply lost"))
	if app.session.MediaName != "" || app.session.MediaEjected || app.tapePickerStatus != "reply lost" || !app.tapePickerOpen {
		t.Fatalf("name=%q ejected=%v status=%q open=%v", app.session.MediaName, app.session.MediaEjected, app.tapePickerStatus, app.tapePickerOpen)
	}
}

func TestPreflightMediaInsertErrorKeepsDisplayedName(t *testing.T) {
	prior := hostclient.SessionResult{ID: "session", Target: "dev", State: "active", CorePackage: &hostclient.SessionCorePackage{PackageID: "package", Generation: 9}, MediaName: "old.d64"}
	app := &App{session: prior, tapePickerGen: 1, tapePickerOpen: true}
	_, dispatched, err := importAndArmTape(context.Background(), &Client{}, prior, filepath.Join(t.TempDir(), "missing.d64"), nil)
	if err == nil || dispatched {
		t.Fatalf("preflight result dispatched=%v error=%v", dispatched, err)
	}
	app.finishTapeArm(1, prior, hostclient.SessionResult{}, dispatched, err)
	if app.session.MediaName != "old.d64" || app.tapePickerStatus == "" || !app.tapePickerOpen {
		t.Fatalf("name=%q status=%q open=%v", app.session.MediaName, app.tapePickerStatus, app.tapePickerOpen)
	}
}

func TestNoChangeMediaInsertKeepsLegacyEjectedMarker(t *testing.T) {
	prior := hostclient.SessionResult{
		ID: "session", Target: "dev", State: "active", MediaEjected: true,
		CorePackage: &hostclient.SessionCorePackage{PackageID: "package", Generation: 9},
	}
	app := &App{session: prior, tapePickerGen: 1, tapePickerOpen: true}
	noChange := &protocol.APIError{Code: protocol.CodeBusy, Message: "rejected before mutation", NoChange: true}
	app.finishTapeArm(1, prior, hostclient.SessionResult{}, true, noChange)
	if !app.session.MediaEjected || app.session.MediaName != "" {
		t.Fatalf("no-change rejection changed media state: name=%q ejected=%v", app.session.MediaName, app.session.MediaEjected)
	}
}

func TestUncertainMediaEjectClearsLegacyEjectedMarker(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/session/live-media/clear" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"MISTER_UNAVAILABLE","message":"eject reply lost","phase":"input"}}`))
	}))
	defer server.Close()

	prior := hostclient.SessionResult{
		ID: "session", Target: "dev", State: "active", MediaEjected: true,
		CorePackage: &hostclient.SessionCorePackage{
			PackageID: strings.Repeat("a", 64), Generation: 9,
			ABI:              hostclient.SessionCoreABI{ID: "fes.simple-computer", Major: 1},
			ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: "fes.media.blob", Major: 1}},
		},
	}
	app := &App{
		client: NewClient(server.URL, server.Client()), session: prior,
		tapePickerGen: 1, tapePickerOpen: true,
	}
	app.doTapeEject(context.Background(), 1, prior)
	if attempts != 3 {
		t.Fatalf("clear attempts = %d, want 3", attempts)
	}
	if app.session.MediaName != "" || app.session.MediaEjected {
		t.Fatalf("uncertain eject retained local media state: name=%q ejected=%v", app.session.MediaName, app.session.MediaEjected)
	}
	if got := currentMediaLabel(app.session); got != "Inserted media name unavailable" {
		t.Fatalf("uncertain eject label = %q", got)
	}
}
