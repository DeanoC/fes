package tenfoot

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
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
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func TestListFirmwarePickerDirMarksExactBIOS(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bios := filepath.Join(dir, "coleco.bios")
	other := filepath.Join(dir, "cart.col")
	hidden := filepath.Join(dir, ".hidden")
	sub := filepath.Join(dir, "usb")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bios, bytes.Repeat([]byte{0x55}, int(protocol.FirmwareBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("too-small"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hidden, bytes.Repeat([]byte{0xaa}, int(protocol.FirmwareBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := listFirmwarePickerDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var biosRow FirmwarePickerRow
	for _, row := range rows {
		names = append(names, row.Name)
		if row.Name == "coleco.bios" {
			biosRow = row
		}
		if row.Name == "cart.col" && row.Selectable {
			t.Fatal("wrong-size file must not be selectable")
		}
		if strings.HasPrefix(row.Name, ".") && row.Kind != firmwarePickerKindParent {
			t.Fatalf("hidden file listed: %+v", row)
		}
	}
	if !biosRow.Selectable || biosRow.Size != protocol.FirmwareBytes {
		t.Fatalf("bios row %+v names=%v", biosRow, names)
	}
	if !strings.Contains(strings.Join(names, ","), "usb/") {
		t.Fatalf("directory missing: %v", names)
	}
}

func TestOpenColecoBIOSFileRejectsWrongSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "short.bin")
	if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openColecoBIOSFile(path); err == nil || !strings.Contains(err.Error(), "8192") {
		t.Fatalf("wrong size err = %v", err)
	}
	if _, _, err := openColecoBIOSFile(dir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory err = %v", err)
	}
}

func TestAppMissingFirmwareOpensPickerAndBecomesReady(t *testing.T) {
	dir := t.TempDir()
	bios := filepath.Join(dir, "coleco.bios")
	payload := bytes.Repeat([]byte{0x55, 0xaa}, int(protocol.FirmwareBytes/2))
	if err := os.WriteFile(bios, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not bios"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newFirmwareHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "frogger catalog", func(s Snapshot) bool {
		return len(s.Games) == 1 && !s.Loading
	})
	app.mu.Lock()
	app.hostSettings.Libraries = []hostclient.LibraryRoot{{ID: "usb", System: "coleco", Root: dir}}
	focusBefore := app.grid.Focus
	app.mu.Unlock()

	now := time.Now()
	app.HandleCommand(CmdSelect, now)
	snap := app.Snapshot()
	if !snap.FirmwarePicker.Open || h.launchCount() != 0 {
		t.Fatalf("picker=%v launches=%d status=%q", snap.FirmwarePicker.Open, h.launchCount(), snap.Status)
	}
	if snap.Grid.Focus != focusBefore {
		t.Fatalf("focus moved %d → %d", focusBefore, snap.Grid.Focus)
	}

	selectLibraryRoot(t, app, dir)
	selectNamedRow(t, app, "readme.txt")
	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if !snap.FirmwarePicker.Open || h.importCount() != 0 {
		t.Fatalf("wrong-size imported picker=%v imports=%d status=%q", snap.FirmwarePicker.Open, h.importCount(), snap.FirmwarePicker.Status)
	}
	if !strings.Contains(snap.FirmwarePicker.Status, "8192") {
		t.Fatalf("wrong-size status %q", snap.FirmwarePicker.Status)
	}

	selectNamedRow(t, app, "coleco.bios")
	app.HandleCommand(CmdSelect, time.Now())
	snap = waitFor(t, app, "bios imported", func(s Snapshot) bool {
		return !s.FirmwarePicker.Open && !s.Loading && len(s.Games) == 1 && s.Games[0].FirmwareReady
	})
	if h.importCount() != 1 || h.selectCount() != 1 || h.launchCount() != 0 {
		t.Fatalf("import=%d select=%d launches=%d", h.importCount(), h.selectCount(), h.launchCount())
	}
	if snap.Games[0].LaunchBlock() != "" {
		t.Fatalf("ready catalog %+v status=%q", snap.Games[0], snap.Status)
	}

	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "launch after BIOS", func(s Snapshot) bool { return s.Launch.Phase == "ok" })
	if h.launchCount() != 1 {
		t.Fatalf("launches %d", h.launchCount())
	}
}

func TestRoomConfirmImportBIOSKeepsLocation(t *testing.T) {
	dir := t.TempDir()
	bios := filepath.Join(dir, "coleco.bios")
	payload := bytes.Repeat([]byte{0x55, 0xaa}, int(protocol.FirmwareBytes/2))
	if err := os.WriteFile(bios, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	h := newFirmwareHost(t)
	index := rooms.NewIndex([]rooms.Pack{
		testRoomPack(t, "coleco", `
function load()
  library.query({ q = "Frogger" }, function(games, err)
    destination.set{ kind = "game", label = "Frogger", query = "Frogger", platform = "fpga", matches = games }
  end)
end
function draw() gfx.rect(0,0,10,10,'#fff') end
`),
	})
	app := newRoomApp(t, h.asRoomHost(), index, true)
	app.mu.Lock()
	app.hostSettings.Libraries = []hostclient.LibraryRoot{{ID: "usb", System: "coleco", Root: dir}}
	app.mu.Unlock()
	now := time.Now()
	waitFor(t, app, "picker", func(s Snapshot) bool { return s.RoomPicker.Open })
	focusHomeRow(t, app, HomeKindRoom, "coleco")
	app.HandleCommand(CmdSelect, now)
	snap := waitFor(t, app, "frogger unavailable", func(s Snapshot) bool {
		return s.Room.Open && s.Room.Destination.Confirm() == rooms.ConfirmImportFirmware
	})
	label := snap.Room.Destination.Label
	if snap.Room.Destination.Action != "Import Coleco BIOS." {
		t.Fatalf("action %q", snap.Room.Destination.Action)
	}

	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if !snap.FirmwarePicker.Open || !snap.Room.Open {
		t.Fatalf("picker overlay %+v room=%v", snap.FirmwarePicker, snap.Room.Open)
	}

	app.HandleCommand(CmdBack, now)
	snap = app.Snapshot()
	if snap.FirmwarePicker.Open || !snap.Room.Open || snap.Room.Destination.Label != label {
		t.Fatalf("back stole focus picker=%v dest=%+v", snap.FirmwarePicker.Open, snap.Room.Destination)
	}

	app.HandleCommand(CmdSelect, now)
	selectLibraryRoot(t, app, dir)
	selectNamedRow(t, app, "coleco.bios")
	app.HandleCommand(CmdSelect, now)
	snap = waitFor(t, app, "frogger ready", func(s Snapshot) bool {
		return s.Room.Open && !s.FirmwarePicker.Open && s.Room.Destination.Availability == rooms.AvailReady
	})
	if snap.Room.Destination.Action != "Play" || snap.Room.Destination.Label != label {
		t.Fatalf("ready dest %+v", snap.Room.Destination)
	}
	if h.launchCount() != 0 {
		t.Fatalf("import launched: %d", h.launchCount())
	}
}

type firmwareHost struct {
	mu       sync.Mutex
	launches []string
	imports  int
	selects  int
	ready    bool
	session  string
	server   *httptest.Server
}

func newFirmwareHost(t *testing.T) *firmwareHost {
	t.Helper()
	h := &firmwareHost{session: `{"state":"idle"}`}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{h.frogger()}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games/fpga-frogger":
			_ = json.NewEncoder(w).Encode(h.frogger())
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []hostclient.Platform{{ID: "fpga", Label: "FPGA"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []hostclient.Collection{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/edition-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"preferences": []hostclient.EditionPreference{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			h.mu.Lock()
			body := h.session
			h.mu.Unlock()
			_, _ = io.WriteString(w, body)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/core-media":
			body, _ := io.ReadAll(r.Body)
			if int64(len(body)) != protocol.FirmwareBytes {
				http.Error(w, `{"error":{"code":"BAD_REQUEST","message":"size"}}`, http.StatusBadRequest)
				return
			}
			h.mu.Lock()
			h.imports++
			h.mu.Unlock()
			id := fmt.Sprintf("%x", sha256.Sum256(body))
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: id, Size: int64(len(body))})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/library/firmware":
			var req struct {
				Slot    string `json:"slot"`
				MediaID string `json:"media_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Slot != protocol.FirmwareRole || req.MediaID == "" {
				http.Error(w, `{"error":{"code":"BAD_REQUEST","message":"slot"}}`, http.StatusBadRequest)
				return
			}
			h.mu.Lock()
			h.selects++
			h.ready = true
			h.mu.Unlock()
			_ = json.NewEncoder(w).Encode(hostclient.CoreFirmware{Slot: protocol.FirmwareRole, MediaID: req.MediaID, Size: protocol.FirmwareBytes})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			var body struct {
				GameID string `json:"game_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			h.mu.Lock()
			ready := h.ready
			if ready {
				h.launches = append(h.launches, body.GameID)
				h.session = `{"state":"active","game_id":"` + body.GameID + `"}`
			}
			session := h.session
			h.mu.Unlock()
			if !ready {
				http.Error(w, `{"error":{"code":"BAD_REQUEST","message":"firmware"}}`, http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, session)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.server.Close)
	return h
}

func (h *firmwareHost) frogger() hostclient.Game {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := hostclient.Game{ID: "fpga-frogger", Title: "Frogger", System: "fpga", State: "available", RootOnline: true, Launchable: true, FirmwareRequired: true}
	g.FirmwareReady = h.ready
	return g
}

func (h *firmwareHost) launchCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.launches)
}

func (h *firmwareHost) importCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.imports
}

func (h *firmwareHost) selectCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.selects
}

func (h *firmwareHost) asRoomHost() *roomHost {
	return &roomHost{server: h.server, launches: h.launches, prefs: map[string]hostclient.EditionPreference{}}
}

func selectLibraryRoot(t *testing.T, app *App, dir string) {
	t.Helper()
	now := time.Now()
	for i := 0; i < 12; i++ {
		snap := app.Snapshot()
		if !snap.FirmwarePicker.Open || len(snap.FirmwarePicker.Rows) == 0 {
			t.Fatalf("picker closed while choosing root: %+v", snap.FirmwarePicker)
		}
		row := snap.FirmwarePicker.Rows[snap.FirmwarePicker.Index]
		if row.Kind == firmwarePickerKindRoot && row.Path == dir {
			app.HandleCommand(CmdSelect, now)
			return
		}
		app.HandleCommand(CmdDown, now)
	}
	t.Fatalf("library root %s not listed: %+v", dir, app.Snapshot().FirmwarePicker.Rows)
}

func selectNamedRow(t *testing.T, app *App, name string) {
	t.Helper()
	now := time.Now()
	for i := 0; i < 16; i++ {
		snap := app.Snapshot()
		if !snap.FirmwarePicker.Open || len(snap.FirmwarePicker.Rows) == 0 {
			t.Fatalf("picker closed while choosing %s", name)
		}
		row := snap.FirmwarePicker.Rows[snap.FirmwarePicker.Index]
		if row.Name == name {
			return
		}
		app.HandleCommand(CmdDown, now)
	}
	t.Fatalf("row %s not listed: %+v", name, app.Snapshot().FirmwarePicker.Rows)
}
