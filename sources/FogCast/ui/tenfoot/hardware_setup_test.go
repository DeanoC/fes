package tenfoot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	app.openHardwarePickerLocked(rooms.Action{Kind: rooms.ActionHardwareTapes, GameID: "test-zx81", PackageID: pkg, ExpectedMediaID: expected})
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	app.mu.Lock()
	app.coreLibrary.Index = 2
	app.handleCoreLibraryLocked(CmdSelect)
	app.mu.Unlock()
	waitCoreLibrary(t, app)
	mu.Lock()
	if imports != 1 || selection["expected_package_id"] != pkg || selection["expected_media_id"] != expected || selection["media_id"] != tape.SHA256 || selection["media_role"] != "blob" {
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

func TestHardwarePickerDoesNotOpenDuringPlay(t *testing.T) {
	app := NewApp(NewClient("http://127.0.0.1:1", nil), 1280, 720, 10)
	app.session = hostclient.SessionResult{State: "active"}
	app.roomsIndex = rooms.NewIndex(rooms.Examples())
	app.mu.Lock()
	defer app.mu.Unlock()
	app.openHardwarePickerLocked(rooms.Action{Kind: rooms.ActionHardwareTapes, GameID: "test-zx81", PackageID: strings.Repeat("a", 64)})
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
