package tenfoot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/zx81tapes"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestInstalledCoreSetupKeepsUsableSiblingPackages(t *testing.T) {
	bad, good := strings.Repeat("a", 64), strings.Repeat("b", 64)
	for _, mode := range []string{"bad sibling", "all failed", "inventory failed", "cancelled", "connection failed"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var goodReads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/v1/library/core-entries":
					if mode == "inventory failed" {
						http.Error(w, "offline", http.StatusServiceUnavailable)
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"entries": []any{}})
				case "/api/v1/core-packages":
					json.NewEncoder(w).Encode(map[string]any{"packages": []any{
						map[string]any{"package_id": bad, "descriptor": map[string]any{"core": map[string]string{"id": "fes.pong", "name": "Pong", "version": "1.0"}}},
						map[string]any{"package_id": good, "descriptor": map[string]any{"core": map[string]string{"id": "fes.pong", "name": "Pong", "version": "2.0"}}},
					}})
				case "/api/v1/core-packages/" + bad, "/api/v1/core-packages/" + good:
					pkg := strings.TrimPrefix(r.URL.Path, "/api/v1/core-packages/")
					if pkg == good {
						goodReads.Add(1)
					} else if mode == "cancelled" {
						cancel()
						<-r.Context().Done()
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"package_id": pkg, "descriptor": map[string]any{
						"core": map[string]string{"id": "fes.pong"}, "abi": map[string]any{"id": "fes.simple-game", "major": 1},
					}})
				case "/api/v1/core-packages/" + bad + "/media-capabilities", "/api/v1/core-packages/" + good + "/media-capabilities":
					if strings.Contains(r.URL.Path, bad) || mode == "all failed" {
						w.WriteHeader(http.StatusInternalServerError)
						w.Write([]byte(`{"error":{"code":"BROKEN_PACKAGE","message":"private /srv/library/package path"}}`))
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"package_id": good, "media": []any{}})
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.Error(w, "unexpected", http.StatusNotFound)
				}
			}))
			defer server.Close()
			httpClient := server.Client()
			if mode == "connection failed" {
				transport := httpClient.Transport
				httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/api/v1/core-packages/"+bad {
						return nil, io.ErrUnexpectedEOF
					}
					return transport.RoundTrip(r)
				})
			}
			a := NewApp(NewClient(server.URL, httpClient), 1280, 720, 10)
			a.ctx = ctx
			a.coreLibrary.Cores = []hostclient.AvailableCore{{Label: "cached"}}
			a.mu.Lock()
			a.openInstalledCoreSetupLocked("fes.pong")
			a.mu.Unlock()
			waitCoreLibrary(t, a)
			a.mu.Lock()
			defer a.mu.Unlock()
			s := &a.coreLibrary
			switch mode {
			case "bad sibling":
				rows := a.coreLibraryRowsLocked()
				if !s.Online || len(s.Cores) != 1 || s.Cores[0].PackageID != good || len(rows) != 3 || rows[2].Core == nil || rows[2].Core.PackageID != good || !strings.Contains(s.Status, "Skipped 1 package") {
					t.Fatalf("healthy package hidden: state %+v rows %+v", s, rows)
				}
			case "all failed":
				if !s.Online || len(s.Cores) != 0 || !strings.Contains(s.Status, "No usable installed") || !strings.Contains(s.Status, "Skipped 2 packages") {
					t.Fatalf("failed packages were reported as absent: %+v", s)
				}
			default:
				if s.Online || len(s.Cores) != 1 || s.Cores[0].Label != "cached" || strings.Contains(s.Status, "Skipped") || goodReads.Load() != 0 {
					t.Fatalf("global failure became a successful partial refresh: %+v good reads=%d", s, goodReads.Load())
				}
			}
			if strings.Contains(s.Status, "private") || strings.Contains(s.Status, "/srv/") {
				t.Fatalf("skipped-package notice exposed error details: %q", s.Status)
			}
		})
	}
}

