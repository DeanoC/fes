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
)

func TestListTapePickerDirMarksPFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tape := filepath.Join(dir, "maze.p")
	other := filepath.Join(dir, "maze.tzx")
	hidden := filepath.Join(dir, ".secret.p")
	sub := filepath.Join(dir, "usb")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tape, []byte("tape-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("not-p"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hidden, []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(dir, "huge.p")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte{1}, int(protocol.MaxDevelopmentMediaBytes)+1), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := listTapePickerDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var tapeRow TapePickerRow
	for _, row := range rows {
		switch row.Name {
		case "maze.p":
			tapeRow = row
		case "maze.tzx":
			if row.Selectable {
				t.Fatal("tzx must not be selectable")
			}
		case "huge.p":
			if row.Selectable {
				t.Fatal("oversized .p must not be selectable")
			}
		}
		if strings.HasPrefix(row.Name, ".") && row.Kind != tapePickerKindParent {
			t.Fatalf("hidden file listed: %+v", row)
		}
	}
	if !tapeRow.Selectable || tapeRow.Size != 10 {
		t.Fatalf("tape row %+v", tapeRow)
	}
}

func TestOpenTapeFileRejectsWrongClass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "maze.tzx")
	if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := openTapeFile(path); err == nil || !strings.Contains(err.Error(), ".p") {
		t.Fatalf("tzx err = %v", err)
	}
	if _, _, _, err := openTapeFile(dir); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory err = %v", err)
	}
}

func TestAppActiveZX81OpensTapePickerAndArms(t *testing.T) {
	dir := t.TempDir()
	tape := filepath.Join(dir, "maze.p")
	payload := []byte("zx81-program")
	if err := os.WriteFile(tape, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not tape"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newTapeHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool {
		return len(s.Games) == 1 && !s.Loading
	})
	app.mu.Lock()
	app.hostSettings.Libraries = []hostclient.LibraryRoot{{ID: "usb", System: "zx81", Root: dir}}
	app.mu.Unlock()

	now := time.Now()
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "zx81 active", func(s Snapshot) bool {
		return s.Session.State == "active" && s.Session.LoadTape
	})
	snap := app.Snapshot()
	if !strings.Contains(snap.HeaderHint(), "load tape") {
		t.Fatalf("chrome hint %q", snap.HeaderHint())
	}

	app.HandleCommand(CmdSearch, now)
	snap = app.Snapshot()
	if !snap.TapePicker.Open || h.replaceCount() != 0 {
		t.Fatalf("picker=%v replaces=%d status=%q", snap.TapePicker.Open, h.replaceCount(), snap.Status)
	}

	selectTapeLibraryRoot(t, app, dir)
	selectTapeNamedRow(t, app, "readme.txt")
	app.HandleCommand(CmdSelect, now)
	snap = app.Snapshot()
	if !snap.TapePicker.Open || h.importCount() != 0 {
		t.Fatalf("wrong file imported picker=%v imports=%d status=%q", snap.TapePicker.Open, h.importCount(), snap.TapePicker.Status)
	}
	if !strings.Contains(snap.TapePicker.Status, ".p") {
		t.Fatalf("wrong-class status %q", snap.TapePicker.Status)
	}

	selectTapeNamedRow(t, app, "maze.p")
	app.HandleCommand(CmdSelect, time.Now())
	snap = waitFor(t, app, "tape armed", func(s Snapshot) bool {
		return !s.TapePicker.Open && strings.Contains(s.Status, "Tape armed")
	})
	if h.importCount() != 1 || h.replaceCount() != 1 || h.clearCount() != 0 {
		t.Fatalf("import=%d replace=%d clear=%d", h.importCount(), h.replaceCount(), h.clearCount())
	}
	if h.lastName != "maze.p" || h.lastMediaID == "" {
		t.Fatalf("armed name=%q media=%q", h.lastName, h.lastMediaID)
	}
	wantID := fmt.Sprintf("%x", sha256.Sum256(payload))
	if h.lastMediaID != wantID {
		t.Fatalf("media id %s want %s", h.lastMediaID, wantID)
	}
	_ = snap
}

func TestAppTapePickerSurfacesBusyReject(t *testing.T) {
	dir := t.TempDir()
	tape := filepath.Join(dir, "maze.p")
	if err := os.WriteFile(tape, []byte("busy-tape"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newTapeHost(t)
	h.busy = true
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.mu.Lock()
	app.hostSettings.Libraries = []hostclient.LibraryRoot{{ID: "usb", System: "zx81", Root: dir}}
	app.mu.Unlock()

	now := time.Now()
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "active", func(s Snapshot) bool { return s.Session.State == "active" && s.Session.LoadTape })
	app.HandleCommand(CmdSearch, now)
	selectTapeLibraryRoot(t, app, dir)
	selectTapeNamedRow(t, app, "maze.p")
	app.HandleCommand(CmdSelect, time.Now())
	snap := waitFor(t, app, "busy status", func(s Snapshot) bool {
		return s.TapePicker.Open && !s.TapePicker.Busy && strings.Contains(s.TapePicker.Status, "tape loader is busy")
	})
	if h.replaceCount() != 1 {
		t.Fatalf("replace calls %d", h.replaceCount())
	}
	if snap.Session.State != "active" {
		t.Fatalf("busy must not stop session: %+v", snap.Session)
	}
}

func TestAppTapePickerEjectClearsMailbox(t *testing.T) {
	h := newTapeHost(t)
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })

	now := time.Now()
	app.HandleCommand(CmdSelect, now)
	waitFor(t, app, "active", func(s Snapshot) bool { return s.Session.State == "active" && s.Session.LoadTape })
	app.HandleCommand(CmdSearch, now)
	selectTapeNamedRow(t, app, tapePickerEjectLabel)
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "ejected", func(s Snapshot) bool {
		return !s.TapePicker.Open && strings.Contains(s.Status, "Tape ejected")
	})
	if h.clearCount() != 1 || h.replaceCount() != 0 {
		t.Fatalf("clear=%d replace=%d", h.clearCount(), h.replaceCount())
	}
}

