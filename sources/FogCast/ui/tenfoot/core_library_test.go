package tenfoot

import (
	"context"
	"encoding/json"
	"github.com/DeanoC/FogCast/hostclient"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func waitCoreLibrary(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		busy := a.coreLibrary.Busy
		a.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("library request stalled")
}
func TestCoreLibraryInstallAndOfflineBrowse(t *testing.T) {
	installed := false
	offline := false
	posts := 0
	pid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if offline {
			http.Error(w, "offline", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/core-catalog":
			state := "available"
			if installed {
				state = "installed"
			}
			json.NewEncoder(w).Encode(map[string]any{"cores": []any{map[string]any{"source_id": "pub", "library_source_id": "library", "core_id": "fes.sms", "package_id": pid, "label": "SMS", "standing": "supported", "artifact_state": state}}})
		case "/api/v1/core-catalog/install":
			posts++
			installed = true
			json.NewEncoder(w).Encode(map[string]string{"package_id": pid})
		case "/api/v1/core-catalog/fes.sms/setup":
			json.NewEncoder(w).Encode(map[string]any{"source_id": "pub", "library_source_id": "library", "core_id": "fes.sms", "package_id": pid, "roms": []any{map[string]any{"id": "cart", "role": "cartridge", "source_size": 32768, "binding": "entry"}}})
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.mu.Lock()
	a.openCoreLibraryLocked()
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	a.mu.Lock()
	if !a.coreLibrary.Online || len(a.coreLibrary.Cores) != 1 {
		t.Fatal("catalog missing")
	}
	a.coreLibrary.Index = 1
	a.handleCoreLibraryLocked(CmdSelect)
	a.handleCoreLibraryLocked(CmdSelect)
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	a.mu.Lock()
	if !installed || posts != 1 || a.coreLibrary.Setup == nil {
		t.Fatal("install/setup failed")
	}
	a.mu.Unlock()
	a.HandleCommand(CmdBack, time.Now())
	a.HandleCommand(CmdDown, time.Now())
	a.HandleCommand(CmdSelect, time.Now())
	waitCoreLibrary(t, a)
	a.mu.Lock()
	if a.coreLibrary.Setup == nil {
		t.Fatal("installed system asks for a redundant install")
	}
	a.mu.Unlock()
	offline = true
	a.mu.Lock()
	a.refreshCoreLibraryLocked()
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coreLibrary.Online || len(a.coreLibrary.Cores) != 1 {
		t.Fatal("offline browse lost")
	}
	a.handleCoreLibraryLocked(CmdSelect)
	if posts != 1 {
		t.Fatal("offline write")
	}
}

func TestCoreLibraryVideoInventoryErrorBlocksInstall(t *testing.T) {
	posts := 0
	pid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/core-catalog":
			json.NewEncoder(w).Encode(map[string]any{"cores": []any{map[string]any{
				"source_id": "pub", "library_source_id": "library", "core_id": "fes.coleco",
				"package_id": pid, "label": "Coleco", "standing": "supported", "artifact_state": "available",
				"video_inventory_error": "video part inventory is unavailable",
			}}})
		case "/api/v1/core-catalog/install":
			posts++
			http.Error(w, "inventory", 500)
		default:
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.mu.Lock()
	a.openCoreLibraryLocked()
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.coreLibrary.Cores) != 1 || a.coreLibrary.Cores[0].VideoInventoryError == "" {
		t.Fatalf("inventory error dropped: %+v", a.coreLibrary.Cores)
	}
	rows := a.coreLibraryRowsLocked()
	if len(rows) < 2 || !strings.Contains(rows[1].Name, "video part inventory is unavailable") {
		t.Fatalf("row label %q", rows[1].Name)
	}
	a.coreLibrary.Index = 1
	a.handleCoreLibraryLocked(CmdSelect)
	if a.coreLibrary.Ref != nil || posts != 0 || a.coreLibrary.Status != "video part inventory is unavailable" {
		t.Fatalf("install offered ref=%v posts=%d status=%q", a.coreLibrary.Ref, posts, a.coreLibrary.Status)
	}
}

func TestCoreLibraryROMCreationKeepsSourceAndDoesNotLaunch(t *testing.T) {
	const pid = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const mid = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var mu sync.Mutex
	var created hostclient.CoreSetupRequest
	imports := 0
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/v1/core-media":
			imports++
			if r.ContentLength != 16384 {
				t.Errorf("bad size %d", r.ContentLength)
			}
			io.Copy(io.Discard, r.Body)
			json.NewEncoder(w).Encode(map[string]any{"media_id": mid, "size": 16384})
		case "/api/v1/core-catalog/entries":
			creates++
			json.NewDecoder(r.Body).Decode(&created)
			json.NewEncoder(w).Encode(map[string]any{"source_id": "library", "publication_source_id": "pub", "entry": map[string]any{"game_id": "synthetic-sg", "core_id": "fes.sg1000", "package_id": pid, "title": "Synthetic"}})
		case "/api/v1/games":
			json.NewEncoder(w).Encode(map[string]any{"games": []any{}})
		default:
			t.Errorf("unexpected/lifecycle request %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	ref := hostclient.CoreReference{LibrarySourceID: "library", SourceID: "pub", CoreID: "fes.sg1000", PackageID: pid}
	a.coreLibrary = coreLibraryState{Open: true, Online: true, Ref: &ref, Setup: &hostclient.CoreSetup{CoreReference: ref, ROMs: []hostclient.SetupROM{{ID: "cart", Role: "cartridge", SourceSize: 16384, Binding: "entry"}}}, ROMs: map[string]string{}, Title: "Synthetic", PickID: "rom:cart"}
	dir := t.TempDir()
	small := filepath.Join(dir, "small.bin")
	cart := filepath.Join(dir, "cart.sg")
	os.WriteFile(small, make([]byte, 16), 0600)
	os.WriteFile(cart, make([]byte, 16384), 0600)
	a.mu.Lock()
	a.importCoreLibraryFileLocked(small)
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	mu.Lock()
	if imports != 0 {
		t.Fatal("wrong size uploaded")
	}
	mu.Unlock()
	a.mu.Lock()
	a.importCoreLibraryFileLocked(cart)
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	a.mu.Lock()
	if a.coreLibrary.ROMs["cart"] != mid {
		t.Fatal("ROM not selected")
	}
	a.createCoreLibraryGameLocked()
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 || created.LibrarySourceID != "library" || created.SourceID != "pub" || created.ROMs["cart"] != mid {
		t.Fatalf("lost source/media identity: %+v", created)
	}
}

func TestCoreLibraryBIOSNeedsExplicitHouseholdSelection(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	ref := hostclient.CoreReference{}
	a.coreLibrary = coreLibraryState{Open: true, Online: true, Ref: &ref, Setup: &hostclient.CoreSetup{ROMs: []hostclient.SetupROM{{ID: "bios", Role: "firmware", SourceSize: 8192, Binding: "household-firmware"}}}, ROMs: map[string]string{"bios": "chosen"}, Title: "Test"}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.createCoreLibraryGameLocked()
	if a.coreLibrary.Busy || !strings.Contains(a.coreLibrary.Status, "Explicitly") {
		t.Fatal("BIOS silently changed")
	}
	rows := a.coreLibraryRowsLocked()
	if len(rows) < 2 || rows[1].Kind != "firmware:bios" {
		t.Fatal("no explicit BIOS action")
	}
}

func TestCoreLibraryNavigationOwnsInputAndPointer(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	a.coreLibrary = coreLibraryState{Open: true, Online: true, Cores: []hostclient.AvailableCore{{Label: "SMS"}}}
	a.HandleCommand(CmdDown, time.Now())
	if a.Snapshot().CoreLibrary.Index != 1 {
		t.Fatal("pad did not navigate systems")
	}
	snap := a.Snapshot()
	overlay := snap
	overlay.FirmwarePicker = snap.CoreLibrary
	panel, ok := firmwarePickerPanel(overlay)
	if !ok {
		t.Fatal("overlay not visible")
	}
	hit := HitTest(snap, panel.X+24, panel.rowY(0)+8)
	if hit.Kind != PointerCoreLibrary || hit.Index != 0 {
		t.Fatalf("pointer escaped overlay: %+v", hit)
	}
	a.mu.Lock()
	a.settingsOSKKind = settingsOSKCoreTitle
	a.settingsOSKField.Buffer = "Named"
	a.mu.Unlock()
	a.HandleCommand(CmdTab, time.Now())
	if a.Snapshot().OSK.Open || a.coreLibrary.Title != "Named" {
		t.Fatal("pad OSK did not submit title")
	}
	a.HandleCommand(CmdBack, time.Now())
	if a.Snapshot().CoreLibrary.Open {
		t.Fatal("back did not dismiss systems")
	}
}

func TestCoreLibraryPendingRequestAllowsCancel(t *testing.T) {
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done(); close(cancelled) }))
	defer server.Close()
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.mu.Lock()
	a.openCoreLibraryLocked()
	a.mu.Unlock()
	<-entered
	a.HandleCommand(CmdSelect, time.Now())
	if !a.Snapshot().CoreLibrary.Busy {
		t.Fatal("select interrupted pending request")
	}
	a.HandleCommand(CmdBack, time.Now())
	if a.Snapshot().CoreLibrary.Open {
		t.Fatal("Back did not dismiss pending overlay")
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("request context not cancelled")
	}
}