func TestHardwareCassetteSelectsNextLaunchAndKeepsExpectedBinding(t *testing.T) {
	pkg, expected := strings.Repeat("a", 64), strings.Repeat("b", 64)
	tape := zx81tapes.Entries()[0]
	var mu sync.Mutex
	var selection map[string]string
	imports := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/library/zx81-tapes":
			json.NewEncoder(w).Encode(map[string]any{"tapes": zx81tapes.Entries()})
		case "/api/v1/library/zx81-tapes/" + tape.ID + "/import":
			mu.Lock()
			imports++
			mu.Unlock()
			json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: tape.SHA256, Size: int64(len(tape.Data))})
		case "/api/v1/core-packages/" + pkg + "/media-capabilities":
			json.NewEncoder(w).Encode(map[string]any{"package_id": pkg, "media": []any{map[string]any{
				"role": "cassette", "format": "zx81-p",
				"interface": map[string]any{"id": "fes.media.zx81-tape", "major": 1, "minor": 0},
			}}})
		case "/api/v1/library/core-entries/test-zx81/media":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			selection = body
			mu.Unlock()
			json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "test-zx81", PackageID: pkg, MediaID: body["media_id"], MediaRole: body["media_role"]})
		default:
			t.Errorf("unexpected lifecycle/catalog request: %s", r.URL.Path)
			http.Error(w, "unexpected", 500)
		}
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.ctx = context.Background()
	app.mu.Lock()
	app.openHardwarePickerLocked(rooms.Action{Kind: rooms.ActionHardwareTapes, GameID: "test-zx81", CoreID: "fes.zx81", PackageID: pkg, ExpectedMediaID: expected})
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	app.mu.Lock()
	app.coreLibrary.Index = 2
	app.handleCoreLibraryLocked(CmdSelect)
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	mu.Lock()
	if imports != 1 || selection["expected_package_id"] != pkg || selection["expected_media_id"] != expected || selection["media_id"] != tape.SHA256 || selection["media_role"] != "cassette" {
		t.Fatalf("wrong binding: imports=%d selection=%+v", imports, selection)
	}
	mu.Unlock()
	app.mu.Lock()
	if app.coreLibrary.ExpectedMediaID != tape.SHA256 || !strings.Contains(app.coreLibrary.Status, "RUN") {
		t.Fatalf("missing selection/instructions: %+v", app.coreLibrary)
	}
	app.mu.Unlock()
	app.HandleCommand(CmdBack, time.Now())
	if app.Snapshot().CoreLibrary.Open {
		t.Fatal("Back did not return from cassette shelf")
	}
}

func TestHardwareTapeRoleFollowsDeclaredPackage(t *testing.T) {
	pkg := strings.Repeat("c", 64)
	for _, tc := range []struct {
		name, role, format, iface string
		starter                   bool
	}{
		{name: "legacy blob starter", role: "blob", format: "raw", iface: "fes.media.blob", starter: true},
		{name: "tape unit import", role: "cassette", format: "zx81-p", iface: "fes.media.zx81-tape"},
		{name: "legacy blob import", role: "blob", format: "raw", iface: "fes.media.blob"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var selection map[string]string
			mediaID := strings.Repeat("d", 64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/library/zx81-tapes":
					json.NewEncoder(w).Encode(map[string]any{"tapes": zx81tapes.Entries()})
				case "/api/v1/library/zx81-tapes/" + zx81tapes.Entries()[0].ID + "/import":
					json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: zx81tapes.Entries()[0].SHA256, Size: int64(len(zx81tapes.Entries()[0].Data))})
				case "/api/v1/core-packages/" + pkg + "/media-capabilities":
					json.NewEncoder(w).Encode(map[string]any{"package_id": pkg, "media": []any{map[string]any{
						"role": tc.role, "format": tc.format,
						"interface": map[string]any{"id": tc.iface, "major": 1, "minor": 0},
					}}})
				case "/api/v1/core-media":
					body, _ := io.ReadAll(r.Body)
					json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: mediaID, Size: int64(len(body))})
				case "/api/v1/library/core-entries/test-zx81/media":
					var body map[string]string
					json.NewDecoder(r.Body).Decode(&body)
					mu.Lock()
					selection = body
					mu.Unlock()
					json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "test-zx81", PackageID: pkg, MediaID: body["media_id"], MediaRole: body["media_role"]})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", 500)
				}
			}))
			defer server.Close()
			app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
			app.ctx = context.Background()
			app.mu.Lock()
			app.openHardwarePickerLocked(rooms.Action{Kind: rooms.ActionHardwareTapes, GameID: "test-zx81", CoreID: "fes.zx81", PackageID: pkg, ExpectedMediaID: ""})
			app.mu.Unlock()
			waitCoreLibrary(t, app)
			if tc.starter {
				app.mu.Lock()
				app.coreLibrary.Index = 2
				app.handleCoreLibraryLocked(CmdSelect)
				app.mu.Unlock()
			} else {
				path := filepath.Join(t.TempDir(), "maze.p")
				if err := os.WriteFile(path, []byte{1, 2, 3, 4}, 0600); err != nil {
					t.Fatal(err)
				}
				app.mu.Lock()
				app.importHardwareFileLocked(path)
				app.mu.Unlock()
			}
			waitCoreLibrary(t, app)
			mu.Lock()
			got := selection
			mu.Unlock()
			wantID := mediaID
			if tc.starter {
				wantID = zx81tapes.Entries()[0].SHA256
			}
			if got["media_role"] != tc.role || got["media_id"] != wantID || got["expected_package_id"] != pkg {
				t.Fatalf("selection = %+v", got)
			}
		})
	}
}

