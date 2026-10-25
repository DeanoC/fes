package tenfoot

import (
	"bytes"
	"context"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/ui/shared"
)

func TestListTapePickerDirMarksPFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tape := filepath.Join(dir, "maze.p")
	other := filepath.Join(dir, "maze.tzx")
	board := filepath.Join(dir, "core.rbf")
	image := filepath.Join(dir, "linux.img")
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
	for _, path := range []string{board, image} {
		if err := os.WriteFile(path, []byte("not media"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(hidden, []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	oversized := filepath.Join(dir, "huge.p")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte{1}, int(protocol.MaxDevelopmentMediaBytes)+1), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := listTapePickerDir(tapeTestPackage(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var tapeRow TapePickerRow
	files := map[string]bool{}
	var eject, typedPath bool
	for _, row := range rows {
		if row.Kind == tapePickerKindFile {
			files[row.Name] = true
		}
		switch row.Name {
		case "maze.p":
			tapeRow = row
		case tapePickerEjectLabel:
			eject = row.Kind == tapePickerKindEject && row.Selectable
		case tapePickerOSKLabel:
			typedPath = row.Kind == tapePickerKindOSK && row.Selectable
		}
		if strings.HasPrefix(row.Name, ".") && row.Kind != tapePickerKindParent {
			t.Fatalf("hidden file listed: %+v", row)
		}
	}
	if !tapeRow.Selectable || tapeRow.Size != 10 {
		t.Fatalf("tape row %+v", tapeRow)
	}
	for _, name := range []string{"maze.tzx", "core.rbf", "linux.img", "huge.p"} {
		if files[name] {
			t.Fatalf("non-admitted file %q was listed: %+v", name, rows)
		}
	}
	if !eject || !typedPath {
		t.Fatalf("nested directory actions missing: %+v", rows)
	}
}

func TestTapePickerDriveRowsKeepActionsAndFilterFiles(t *testing.T) {
	rows := tapePickerDriveRows(tapeTestPackage(), []FirmwarePickerRow{
		{Name: "..", Kind: tapePickerKindParent},
		{Name: "ZX81/", Kind: tapePickerKindDir},
		{Name: "maze.p", Kind: tapePickerKindFile, Size: 10},
		{Name: "core.rbf", Kind: tapePickerKindFile, Size: 10},
		{Name: "linux.img", Kind: tapePickerKindFile, Size: 10},
	})
	if len(rows) != 5 || rows[0].Kind != tapePickerKindParent || rows[1].Kind != tapePickerKindEject || rows[2].Kind != tapePickerKindOSK || rows[3].Kind != tapePickerKindDir || rows[4].Name != "maze.p" || !rows[4].Selectable {
		t.Fatalf("drive folder rows %+v", rows)
	}
}

func TestTapePathOSKDoesNotSeedHostDriveURI(t *testing.T) {
	app := &App{tapePickerPath: drivePath(true, strings.Repeat("a", 24), "media")}
	app.openTapePathOSKLocked()
	if app.settingsOSKKind != settingsOSKTapePath || app.settingsOSKField.Buffer != "" {
		t.Fatalf("path OSK seeded inaccessible host URI: kind=%v buffer=%q", app.settingsOSKKind, app.settingsOSKField.Buffer)
	}
}

func TestOpenTapeFileRejectsWrongClass(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "maze.tzx")
	if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := importAndArmTape(context.Background(), NewClient("", nil), hostclient.SessionResult{CorePackage: tapeTestPackage()}, path, nil); err == nil {
		t.Fatalf("tzx err = %v", err)
	}
	if _, _, err := importAndArmTape(context.Background(), NewClient("", nil), hostclient.SessionResult{CorePackage: tapeTestPackage()}, dir, nil); err == nil {
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
	h.computerTape = true
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
	for _, row := range app.Snapshot().TapePicker.Rows {
		if row.Name == "readme.txt" {
			t.Fatalf("non-media file remained in selector: %+v", app.Snapshot().TapePicker.Rows)
		}
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

	app.HandleCommand(CmdSearch, time.Now())
	snap = waitFor(t, app, "reopened tape selector", func(s Snapshot) bool { return s.TapePicker.Open })
	if snap.TapePicker.Current != "Inserted: maze.p" {
		t.Fatalf("reopened selector current media = %q", snap.TapePicker.Current)
	}
	selectTapeNamedRow(t, app, tapePickerEjectLabel)
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "tape ejected after reopen", func(s Snapshot) bool {
		return !s.TapePicker.Open && strings.Contains(s.Status, "Tape ejected")
	})
	app.HandleCommand(CmdSearch, time.Now())
	snap = waitFor(t, app, "reopened empty tape selector", func(s Snapshot) bool { return s.TapePicker.Open })
	if snap.TapePicker.Current != "No media inserted" {
		t.Fatalf("selector after eject current media = %q", snap.TapePicker.Current)
	}
}

func TestAppTapePickerSurfacesBusyReject(t *testing.T) {
	dir := t.TempDir()
	tape := filepath.Join(dir, "maze.p")
	if err := os.WriteFile(tape, []byte("busy-tape"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newTapeHost(t)
	h.busy = true
	h.computerTape = true
	h.mediaName = "first.p"
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
	if snap.TapePicker.Current != "Inserted: first.p" {
		t.Fatalf("busy rejection lost mounted filename: %q", snap.TapePicker.Current)
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

func TestAppTapePickerEjectRetriesBusyAndKeepsSession(t *testing.T) {
	h := newTapeHost(t)
	h.clearBusyLeft = 2
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "active", func(s Snapshot) bool { return s.Session.State == "active" && s.Session.LoadTape })
	app.HandleCommand(CmdSearch, time.Now())
	selectTapeNamedRow(t, app, tapePickerEjectLabel)
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "ejected", func(s Snapshot) bool {
		return !s.TapePicker.Open && strings.Contains(s.Status, "Tape ejected") && s.Session.State == "active"
	})
	if h.clearCount() != 3 {
		t.Fatalf("clear=%d", h.clearCount())
	}
}

func TestAppTapePickerEjectUnavailableKeepsSession(t *testing.T) {
	h := newTapeHost(t)
	h.clearUnavailable = true
	h.computerTape = true
	h.mediaName = "old.p"
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "active", func(s Snapshot) bool { return s.Session.State == "active" && s.Session.LoadTape })
	app.HandleCommand(CmdSearch, time.Now())
	selectTapeNamedRow(t, app, tapePickerEjectLabel)
	app.HandleCommand(CmdSelect, time.Now())
	snap := waitFor(t, app, "unavailable eject", func(s Snapshot) bool {
		return s.TapePicker.Open && !s.TapePicker.Busy && strings.Contains(s.Status, "MiSTer is unavailable")
	})
	if snap.Session.State != "active" || h.clearCount() != 3 {
		t.Fatalf("state=%s clears=%d status=%q", snap.Session.State, h.clearCount(), snap.Status)
	}
	if snap.TapePicker.Current != "Inserted media name unavailable" {
		t.Fatalf("uncertain eject retained stale filename: %q", snap.TapePicker.Current)
	}
}

func TestAppTapePickerEjectBusyRejectionKeepsMediaName(t *testing.T) {
	h := newTapeHost(t)
	h.clearBusyLeft = 3
	h.computerTape = true
	h.mediaName = "old.p"
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "active", func(s Snapshot) bool { return s.Session.State == "active" && s.Session.LoadTape })
	app.HandleCommand(CmdSearch, time.Now())
	selectTapeNamedRow(t, app, tapePickerEjectLabel)
	app.HandleCommand(CmdSelect, time.Now())
	snap := waitFor(t, app, "busy eject rejection", func(s Snapshot) bool {
		return s.TapePicker.Open && !s.TapePicker.Busy && strings.Contains(s.Status, "tape loader is busy")
	})
	if h.clearCount() != 3 || snap.Session.State != "active" || snap.TapePicker.Current != "Inserted: old.p" {
		t.Fatalf("clears=%d state=%s media=%q status=%q", h.clearCount(), snap.Session.State, snap.TapePicker.Current, snap.Status)
	}
}

func TestAppTapePickerEjectRetryAfterUncertainAttemptClearsName(t *testing.T) {
	h := newTapeHost(t)
	h.computerTape = true
	h.mediaName = "old.p"
	h.clearErrorSequence = []bool{false, true, true}
	app := NewApp(NewClient(h.server.URL, h.server.Client()), 1280, 720, 20)
	app.SetPrefsPath(filepath.Join(t.TempDir(), "tenfoot.json"))
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	waitFor(t, app, "catalog", func(s Snapshot) bool { return len(s.Games) == 1 && !s.Loading })
	app.HandleCommand(CmdSelect, time.Now())
	waitFor(t, app, "active", func(s Snapshot) bool { return s.Session.State == "active" && s.Session.LoadTape })
	app.HandleCommand(CmdSearch, time.Now())
	selectTapeNamedRow(t, app, tapePickerEjectLabel)
	app.HandleCommand(CmdSelect, time.Now())
	snap := waitFor(t, app, "uncertain retry sequence", func(s Snapshot) bool {
		return s.TapePicker.Open && !s.TapePicker.Busy && strings.Contains(s.Status, "tape loader is busy")
	})
	if h.clearCount() != 3 || snap.TapePicker.Current != "Inserted media name unavailable" {
		t.Fatalf("clears=%d media=%q status=%q", h.clearCount(), snap.TapePicker.Current, snap.Status)
	}
}

func TestDismissedTapeArmDoesNotReviveSession(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "maze.p"), []byte("late-tape"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newTapeHost(t)
	h.armStarted = make(chan struct{}, 1)
	h.armRelease = make(chan struct{})
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
	app.HandleCommand(CmdSelect, now)
	select {
	case <-h.armStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("arm did not start")
	}
	app.HandleCommand(CmdHome, now)
	if app.Snapshot().TapePicker.Open {
		t.Fatal("home left the tape picker open")
	}
	app.HandleCommand(CmdStop, now)
	waitFor(t, app, "stopped", func(s Snapshot) bool { return s.Session.State == "idle" })
	close(h.armRelease)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.Tick(time.Now())
		time.Sleep(10 * time.Millisecond)
	}
	snap := app.Snapshot()
	if snap.Session.State != "idle" || snap.TapePicker.Open || strings.Contains(snap.Status, "Tape armed") {
		t.Fatalf("late arm revived sofa: state=%s open=%v status=%q", snap.Session.State, snap.TapePicker.Open, snap.Status)
	}
}

func TestPlayHIDSlashOpensLoadTape(t *testing.T) {
	app := pointerCatalog(4)
	app.NoteInput(InputKeyboard, 2)
	app.session = zx81LiveSession()
	if !app.HandlePlayHIDKey("/", true, time.Now()) {
		t.Fatal("slash must stay on the play HID path")
	}
	snap := app.Snapshot()
	if !snap.TapePicker.Open || !snap.Session.LoadTape {
		t.Fatalf("slash did not open Load-tape: %+v", snap.TapePicker)
	}
	if got := app.Affinity(); got.Kind != InputKeyboard || got.ID != 2 {
		t.Fatalf("slash stole affinity %#v", got)
	}
	app.NoteInput(InputMouse, 1)
	app.closeTapePickerForTest()
	if !app.HandlePlayHIDKey("slash", true, time.Now()) {
		t.Fatal("mouse-affinity slash must stay on the play HID path")
	}
	if !app.Snapshot().TapePicker.Open {
		t.Fatal("mouse affinity slash did not open Load-tape")
	}
}

func TestPlayHIDLetterStaysCoreKeyWhileLoadTapeOffered(t *testing.T) {
	var hidHits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session/input/event" {
			http.NotFound(w, r)
			return
		}
		hidHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)
	app := pointerCatalog(4)
	app.client = NewClient(server.URL, server.Client())
	app.session = zx81LiveSession()
	if !app.HandlePlayHIDKey("s", true, time.Now()) {
		t.Fatal("letter s must stay on the play HID path")
	}
	waitSnapshot(t, app, time.Second, func(Snapshot) bool { return hidHits.Load() == 1 })
	if app.Snapshot().TapePicker.Open {
		t.Fatal("letter s opened Load-tape")
	}
}

func TestHeaderHintPrefersOSKDuringActiveTapeSession(t *testing.T) {
	snap := Snapshot{
		Affinity:  InputKeyboard,
		GPUParked: true,
		Session: SessionSnapshot{
			State:    "active",
			LoadTape: true,
		},
		TapePicker: TapePickerSnapshot{Open: true, Hint: "Enter arm  Esc back"},
		OSK:        shared.OSKSnapshot{Open: true, Hint: "type  Enter done  Esc close", Page: shared.OSKPageLetters},
	}
	got := snap.HeaderHint()
	if !strings.Contains(got, "Enter done") || strings.Contains(got, "arm") {
		t.Fatalf("osk hint = %q", got)
	}
}

func (a *App) closeTapePickerForTest() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closeTapePickerLocked()
}

