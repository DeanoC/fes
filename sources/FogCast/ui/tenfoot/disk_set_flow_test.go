package tenfoot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
)

// setHost is a host library holding one C64 title, enough for choosing a
// set, adding a disk and reading the title back.
type setHost struct {
	mu    sync.Mutex
	entry hostclient.CoreEntry
	sets  []map[string]any
}

func (h *setHost) serve(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		base := "/api/v1/library/core-entries/" + h.entry.GameID
		switch {
		case r.URL.Path == "/api/v1/core-media":
			body, _ := io.ReadAll(r.Body)
			sum := sha256.Sum256(body)
			json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: hex.EncodeToString(sum[:]), Size: int64(len(body))})
		case r.URL.Path == base && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(h.entry)
		case r.URL.Path == base+"/media":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			h.entry.MediaID, h.entry.MediaRole, h.entry.MediaName, h.entry.MoreMedia = req["media_id"], req["media_role"], "", nil
			json.NewEncoder(w).Encode(h.entry)
		case r.URL.Path == base+"/media-name":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			h.entry.MediaName = req["media_name"]
			json.NewEncoder(w).Encode(h.entry)
		case r.URL.Path == base+"/media-set":
			var req struct {
				Expected []string              `json:"expected_more_media"`
				More     []hostclient.SetMedia `json:"more_media"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if !reflect.DeepEqual(req.Expected, setIDs(h.entry.MoreMedia)) {
				http.Error(w, `{"error":{"code":"STALE_REVISION"}}`, http.StatusConflict)
				return
			}
			h.sets = append(h.sets, map[string]any{"expected": req.Expected, "more": req.More})
			h.entry.MoreMedia = req.More
			if len(h.entry.MoreMedia) == 0 {
				h.entry.MoreMedia = nil
			}
			json.NewEncoder(w).Encode(h.entry)
		case r.URL.Path == "/api/v1/games":
			io.WriteString(w, `{"games":[]}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.Error(w, "unexpected", 500)
		}
	}))
}

func d64(t *testing.T, dir, name string, fill byte) (string, string) {
	t.Helper()
	data := make([]byte, protocol.C64DiskBytes)
	data[0] = fill
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return path, hex.EncodeToString(sum[:])
}

func TestChoosingOneDiskOfASetKeepsTheWholeSetThenAddDiskExtendsIt(t *testing.T) {
	pkg, prior := strings.Repeat("a", 64), strings.Repeat("b", 64)
	host := &setHost{entry: hostclient.CoreEntry{GameID: "copper", CoreID: "fes.c64", PackageID: pkg, MediaRole: "disk", MediaID: prior}}
	server := host.serve(t)
	defer server.Close()
	dir := t.TempDir()
	p2, id2 := d64(t, dir, "Copper Canyon (Disk 2 of 2).d64", 2)
	_, id1 := d64(t, dir, "Copper Canyon (Disk 1 of 2).d64", 1)
	extra, id3 := d64(t, dir, "Copper Canyon Extras.d64", 3)
	capability := protocol.CoreMediaCapability{Role: "disk", MinBytes: protocol.C64DiskBytes, MaxBytes: protocol.C64DiskBytes, Extensions: []string{".d64"}}
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.coreLibrary = coreLibraryState{Open: true, Online: true, HardwareMode: "system-media", HardwareGameID: "copper", HardwarePackageID: pkg, ExpectedMediaID: prior, PickID: "media:disk",
		Setup: &hostclient.CoreSetup{Capabilities: protocol.CoreMediaCapabilities{Media: []protocol.CoreMediaCapability{capability}}}}
	a.coreLibraryDirLocked(dir)
	// Picking disk 2 still makes disk 1 the first disk.
	a.mu.Lock()
	a.importCoreLibraryFileLocked(p2)
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	host.mu.Lock()
	got := host.entry
	host.mu.Unlock()
	want := []hostclient.SetMedia{{MediaID: id2, MediaName: "Copper Canyon (Disk 2 of 2).d64"}}
	if got.MediaID != id1 || got.MediaName != "Copper Canyon (Disk 1 of 2).d64" || !reflect.DeepEqual(got.MoreMedia, want) {
		t.Fatalf("set kept as %+v", got)
	}
	// Add disk puts a further file after the last disk. The picker is left
	// as opening it leaves it once the package read is done.
	a.mu.Lock()
	a.coreLibrary.AddDisk, a.coreLibrary.AddDiskRole, a.coreLibrary.ExpectedMediaID = true, "disk", id1
	a.coreLibrary.PickID = ""
	rows := a.coreLibraryRowsLocked()
	if len(rows) != 1 || rows[0].Kind != "media:disk" || a.coreLibrary.AddDiskRole != "disk" {
		a.mu.Unlock()
		t.Fatalf("add disk rows %+v status %q", rows, a.coreLibrary.Status)
	}
	a.coreLibrary.PickID = "media:disk"
	a.importCoreLibraryFileLocked(extra)
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	host.mu.Lock()
	defer host.mu.Unlock()
	want = append(want, hostclient.SetMedia{MediaID: id3, MediaName: "Copper Canyon Extras.d64"})
	if host.entry.MediaID != id1 || !reflect.DeepEqual(host.entry.MoreMedia, want) {
		t.Fatalf("after add disk %+v (status %q)", host.entry, a.coreLibrary.Status)
	}
}

// Next disk sends the running machine the chosen set disk through the live
// media change, by its media ID and file name, and takes the reply.
func TestNextDiskPutsTheSetDiskInTheRunningMachine(t *testing.T) {
	h := newTapeHost(t)
	h.computerTape = true
	h.mediaName = "CANYON1.P"
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "running", func(s Snapshot) bool { return s.Session.State == "active" })
	disk2 := strings.Repeat("d", 64)
	app.mu.Lock()
	app.changeSessionDiskLocked(disk2, "CANYON2.P")
	app.mu.Unlock()
	waitFor(t, app, "disk changed", func(Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return !app.diskChangeBusy && app.session.MediaName == "CANYON2.P"
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.replaces != 1 || h.lastMediaID != disk2 || h.lastName != "CANYON2.P" {
		t.Fatalf("live change replaces=%d id=%s name=%q", h.replaces, h.lastMediaID, h.lastName)
	}
}

// In the running machine's media picker, a set disk's row puts that disk in
// by its media ID and file name, as the kit's in-play route offers it.
func TestMediaPickerSetDiskRowPutsTheDiskIn(t *testing.T) {
	h := newTapeHost(t)
	h.computerTape = true
	h.mediaName = "CANYON1.P"
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "running", func(s Snapshot) bool { return s.Session.State == "active" })
	disk2 := strings.Repeat("d", 64)
	app.mu.Lock()
	app.tapePickerOpen, app.tapePickerSession = true, app.session
	app.startSetDiskArmLocked(TapePickerRow{Kind: tapePickerKindSetDisk, SetMediaID: disk2, SetMediaName: "CANYON2.P"})
	app.mu.Unlock()
	waitFor(t, app, "disk in", func(Snapshot) bool {
		app.mu.Lock()
		defer app.mu.Unlock()
		return !app.tapePickerBusy && app.session.MediaName == "CANYON2.P"
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.replaces != 1 || h.lastMediaID != disk2 || h.lastName != "CANYON2.P" {
		t.Fatalf("live change replaces=%d id=%s name=%q", h.replaces, h.lastMediaID, h.lastName)
	}
}