func TestHardwarePickerOmitsZX81StartersForZX80(t *testing.T) {
	var mu sync.Mutex
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/library/zx81-tapes" {
			mu.Lock()
			requests++
			mu.Unlock()
			if r.URL.Query().Get("game_id") != "test-zx80" {
				t.Errorf("game_id = %q", r.URL.Query().Get("game_id"))
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"tapes": []hostclient.HardwareTape{}})
	}))
	defer server.Close()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.ctx = context.Background()
	app.mu.Lock()
	app.openHardwarePickerLocked(rooms.Action{Kind: rooms.ActionHardwareTapes, GameID: "test-zx80", CoreID: "fes.zx80", PackageID: strings.Repeat("a", 64), ExpectedMediaID: ""})
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	app.mu.Lock()
	rows := app.hardwarePickerRowsLocked()
	app.mu.Unlock()
	mu.Lock()
	got := requests
	mu.Unlock()
	if got != 1 {
		t.Fatalf("ZX80 setup tape requests = %d, want the saved-program shelf", got)
	}
	if len(rows) != 2 || rows[0].Kind != "empty-tape" || rows[1].Kind != "import-tape" || rows[1].Name != "Import a .o cassette…" {
		t.Fatalf("ZX80 cassette picker rows = %+v", rows)
	}
}

func TestAdmitHardwareCassette(t *testing.T) {
	if err := admitHardwareCassette("fes.zx80", "dragons-add.o", 645); err != nil {
		t.Fatal(err)
	}
	if err := admitHardwareCassette("fes.zx80", "game.p", 645); err == nil || !strings.Contains(err.Error(), ".o / .O") {
		t.Fatalf("ZX80 .p = %v", err)
	}
	if err := admitHardwareCassette("fes.zx81", "game.p", 645); err != nil {
		t.Fatal(err)
	}
	if err := admitHardwareCassette("fes.zx81", "dragons-add.o", 645); err == nil || !strings.Contains(err.Error(), ".p / .P") {
		t.Fatalf("ZX81 .o = %v", err)
	}
}

func TestImportHardwareCassetteUsesCapturedCoreID(t *testing.T) {
	const driveID = "0123456789abcdef01234567"
	pkg, mediaID := strings.Repeat("e", 64), strings.Repeat("f", 64)
	payload := []byte("raw ZX80 O cassette")
	fileRequest := make(chan struct{}, 1)
	allowFileResponse := make(chan struct{})
	var releaseFileOnce sync.Once
	releaseFileResponse := func() { releaseFileOnce.Do(func() { close(allowFileResponse) }) }
	var mu sync.Mutex
	var uploadCount int
	var uploaded string
	var selection map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/storage/drives/" + driveID + "/file":
			fileRequest <- struct{}{}
			<-allowFileResponse
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
		case "/api/v1/core-media":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read core-media upload: %v", err)
			}
			mu.Lock()
			uploadCount++
			uploaded = string(body)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: mediaID, Size: int64(len(body))})
		case "/api/v1/core-packages/" + pkg + "/media-capabilities":
			json.NewEncoder(w).Encode(map[string]any{"package_id": pkg, "media": []any{map[string]any{
				"role": "cassette", "format": "zx80-o", "extensions": []string{".o"},
				"interface": map[string]any{"id": "fes.media.zx81-tape", "major": 1, "minor": 0},
			}}})
		case "/api/v1/library/core-entries/test-zx80/media":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode cassette selection: %v", err)
			}
			mu.Lock()
			selection = body
			mu.Unlock()
			json.NewEncoder(w).Encode(hostclient.CoreEntry{GameID: "test-zx80", PackageID: pkg, MediaID: body["media_id"], MediaRole: body["media_role"]})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	defer releaseFileResponse()
	app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
	app.ctx = context.Background()
	app.mu.Lock()
	app.coreLibrary.Open = true
	app.coreLibrary.HardwareMode = "tapes"
	app.coreLibrary.HardwareGameID = "test-zx80"
	app.coreLibrary.HardwarePackageID = pkg
	app.coreLibrary.HardwareCoreID = "fes.zx80"
	app.importHardwareFileLocked("host-drive:" + driveID + "/dragons-add.o")
	app.mu.Unlock()

	select {
	case <-fileRequest:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the host storage request")
	}
	app.mu.Lock()
	app.coreLibrary.HardwareCoreID = "fes.zx81"
	app.mu.Unlock()
	releaseFileResponse()
	waitCoreLibrary(t, app)

	mu.Lock()
	defer mu.Unlock()
	if uploadCount != 1 || uploaded != string(payload) {
		t.Fatalf("ZX80 cassette upload count=%d bytes=%q", uploadCount, uploaded)
	}
	if selection["expected_package_id"] != pkg || selection["media_id"] != mediaID || selection["media_role"] != "cassette" {
		t.Fatalf("ZX80 cassette selection = %+v", selection)
	}
}