func zx81LiveSession() hostclient.SessionResult {
	return hostclient.SessionResult{
		ID:     "host-zx81",
		Target: "dev",
		State:  "active",
		CorePackage: &hostclient.SessionCorePackage{
			PackageID:  strings.Repeat("a", 64),
			Generation: 9,
			ABI:        hostclient.SessionCoreABI{ID: "fes.simple-computer", Major: 1},
			ActiveInterfaces: []hostclient.SessionCoreInterface{
				{ID: "fes.media.blob", Major: 1},
				{ID: "fes.keyboard", Major: 1},
			},
		},
		CoreKeyboard: true,
		Input:        &hostclient.SessionInput{State: "attached", Ready: true},
	}
}

type tapeHost struct {
	mu                 sync.Mutex
	launches           int
	imports            int
	replaces           int
	clears             int
	busy               bool
	clearBusyLeft      int
	clearErrorSequence []bool
	clearUnavailable   bool
	lastMediaID        string
	lastName           string
	mediaName          string
	computerTape       bool
	pkg                string
	sessionID          string
	server             *httptest.Server
	armStarted         chan struct{}
	armRelease         chan struct{}
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
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/stop":
			h.mu.Lock()
			h.launches = 0
			h.mu.Unlock()
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
			if !busy {
				h.mediaName = req.Name
			}
			h.mu.Unlock()
			if h.armStarted != nil {
				select {
				case h.armStarted <- struct{}{}:
				default:
				}
			}
			if h.armRelease != nil {
				<-h.armRelease
			}
			if busy {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": "BUSY", "message": "tape loader is busy; retry after LOAD finishes", "phase": "input", "no_change": true},
				})
				return
			}
			_, _ = io.WriteString(w, h.sessionJSON())
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/session/live-media/clear":
			h.mu.Lock()
			h.clears++
			sequenced := len(h.clearErrorSequence) > 0
			noChange := false
			if sequenced {
				noChange = h.clearErrorSequence[0]
				h.clearErrorSequence = h.clearErrorSequence[1:]
			}
			busyLeft := h.clearBusyLeft
			if busyLeft > 0 {
				h.clearBusyLeft--
			}
			unavailable := h.clearUnavailable
			h.mu.Unlock()
			if sequenced {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": "BUSY", "message": "tape loader is busy; retry after LOAD finishes", "phase": "input", "no_change": noChange},
				})
				return
			}
			if busyLeft > 0 {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]any{"code": "BUSY", "message": "tape loader is busy; retry after LOAD finishes", "phase": "input", "no_change": true},
				})
				return
			}
			if unavailable {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": map[string]string{"code": "MISTER_UNAVAILABLE", "message": "MiSTer is unavailable", "phase": "input"},
				})
				return
			}
			h.mu.Lock()
			h.mediaName = ""
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
	if h.computerTape {
		state := protocol.MediaUnitEmpty
		if h.mediaName != "" {
			state = protocol.MediaUnitReady
		}
		return fmt.Sprintf(`{"id":%q,"target":"dev","state":"active","game_id":"fpga-zx81","execution":"fpga_native","media_name":%q,"core_package":{"package_id":%q,"generation":9,"abi":{"id":"fes.computer","major":1,"minor":0},"active_interfaces":[{"id":"fes.media.zx81-tape","major":1,"minor":0},{"id":"fes.keyboard.hid","major":1,"minor":0}],"media_units":[{"unit":0,"interface":{"id":"fes.media.zx81-tape","major":1,"minor":0},"min_bytes":1,"max_bytes":16384,"chunk_bytes":512,"state":%q}]}}`, h.sessionID, h.mediaName, h.pkg, state)
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

func tapeTestPackage() *hostclient.SessionCorePackage {
	return &hostclient.SessionCorePackage{ABI: hostclient.SessionCoreABI{ID: "fes.simple-computer", Major: 1}, ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: "fes.media.blob", Major: 1}}}
}

