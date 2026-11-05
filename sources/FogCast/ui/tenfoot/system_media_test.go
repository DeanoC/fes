package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestSystemMediaImportSavesNextStartWithCapturedSelection(t *testing.T) {
	for _, tc := range []struct {
		name, core, role, format, ext string
		size                          int64
		oldHost                       bool
	}{
		{"c64 disk", "fes.c64", "disk", "", ".d64", 174848, false},
		{"Apple II disk", "fes.apple2", "disk", "", ".do", 143360, false},
		{"Atari ST disk", "fes.atari-st", "disk", "", ".st", 737280, false},
		{"PC/XT disk", "fes.pcxt", "disk", "", ".img", 368640, false},
		{"Spectrum tape", "fes.spectrum128", "cassette", "", ".tap", 16, false},
		{"ZX81 tape", "fes.zx81", "cassette", "zx81-p", ".p", 16, false},
		{"ZX80 tape", "fes.zx80", "cassette", "zx80-o", ".o", 16, false},
		{"ZX81 tape on old host", "fes.zx81", "cassette", "zx81-p", ".p", 16, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pkg, prior, next := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
			var selected map[string]string
			var named map[string]string
			nameRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/core-media":
					if r.ContentLength != tc.size {
						t.Errorf("import size %d", r.ContentLength)
					}
					io.Copy(io.Discard, r.Body)
					json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: next, Size: tc.size})
				case "/api/v1/library/core-entries/my-machine/media":
					json.NewDecoder(r.Body).Decode(&selected)
					json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "my-machine", CoreID: tc.core, PackageID: pkg, MediaRole: tc.role, MediaID: next})
				case "/api/v1/library/core-entries/my-machine/media-name":
					nameRequests++
					json.NewDecoder(r.Body).Decode(&named)
					if tc.oldHost {
						http.NotFound(w, r)
						return
					}
					json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "my-machine", PackageID: pkg, MediaID: named["media_id"], MediaName: named["media_name"]})
				case "/api/v1/games":
					io.WriteString(w, `{"games":[]}`)
				default:
					t.Errorf("unexpected lifecycle or API request: %s %s", r.Method, r.URL)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer server.Close()
			a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
			a.ctx = context.Background()
			a.coreLibrary = coreLibraryState{Open: true, Online: true, HardwareMode: "system-media", HardwareGameID: "my-machine", HardwarePackageID: pkg, ExpectedMediaID: prior, PickID: "media:" + tc.role,
				Setup: &hostclient.CoreSetup{Capabilities: protocol.CoreMediaCapabilities{Media: []protocol.CoreMediaCapability{{Role: tc.role, Format: tc.format, MinBytes: tc.size, MaxBytes: tc.size, Extensions: []string{tc.ext}}}}}}
			path := filepath.Join(t.TempDir(), "game"+strings.ToUpper(tc.ext))
			if err := os.WriteFile(path, make([]byte, tc.size), 0600); err != nil {
				t.Fatal(err)
			}
			a.mu.Lock()
			a.importCoreLibraryFileLocked(path)
			a.mu.Unlock()
			waitCoreLibrary(t, a)
			a.mu.Lock()
			defer a.mu.Unlock()
			if selected["expected_package_id"] != pkg || selected["expected_media_id"] != prior || selected["media_id"] != next || selected["media_role"] != tc.role {
				t.Fatalf("selection identity: %+v", selected)
			}
			wantsName := tc.format == "zx81-p" || tc.format == "zx80-o"
			if wantsName {
				if nameRequests != 1 || named["expected_package_id"] != pkg || named["media_id"] != next || named["media_name"] != "game"+strings.ToUpper(tc.ext) {
					t.Fatalf("name selection: requests=%d body=%+v", nameRequests, named)
				}
			} else if nameRequests != 0 {
				t.Fatalf("unexpected media name request: %+v", named)
			}
			if a.coreLibrary.ExpectedMediaID != next || a.coreLibrary.PickID != "" {
				t.Fatalf("save did not finish: %+v", a.coreLibrary)
			}
		})
	}
}

func TestSystemMediaRefusesStaleEntryBeforeOfferingImports(t *testing.T) {
	pkg, prior := strings.Repeat("a", 64), strings.Repeat("b", 64)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/library/core-entries/my-machine" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "my-machine", CoreID: "fes.c64", PackageID: pkg, MediaID: strings.Repeat("c", 64)})
	}))
	defer server.Close()
	a := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	a.ctx = context.Background()
	a.mu.Lock()
	a.openSystemMediaLocked(rooms.Action{GameID: "my-machine", CoreID: "fes.c64", PackageID: pkg, ExpectedMediaID: prior})
	a.mu.Unlock()
	waitCoreLibrary(t, a)
	a.mu.Lock()
	defer a.mu.Unlock()
	if requests != 1 || a.coreLibrary.Setup != nil || !strings.Contains(a.coreLibrary.Status, "changed") {
		t.Fatalf("stale setup offered: requests=%d state=%+v", requests, a.coreLibrary)
	}
}

func TestSetupMediaRowsIncludeDeclaredCassetteAndDisk(t *testing.T) {
	a := NewApp(nil, 1280, 720, 10)
	a.coreLibrary = coreLibraryState{Open: true, InstalledSetup: true, Ref: &hostclient.CoreReference{}, Setup: &hostclient.CoreSetup{Capabilities: protocol.CoreMediaCapabilities{Media: []protocol.CoreMediaCapability{
		{Role: "cassette", Extensions: []string{".tap", ".z80"}},
		{Role: "disk", Extensions: []string{".dsk"}},
	}}}}
	seen := map[string]bool{}
	for _, row := range a.coreLibraryRowsLocked() {
		seen[row.Kind] = true
	}
	if !seen["media:cassette"] || !seen["media:disk"] {
		t.Fatalf("missing media choices: %+v", a.coreLibraryRowsLocked())
	}
	if admitSetupMedia(protocol.CoreMediaCapability{MinBytes: 1, MaxBytes: 32, Extensions: []string{".d64"}}, "tape.tap", 16) {
		t.Fatal("wrong media format offered")
	}
}
