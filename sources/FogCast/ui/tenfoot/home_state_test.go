package tenfoot

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/ui/rooms"
	"golang.org/x/sys/unix"
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
	// Closing Home runs its unload save and flushes the store. These tests
	// cover filesystem selection, not the render loop or save debounce.
	app.Stop()

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

	again := startHomeState(t, cores)
	waitFor(t, again, "restored core", func(s Snapshot) bool {
		return s.Room.Destination.Label == "Zx81" && s.Room.Destination.PackageID == wantID
	})
}

// TestHomeRestoresSelectionFromReadOnlyConfig keeps a Home selection that
// was already saved when that config directory is later mounted read-only.
// A different writable kit directory must not replace it.
func TestHomeRestoresSelectionFromReadOnlyConfig(t *testing.T) {
	homeFile, kitDir := unwritableHomeFile(t)
	config := mountTmpfs(t)
	cores := homeFixtureCores()
	wantID := cores[1].PackageID
	stateFile := filepath.Join(config, "FogCast", "rooms", "example.fes-cores.state.json")
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "{\n  \"package_id\": \"" + wantID + "\"\n}\n"
	if err := os.WriteFile(stateFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	remountReadOnly(t, config)
	if err := os.WriteFile(filepath.Join(config, "probe"), []byte("x"), 0o644); err == nil {
		t.Fatal("read-only config accepted a write")
	}
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_STATE_HOME", config)
	useKitStateDir(t, kitDir)

	app := startHomeState(t, cores)
	waitFor(t, app, "read-only restored core", func(s Snapshot) bool {
		return s.Room.Destination.Label == "Zx81" && s.Room.Destination.PackageID == wantID
	})
	if _, err := os.Lstat(filepath.Join(kitDir, "FogCast")); !os.IsNotExist(err) {
		t.Fatalf("read-only config fell back to the kit directory: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(homeFile, ".config")); err == nil {
		t.Fatal("state was written under HOME")
	}
}

// TestHomeRestoresSelectionWhenHomeDirectoryIsReadOnly is the kit case:
// HOME is a directory on a read-only filesystem and XDG_CONFIG_HOME is unset.
// The selection is stored under the configured kit state directory.
func TestHomeRestoresSelectionWhenHomeDirectoryIsReadOnly(t *testing.T) {
	home := mountTmpfs(t)
	remountReadOnly(t, home)
	if err := os.WriteFile(filepath.Join(home, "probe"), []byte("x"), 0o644); err == nil {
		t.Fatal("read-only home accepted a write")
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	stateDir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	useKitStateDir(t, stateDir)
	restoreHomeSelection(t, home, stateDir)
}

// TestHomeRestoresSelectionFromNestedConfigDir keeps an explicit config
// directory whose parents do not exist yet. Tenfoot creates them there
// instead of moving the selection to the kit directory.
func TestHomeRestoresSelectionFromNestedConfigDir(t *testing.T) {
	homeFile, kitDir := unwritableHomeFile(t)
	xdg := filepath.Join(t.TempDir(), "a", "b", "config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("XDG_STATE_HOME", xdg)
	useKitStateDir(t, kitDir)
	restoreHomeSelection(t, homeFile, xdg)
	if _, err := os.Lstat(filepath.Join(kitDir, "FogCast")); err == nil {
		t.Fatal("nested config directory was redirected to the kit directory")
	}
}

func mountTmpfs(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("read-only mount requires linux")
	}
	dir := filepath.Join(t.TempDir(), "mnt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := unix.Mount("tmpfs", dir, "tmpfs", 0, "mode=1777")
	if err == nil {
		t.Cleanup(func() { _ = unix.Unmount(dir, unix.MNT_DETACH) })
		return dir
	}
	if !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) {
		t.Fatalf("tmpfs mount: %v", err)
	}
	out, serr := exec.Command("sudo", "-n", "mount", "-t", "tmpfs", "-o", "mode=1777", "tmpfs", dir).CombinedOutput()
	if serr != nil {
		t.Skipf("tmpfs mount: %v (%s)", serr, out)
	}
	t.Cleanup(func() { _ = exec.Command("sudo", "-n", "umount", dir).Run() })
	return dir
}

func remountReadOnly(t *testing.T, dir string) {
	t.Helper()
	err := unix.Mount("", dir, "", unix.MS_REMOUNT|unix.MS_RDONLY, "")
	if err == nil {
		return
	}
	if !errors.Is(err, unix.EPERM) && !errors.Is(err, unix.EACCES) {
		t.Fatalf("remount ro: %v", err)
	}
	out, serr := exec.Command("sudo", "-n", "mount", "-o", "remount,ro", dir).CombinedOutput()
	if serr != nil {
		t.Fatalf("remount ro: %v (%s)", serr, out)
	}
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
	// Closing Home runs its unload save and flushes the store. These tests
	// cover filesystem selection, not the render loop or save debounce.
	app.Stop()
	if _, err := os.Lstat(filepath.Join(homeFile, ".config")); err == nil {
		t.Fatal("state was written under HOME")
	}
	if strings.Count(buf.String(), "state directory is not writable") != 1 {
		t.Fatalf("warning count:\n%s", buf.String())
	}
}