func zx81ComputerTapePackage() *hostclient.SessionCorePackage {
	tape := protocol.Zx81TapeInterface()
	return &hostclient.SessionCorePackage{
		ABI:              hostclient.SessionCoreABI{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: tape.ID, Major: tape.Major, Minor: tape.Minor}},
		MediaUnits: []protocol.MediaUnitStatus{{
			Unit: protocol.Zx81TapeUnit, Interface: tape, MinBytes: 1, MaxBytes: 16384, ChunkBytes: 512, State: "empty",
		}},
	}
}

func TestTapePickerIncludesEmuhdVolumeRoot(t *testing.T) {
	t.Parallel()
	found := false
	for _, path := range tapePickerVolumeRoots {
		if path == "/mnt/emuhd" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("local /mnt/emuhd games must remain browsable when that directory exists")
	}
}

func TestEmptyDirectoryDoesNotHideTapePicker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("not a tape"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := listTapePickerDir(zx81ComputerTapePackage(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 3 || rows[0].Kind != tapePickerKindParent || rows[1].Kind != tapePickerKindEject || rows[2].Kind != tapePickerKindOSK {
		t.Fatalf("empty library must stay browsable: %+v", rows)
	}
	for _, row := range rows {
		if row.Kind == tapePickerKindFile {
			t.Fatalf("non-.p file was listed: %+v", row)
		}
	}
	app := &App{hostSettings: hostclient.LibrarySettings{Libraries: []hostclient.LibraryRoot{{ID: "usb", System: "zx81", Root: dir}}}}
	app.showTapePickerRootsLocked("Choose tape or disk media.")
	roots := app.tapePickerRows
	if len(roots) < 2 || roots[0].Kind != tapePickerKindEject || !roots[0].Selectable || roots[1].Kind != tapePickerKindOSK {
		t.Fatalf("eject and typed path must lead an empty library: %+v", roots)
	}
	saw := false
	for _, row := range roots {
		if row.Kind == tapePickerKindRoot && row.Path == dir {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("empty library root omitted: %+v", roots)
	}
}

func TestTapePickerBrowsesAccessibleDirectoriesAndFiltersSystemFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "BIOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	games := filepath.Join(root, "ZX81")
	if err := os.Mkdir(games, 0o755); err != nil {
		t.Fatal(err)
	}
	const game = "3D Monster Maze.p"
	if err := os.WriteFile(filepath.Join(games, game), []byte("zx81-game"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"linux.img", "core.rbf"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("system file"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := &App{}
	app.hostSettings.Libraries = []hostclient.LibraryRoot{{ID: "zx81", System: "zx81", Root: root}}
	app.showTapePickerRootsLocked("Choose tape or disk media.")
	saw := false
	for _, row := range app.tapePickerRows {
		if row.Kind == tapePickerKindRoot && row.Path == root {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("configured accessible library missing from roots: %+v", app.tapePickerRows)
	}
	top, err := listTapePickerDir(zx81ComputerTapePackage(), root)
	if err != nil {
		t.Fatal(err)
	}
	var bios, shelf bool
	files := map[string]bool{}
	for _, row := range top {
		if row.Name == "BIOS/" && row.Kind == tapePickerKindDir {
			bios = true
		}
		if row.Name == "ZX81/" && row.Kind == tapePickerKindDir {
			shelf = true
		}
		if row.Kind == tapePickerKindFile {
			files[row.Name] = true
		}
	}
	if !bios || !shelf || files["linux.img"] || files["core.rbf"] {
		t.Fatalf("directories must stay navigable while system files are filtered: %+v", top)
	}
	rows, err := listTapePickerDir(zx81ComputerTapePackage(), games)
	if err != nil {
		t.Fatal(err)
	}
	var tape TapePickerRow
	for _, row := range rows {
		if row.Name == game {
			tape = row
		}
	}
	if !tape.Selectable || tape.Kind != tapePickerKindFile || tape.Size != 9 {
		t.Fatalf("real .p beside BIOS was not selectable: %+v", tape)
	}
}

func TestMediaPickerUsesRunningDiskFormat(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int{"disk.d64": 174848, "wrong.dsk": 174848, "short.d64": 10} {
		if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0600); err != nil {
			t.Fatal(err)
		}
	}
	contract := protocol.C64DiskInterface()
	p := &hostclient.SessionCorePackage{ABI: hostclient.SessionCoreABI{ID: "fes.computer", Major: 1},
		ActiveInterfaces: []hostclient.SessionCoreInterface{{ID: contract.ID, Major: 1}},
		MediaUnits:       []protocol.MediaUnitStatus{{Unit: 0, Interface: contract, MinBytes: 174848, MaxBytes: 174848, ChunkBytes: 512, State: "empty"}}}
	rows, err := listTapePickerDir(p, dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Kind == tapePickerKindFile {
			if row.Name != "disk.d64" || !row.Selectable {
				t.Fatalf("non-admitted disk was listed: %+v", row)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("admitted D64 was filtered: %+v", rows)
	}
}
