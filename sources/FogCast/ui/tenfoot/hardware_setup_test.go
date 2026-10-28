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
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/zx81tapes"
	"github.com/DeanoC/FogCast/ui/rooms"
)

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