func TestHardwarePickerShowsSavedTimexPrograms(t *testing.T) {
	mediaID := strings.Repeat("ab", 32)
	saved := hostclient.HardwareTape{
		ID: "saved-" + mediaID, Name: "Saved program", Filename: "saved.p",
		License: "Saved", Controls: `After LOAD "", enter RUN.`,
		SHA256: mediaID, RAMKB: 1, MediaID: mediaID,
	}
	for _, coreID := range []string{"fes.ts1000", "fes.ts1500"} {
		t.Run(coreID, func(t *testing.T) {
			gameID := "test-" + strings.TrimPrefix(coreID, "fes.")
			var mu sync.Mutex
			var gameQuery string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/library/zx81-tapes" {
					mu.Lock()
					gameQuery = r.URL.Query().Get("game_id")
					mu.Unlock()
				}
				json.NewEncoder(w).Encode(map[string]any{"tapes": []hostclient.HardwareTape{saved}})
			}))
			defer server.Close()
			app := NewApp(NewClient(server.URL, server.Client()), 1280, 720, 10)
			app.ctx = context.Background()
			app.mu.Lock()
			app.openHardwarePickerLocked(rooms.Action{Kind: rooms.ActionHardwareTapes, GameID: gameID, CoreID: coreID, PackageID: strings.Repeat("a", 64), ExpectedMediaID: ""})
			app.mu.Unlock()
			waitCoreLibrary(t, app)
			app.mu.Lock()
			rows := app.hardwarePickerRowsLocked()
			app.mu.Unlock()
			mu.Lock()
			gotGame := gameQuery
			mu.Unlock()
			if gotGame != gameID {
				t.Fatalf("game_id = %q, want %s", gotGame, gameID)
			}
			if len(rows) != 3 || rows[2].Kind != "tape:"+saved.ID || rows[2].Name != "Saved program · 1K RAM · Saved" {
				t.Fatalf("Timex cassette picker rows = %+v", rows)
			}
		})
	}
}

func TestHardwarePickerDoesNotOpenDuringPlay(t *testing.T) {
	app := NewApp(NewClient("http://127.0.0.1:1", nil), 1280, 720, 10)
	app.session = hostclient.SessionResult{State: "active"}
	app.roomsIndex = rooms.NewIndex(rooms.Examples())
	app.mu.Lock()
	defer app.mu.Unlock()
	app.openHardwarePickerLocked(rooms.Action{Kind: rooms.ActionHardwareTapes, GameID: "test-zx81", CoreID: "fes.zx81", PackageID: strings.Repeat("a", 64)})
	if app.coreLibrary.Open {
		t.Fatal("opened pre-launch picker during play")
	}
	if !app.playingHardwareRoomAvailableLocked() {
		t.Fatal("host display should retain its live room")
	}
	app.localCores = &fakeLocalCores{}
	if app.playingHardwareRoomAvailableLocked() {
		t.Fatal("idle-only HDMI offered a live hardware room")
	}
	app.openTapePickerLocked()
	if app.tapePickerOpen {
		t.Fatal("idle-only HDMI offered live tape picker")
	}
}