type tapeHost struct {
	mu           sync.Mutex
	launches     int
	imports      int
	replaces     int
	clears       int
	busy         bool
	lastMediaID  string
	lastName     string
	pkg          string
	sessionID    string
	server       *httptest.Server
}

func newTapeHost(t *testing.T) *tapeHost {
	t.Helper()
	h := &tapeHost{
		pkg:       strings.Repeat("a", 64),
		sessionID: "host-zx81",
	}
	h.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games":
			_ = json.NewEncoder(w).Encode(map[string]any{"games": []hostclient.Game{h.zx81()}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/games/fpga-zx81":
			_ = json.NewEncoder(w).Encode(h.zx81())
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platforms":
			_ = json.NewEncoder(w).Encode(map[string]any{"platforms": []hostclient.Platform{{ID: "fpga", Label: "FPGA"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/collections":
			_ = json.NewEncoder(w).Encode(map[string]any{"collections": []hostclient.Collection{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/library/edition-preferences":
			_ = json.NewEncoder(w).Encode(map[string]any{"preferences": []hostclient.EditionPreference{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/session":
			_, _ = io.WriteString(w, h.sessionJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/launch":
			h.mu.Lock()
			h.launches++
			h.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, h.sessionJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/core-media":
			body, _ := io.ReadAll(r.Body)
			h.mu.Lock()
			h.imports++
			h.mu.Unlock()
			id := fmt.Sprintf("%x", sha256.Sum256(body))
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(hostclient.CoreMedia{MediaID: id, Size: int64(len(body))})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media":
			if r.Header.Get("X-FogCast-Session-ID") != h.sessionID ||
				r.Header.Get("X-FogCast-Package-ID") != h.pkg ||
				r.Header.Get("X-FogCast-Core-Generation") != "9" ||
				r.Header.Get("X-FogCast-Target") != "dev" {
				http.Error(w, `{"error":{"code":"BUSY","message":"identity"}}`, http.StatusConflict)
				return
			}
			var req protocol.LiveMediaRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			h.mu.Lock()
			h.replaces++
			h.lastMediaID = req.MediaID
			h.lastName = req.Name
			busy := h.busy
			h.mu.Unlock()
			if busy {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]string{"code": "BUSY", "message": "tape loader is busy; retry after LOAD finishes"},
				})
				return
			}
			_, _ = io.WriteString(w, h.sessionJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media/clear":
			h.mu.Lock()
			h.clears++
			h.mu.Unlock()
			_, _ = io.WriteString(w, h.sessionJSON())
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.server.Close)
	return h
}

func (h *tapeHost) sessionJSON() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.launches == 0 {
		return `{"id":"` + h.sessionID + `","target":"dev","state":"idle"}`
	}
	return fmt.Sprintf(`{"id":%q,"target":"dev","state":"active","game_id":"fpga-zx81","execution":"fpga_native","core_package":{"package_id":%q,"generation":9,"abi":{"id":"fes.simple-computer","major":1,"minor":0},"active_interfaces":[{"id":"fes.media.blob","major":1,"minor":0},{"id":"fes.keyboard","major":1,"minor":0}]}}`, h.sessionID, h.pkg)
}

func (h *tapeHost) zx81() hostclient.Game {
	return hostclient.Game{ID: "fpga-zx81", Title: "ZX81", System: "fpga", State: "available", RootOnline: true, Launchable: true}
}

func (h *tapeHost) importCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.imports
}

func (h *tapeHost) replaceCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.replaces
}

func (h *tapeHost) clearCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.clears
}

func selectTapeLibraryRoot(t *testing.T, app *App, dir string) {
	t.Helper()
	now := time.Now()
	for i := 0; i < 32; i++ {
		snap := app.Snapshot()
		if !snap.TapePicker.Open || len(snap.TapePicker.Rows) == 0 {
			t.Fatalf("picker closed while choosing root: %+v", snap.TapePicker)
		}
		row := snap.TapePicker.Rows[snap.TapePicker.Index]
		if row.Kind == tapePickerKindRoot && row.Path == dir {
			app.HandleCommand(CmdSelect, now)
			return
		}
		app.HandleCommand(CmdDown, now)
	}
	t.Fatalf("library root %s not listed: %+v", dir, app.Snapshot().TapePicker.Rows)
}

func selectTapeNamedRow(t *testing.T, app *App, name string) {
	t.Helper()
	now := time.Now()
	for i := 0; i < 64; i++ {
		snap := app.Snapshot()
		if !snap.TapePicker.Open || len(snap.TapePicker.Rows) == 0 {
			t.Fatal("picker closed while choosing row")
		}
		row := snap.TapePicker.Rows[snap.TapePicker.Index]
		if row.Name == name {
			return
		}
		app.HandleCommand(CmdDown, now)
	}
	t.Fatalf("row %s not listed: %+v", name, app.Snapshot().TapePicker.Rows)
}
