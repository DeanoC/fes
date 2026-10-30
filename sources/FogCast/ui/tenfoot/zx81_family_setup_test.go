package tenfoot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestZX81SetupRoomPicksAVariantThenItsPackages(t *testing.T) {
	zx80 := strings.Repeat("a", 64)
	zx81 := strings.Repeat("b", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/library/core-entries":
			json.NewEncoder(w).Encode(map[string]any{"entries": []any{}})
		case "/api/v1/core-packages":
			json.NewEncoder(w).Encode(map[string]any{"packages": []any{
				map[string]any{"package_id": zx80, "descriptor": map[string]any{"core": map[string]any{"id": "fes.zx80", "name": "ZX80", "version": "0.1.0"}}},
				map[string]any{"package_id": zx81, "descriptor": map[string]any{"core": map[string]any{"id": "fes.zx81", "name": "Zx81", "version": "1.6.0"}}},
			}})
		case "/api/v1/core-packages/" + zx80:
			json.NewEncoder(w).Encode(map[string]any{"package_id": zx80, "descriptor": map[string]any{
				"core": map[string]string{"id": "fes.zx80", "name": "ZX80", "version": "0.1.0"},
				"abi":  map[string]any{"id": "fes.computer", "major": 1},
				"rom":  map[string]any{"id": "zx80-rom", "role": "firmware", "source_size": 4096},
			}})
		case "/api/v1/core-packages/" + zx80 + "/media-capabilities":
			json.NewEncoder(w).Encode(map[string]any{"package_id": zx80, "media": []any{}})
		case "/api/v1/core-catalog":
			json.NewEncoder(w).Encode(map[string]any{"cores": []any{map[string]any{
				"source_id": "pub", "core_id": "fes.zx80", "package_id": zx80,
				"label": "Sinclair ZX80", "standing": "experimental", "artifact_state": "available",
			}}})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.mu.Lock()
	a.openInstalledCoreSetupLocked("")
	snap := a.coreLibrarySnapshotLocked()
	if snap.Title != "ZX81 family" || len(snap.Rows) != 4 {
		t.Fatalf("variant list %+v", snap)
	}
	for _, row := range snap.Rows {
		if !strings.HasPrefix(row.Kind, "variant:") || strings.Contains(row.Kind, "published") {
			t.Fatalf("variant row %+v", row)
		}
	}
	if snap.Rows[1].Name != "Sinclair ZX80 · PAL · 4 KiB BASIC · 1 KiB RAM" || snap.Rows[1].Kind != "variant:fes.zx80" {
		t.Fatalf("zx80 row %+v", snap.Rows[1])
	}
	a.coreLibrary.Index = 1
	a.handleCoreLibraryLocked(CmdSelect)
	a.mu.Unlock()
	waitCoreLibrary(t, a)

	a.mu.Lock()
	if a.coreLibrary.FilterCoreID != "fes.zx80" {
		t.Fatalf("filter %q", a.coreLibrary.FilterCoreID)
	}
	snap = a.coreLibrarySnapshotLocked()
	if snap.Title != "Sinclair ZX80" {
		t.Fatalf("package title %q", snap.Title)
	}
	var sawPackage, sawOther bool
	for _, row := range snap.Rows {
		if strings.Contains(row.Name, "Sinclair ZX80 0.1.0") {
			sawPackage = true
		}
		if strings.Contains(row.Name, "1.6.0") || strings.Contains(row.Name, "Timex") {
			sawOther = true
		}
	}
	if !sawPackage || sawOther || len(snap.Rows) != 3 {
		t.Fatalf("package rows %+v", snap.Rows)
	}
	a.coreLibrary.Index = 2
	a.handleCoreLibraryLocked(CmdSelect)
	a.mu.Unlock()
	waitCoreLibrary(t, a)

	a.mu.Lock()
	if a.coreLibrary.Ref == nil || a.coreLibrary.FilterCoreID != "fes.zx80" || a.coreLibrary.Setup == nil {
		t.Fatalf("rom step filter %q ref %v setup %v", a.coreLibrary.FilterCoreID, a.coreLibrary.Ref, a.coreLibrary.Setup)
	}
	a.handleCoreLibraryLocked(CmdBack)
	if a.coreLibrary.Ref != nil || a.coreLibrary.FilterCoreID != "fes.zx80" || a.coreLibrarySnapshotLocked().Title != "Sinclair ZX80" {
		t.Fatalf("back from ROM lost the package list: %q %v", a.coreLibrary.FilterCoreID, a.coreLibrary.Ref)
	}
	a.handleCoreLibraryLocked(CmdBack)
	snap = a.coreLibrarySnapshotLocked()
	if a.coreLibrary.FilterCoreID != "" || snap.Title != "ZX81 family" || len(snap.Rows) != 4 || !a.coreLibrary.Open {
		t.Fatalf("back from packages %+v filter %q", snap, a.coreLibrary.FilterCoreID)
	}
	a.handleCoreLibraryLocked(CmdBack)
	if a.coreLibrary.Open {
		t.Fatal("back from the variant list stayed open")
	}

	a.openInstalledCoreSetupLocked("")
	a.coreLibrary.Index = 1
	a.handleCoreLibraryLocked(CmdSelect)
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	a.mu.Lock()
	browse := -1
	for i, row := range a.coreLibraryRowsLocked() {
		if row.Kind == "published" {
			browse = i
		}
	}
	if browse < 0 {
		t.Fatal("browse missing")
	}
	a.coreLibrary.Index = browse
	a.handleCoreLibraryLocked(CmdSelect)
	if a.coreLibrary.FilterCoreID != "fes.zx80" || a.coreLibrary.InstalledSetup {
		t.Fatalf("browse reset the variant: filter %q installed %v", a.coreLibrary.FilterCoreID, a.coreLibrary.InstalledSetup)
	}
	a.mu.Unlock()
	waitCoreLibrary(t, a)
}

func TestInstalledSystemSetupStartsWithNeutralStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request while opening the system picker: %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer server.Close()
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()

	a.mu.Lock()
	a.openInstalledCoreSetupLocked("")
	snap := a.coreLibrarySnapshotLocked()
	a.mu.Unlock()
	if snap.Status != "Preparing system setup…" || strings.Contains(snap.Status, "ZX81") {
		t.Fatalf("system setup started with a core-specific status: %q", snap.Status)
	}
}