func TestCoreLibraryRowsCarryCatalogIdentity(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	a.coreLibrary.Cores = []hostclient.AvailableCore{{CoreReference: hostclient.CoreReference{CoreID: "fes.sms", PackageID: "a"}, Label: "same"}, {CoreReference: hostclient.CoreReference{CoreID: "fes.coleco", PackageID: "b"}, Label: "same"}}
	rows := a.coreLibraryRowsLocked()
	if rows[1].Core == nil || rows[2].Core == nil || rows[1].Core.CoreID != "fes.sms" || rows[2].Core.CoreID != "fes.coleco" {
		t.Fatalf("row identities lost: %#v", rows)
	}
}

func TestCoreLibraryRequiresStartupBlob(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	setup := &hostclient.CoreSetup{}
	if err := json.Unmarshal([]byte(`{"abi":{"id":"fes.application","major":1,"minor":0},"interfaces":[{"id":"fes.media.blob","major":1,"minor":0,"required":true}]}`), &setup.Descriptor); err != nil {
		t.Fatal(err)
	}
	a.coreLibrary = coreLibraryState{Open: true, Online: true, Title: "requires blob", Ref: &hostclient.CoreReference{}, Setup: setup}
	a.createCoreLibraryGameLocked()
	if a.coreLibrary.Busy || a.coreLibrary.Status != "Choose required blob media." {
		t.Fatalf("required blob not blocked: %#v", a.coreLibrary)
	}
}

func TestSystemsSettingsHint(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	for i, row := range a.settingsRowsLocked() {
		if row.ID == "systems" {
			a.settingsIndex = i
			if hint := a.settingsHintLocked(); !strings.Contains(hint, "browse systems") {
				t.Fatalf("hint = %q", hint)
			}
			return
		}
	}
	t.Fatal("systems settings row missing")
}
