package tenfoot

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/ui/rooms"
)

func unwritableHomeFile(t *testing.T) (homeFile, stateDir string) {
	t.Helper()
	root := t.TempDir()
	homeFile = filepath.Join(root, "home")
	if err := os.WriteFile(homeFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	stateDir = filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeFile)
	return homeFile, stateDir
}

func startHomeState(t *testing.T, cores []localcores.Core) *App {
	t.Helper()
	host := newRoomHost(t)
	app := NewApp(NewClient(host.server.URL, host.server.Client()), 1280, 720, 50)
	app.SetRooms(rooms.NewIndex(rooms.Examples()), t.TempDir())
	app.SetHomeRooms(true)
	app.SetHomeRoom(installedCoresHomeID)
	app.SetKitLocal(&fakeLocalCores{cores: cores}, &fakePadFeed{})
	app.Start(t.Context())
	t.Cleanup(app.Stop)
	return app
}

func settleHomeSelection(t *testing.T, app *App) {
	t.Helper()
	now := time.Now()
	for i := 1; i <= 15; i++ {
		now = now.Add(100 * time.Millisecond)
		app.Tick(now)
	}
}

func useKitStateDir(t *testing.T, dir string) {
	t.Helper()
	orig := kitPersistentStateDir
	kitPersistentStateDir = dir
	t.Cleanup(func() { kitPersistentStateDir = orig })
}

// TestHomeRestoresSelectionWhenHomeIsUnwritable saves the shelf selection
// under the configured state directory while HOME is not a writable
// directory, then opens Home again and reads that selection back.
func TestHomeRestoresSelectionWhenHomeIsUnwritable(t *testing.T) {
	homeFile, stateDir := unwritableHomeFile(t)
	t.Setenv("XDG_CONFIG_HOME", stateDir)
	t.Setenv("XDG_STATE_HOME", stateDir)
	restoreHomeSelection(t, homeFile, stateDir)
}

// TestHomeRestoresSelectionFromKitStateDir covers a supervisor that still
// leaves HOME=/ and does not set XDG_CONFIG_HOME. Tenfoot then uses the
// FAT state directory directly.
func TestHomeRestoresSelectionFromKitStateDir(t *testing.T) {
	homeFile, stateDir := unwritableHomeFile(t)
	t.Setenv("XDG_CONFIG_HOME", "")
	useKitStateDir(t, stateDir)
	restoreHomeSelection(t, homeFile, stateDir)
}

func restoreHomeSelection(t *testing.T, homeFile, stateDir string) {
	t.Helper()
	cores := homeFixtureCores()
	wantID := cores[1].PackageID
	app := startHomeState(t, cores)
	waitFor(t, app, "first core", func(s Snapshot) bool {
		return s.Room.Destination.Label == "FES Pong"
	})
	app.HandleCommand(CmdRight, time.Now())
	waitFor(t, app, "moved core", func(s Snapshot) bool {
		return s.Room.Destination.Label == "Zx81"
	})
	settleHomeSelection(t, app)

	stateFile := filepath.Join(stateDir, "FogCast", "rooms", "example.fes-cores.state.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("state file: %v", err)
	}
	if !strings.Contains(string(data), wantID) {
		t.Fatalf("state file %s does not contain %s", data, wantID)
	}
	if _, err := os.Lstat(filepath.Join(homeFile, ".config")); err == nil {
		t.Fatal("state was written under HOME")
	}
	app.Stop()

	again := startHomeState(t, cores)
	waitFor(t, again, "restored core", func(s Snapshot) bool {
		return s.Room.Destination.Label == "Zx81" && s.Room.Destination.PackageID == wantID
	})
}

func TestHomeKeepsWorkingWhenStateDirIsUnwritable(t *testing.T) {
	homeFile, _ := unwritableHomeFile(t)
	t.Setenv("XDG_CONFIG_HOME", homeFile)
	stateWarnMu.Lock()
	prevWarned := stateDirWarned
	prevWriter := stateWarnWriter
	stateDirWarned = false
	var buf bytes.Buffer
	stateWarnWriter = &buf
	stateWarnMu.Unlock()
	t.Cleanup(func() {
		stateWarnMu.Lock()
		stateDirWarned = prevWarned
		stateWarnWriter = prevWriter
		stateWarnMu.Unlock()
	})
	useKitStateDir(t, homeFile)

	app := startHomeState(t, homeFixtureCores())
	waitFor(t, app, "first core", func(s Snapshot) bool {
		return s.Room.Destination.Label == "FES Pong"
	})
	app.HandleCommand(CmdRight, time.Now())
	snap := waitFor(t, app, "moved core", func(s Snapshot) bool {
		return s.Room.Destination.Label == "Zx81"
	})
	if snap.Room.Err != "" {
		t.Fatalf("room failed: %s", snap.Room.Err)
	}
	settleHomeSelection(t, app)
	if _, err := os.Lstat(filepath.Join(homeFile, ".config")); err == nil {
		t.Fatal("state was written under HOME")
	}
	if strings.Count(buf.String(), "state directory is not writable") != 1 {
		t.Fatalf("warning count:\n%s", buf.String())
	}
}
