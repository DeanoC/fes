package appliancedata

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance/blockprobe"
)

type recordingMounter struct {
	mounted      map[string]string
	boundSources map[string]*os.File
	volumes      []string
	binds        []string
	unbinds      []string
	mountErr     error
	bindErr      error
	bindErrOn    string
	onUnbind     func(string)
}

func newRecordingMounter() *recordingMounter {
	return &recordingMounter{mounted: map[string]string{}, boundSources: map[string]*os.File{}}
}

func (m *recordingMounter) MountVolume(source, target string) error {
	if m.mountErr != nil {
		return m.mountErr
	}
	m.mounted[target] = source
	m.volumes = append(m.volumes, source+" -> "+target)
	return nil
}

func (m *recordingMounter) Bind(source, target string) error {
	if m.bindErr != nil && (m.bindErrOn == "" || m.bindErrOn == target) {
		return m.bindErr
	}
	bound, err := os.Open(source)
	if err != nil {
		return err
	}
	m.mounted[target] = source
	m.boundSources[target] = bound
	m.binds = append(m.binds, source+" -> "+target)
	return nil
}

func (m *recordingMounter) Unbind(_, target string) error {
	if m.onUnbind != nil {
		m.onUnbind(target)
	}
	if m.mounted[target] == "" {
		return errors.New("target is not mounted")
	}
	delete(m.mounted, target)
	if source := m.boundSources[target]; source != nil {
		_ = source.Close()
	}
	delete(m.boundSources, target)
	m.unbinds = append(m.unbinds, target)
	return nil
}

func (m *recordingMounter) BindMatches(source, target string) (bool, error) {
	bound, ok := m.boundSources[target]
	if !ok {
		return false, nil
	}
	boundInfo, err := bound.Stat()
	if err != nil {
		return false, err
	}
	current, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(boundInfo, current), nil
}

func (m *recordingMounter) Close() error {
	var errs []error
	for target, source := range m.boundSources {
		if err := source.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close bind source for %s: %w", target, err))
		}
		delete(m.boundSources, target)
	}
	return errors.Join(errs...)
}

func testConfig(t *testing.T, mounter *recordingMounter) Config {
	t.Helper()
	t.Cleanup(func() { _ = mounter.Close() })
	root := t.TempDir()
	device := filepath.Join(root, "FESDATA3")
	if err := os.WriteFile(device, ext4Superblock(DefaultLabel), 0o600); err != nil {
		t.Fatal(err)
	}
	fat := filepath.Join(root, "fat", "fogcast")
	mount := filepath.Join(root, "run", "fesdata3")
	libraryFAT := filepath.Join(root, "fat", "games")
	libraryData := filepath.Join(mount, "games")
	if err := os.MkdirAll(fat, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(libraryFAT, 0o700); err != nil {
		t.Fatal(err)
	}
	return Config{
		Devices:         []string{device},
		Label:           DefaultLabel,
		MountPoint:      mount,
		FATRoot:         fat,
		LibraryFATRoot:  libraryFAT,
		LibraryDataRoot: libraryData,
		BindNames:       append([]string(nil), DefaultBindNames...),
		EvaluateGate:    func() (Gate, error) { return Gate{}, nil },
		DevicePresent:   pathExists,
		ReadLabel:       readExt4Label,
		Mounted:         func(target string) bool { return mounter.mounted[target] != "" },
		BindMatches:     mounter.BindMatches,
		Mounter:         mounter,
	}
}

func ext4Superblock(label string) []byte {
	buf := make([]byte, 2048)
	binary.LittleEndian.PutUint16(buf[ext4SuperOffset+ext4MagicOffset:], ext4Magic)
	copy(buf[ext4SuperOffset+ext4LabelOffset:], label)
	return buf
}

func TestPrepareSkipsWhenP3Absent(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	fatGame := filepath.Join(cfg.LibraryFATRoot, "sms", "kept.sms")
	if err := os.MkdirAll(filepath.Dir(fatGame), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fatGame, []byte("fat-library"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.DevicePresent = func(string) bool { return false }
	result, err := Prepare(cfg)
	if err != nil || result.Skipped != SkipAbsent || len(result.Bound) != 0 {
		t.Fatalf("absent = %+v %v", result, err)
	}
	if len(mounter.volumes) != 0 || len(mounter.binds) != 0 {
		t.Fatalf("absent mutated mounts: %+v %+v", mounter.volumes, mounter.binds)
	}
	if got, err := os.ReadFile(fatGame); err != nil || string(got) != "fat-library" {
		t.Fatalf("FAT library was not preserved without p3: %q %v", got, err)
	}
	if pending, err := gamesFallbackPending(cfg.LibraryFATRoot); err != nil || !pending {
		t.Fatalf("FAT fallback was not recorded: pending=%v err=%v", pending, err)
	}
}

func TestPrepareRecordsFallbackAfterP3MountFailure(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	mounter.mountErr = errors.New("p3 mount failed")
	cfg := testConfig(t, mounter)

	result, err := Prepare(cfg)
	if !errors.Is(err, mounter.mountErr) || result.Device == "" {
		t.Fatalf("failed mount = %+v %v", result, err)
	}
	if pending, err := gamesFallbackPending(cfg.LibraryFATRoot); err != nil || !pending {
		t.Fatalf("FAT fallback was not recorded after mount failure: pending=%v err=%v", pending, err)
	}
}

func TestPrepareCopiesFATGamesAddedWhileP3IsUnavailable(t *testing.T) {
	initialMounter := newRecordingMounter()
	cfg := testConfig(t, initialMounter)
	fatGame := filepath.Join(cfg.LibraryFATRoot, "sms", "initial.sms")
	if err := os.MkdirAll(filepath.Dir(fatGame), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fatGame, []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := Prepare(cfg); err != nil || result.Skipped != "" {
		t.Fatalf("initial prepare = %+v %v", result, err)
	}

	absentMounter := newRecordingMounter()
	t.Cleanup(func() { _ = absentMounter.Close() })
	cfg.DevicePresent = func(string) bool { return false }
	cfg.Mounter = absentMounter
	cfg.Mounted = func(target string) bool { return absentMounter.mounted[target] != "" }
	cfg.BindMatches = absentMounter.BindMatches
	if result, err := Prepare(cfg); err != nil || result.Skipped != SkipAbsent {
		t.Fatalf("absent prepare = %+v %v", result, err)
	}
	addedGame := filepath.Join(cfg.LibraryFATRoot, "sms", "added-while-absent.sms")
	if err := os.WriteFile(addedGame, []byte("added on FAT"), 0o600); err != nil {
		t.Fatal(err)
	}

	restoredMounter := newRecordingMounter()
	t.Cleanup(func() { _ = restoredMounter.Close() })
	cfg.DevicePresent = pathExists
	cfg.Mounter = restoredMounter
	cfg.Mounted = func(target string) bool { return restoredMounter.mounted[target] != "" }
	cfg.BindMatches = restoredMounter.BindMatches
	result, err := Prepare(cfg)
	if err != nil || !slices.Contains(result.Bound, "games") {
		t.Fatalf("restored prepare = %+v %v", result, err)
	}
	if got, err := os.ReadFile(filepath.Join(cfg.LibraryDataRoot, "sms", "added-while-absent.sms")); err != nil || string(got) != "added on FAT" {
		t.Fatalf("FAT game was hidden by the old p3 tree: %q %v", got, err)
	}
	if pending, err := gamesFallbackPending(cfg.LibraryFATRoot); err != nil || pending {
		t.Fatalf("FAT fallback marker was not cleared: pending=%v err=%v", pending, err)
	}
}

func TestPrepareRetriesInterruptedGameRepairBeforeBinding(t *testing.T) {
	t.Parallel()
	firstMounter := newRecordingMounter()
	cfg := testConfig(t, firstMounter)
	firstGame := filepath.Join(cfg.LibraryFATRoot, "a.sms")
	secondGame := filepath.Join(cfg.LibraryFATRoot, "zdir", "b.sms")
	if err := os.MkdirAll(filepath.Dir(secondGame), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(firstGame, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secondGame, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(cfg); err != nil {
		t.Fatalf("initial prepare: %v", err)
	}

	// Replacing the bound source makes Prepare take its repair path. Leave a
	// destination conflict after one file can be copied to model an interrupted
	// repair with a completed migration marker and a partial p3 games tree.
	if err := os.RemoveAll(cfg.LibraryDataRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.LibraryDataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.LibraryDataRoot, "zdir"), []byte("conflict"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(cfg); err == nil {
		t.Fatal("repair accepted a file where the game directory must be")
	}
	if got, err := os.ReadFile(filepath.Join(cfg.LibraryDataRoot, "a.sms")); err != nil || string(got) != "first" {
		t.Fatalf("partial repair did not copy its first game: %q %v", got, err)
	}
	if pending, err := gamesFallbackPending(cfg.LibraryFATRoot); err != nil || !pending {
		t.Fatalf("interrupted repair did not retain retry marker: pending=%v err=%v", pending, err)
	}

	if err := os.Remove(filepath.Join(cfg.LibraryDataRoot, "zdir")); err != nil {
		t.Fatal(err)
	}
	secondMounter := newRecordingMounter()
	t.Cleanup(func() { _ = secondMounter.Close() })
	cfg.Mounter = secondMounter
	cfg.Mounted = func(target string) bool { return secondMounter.mounted[target] != "" }
	cfg.BindMatches = secondMounter.BindMatches
	result, err := Prepare(cfg)
	if err != nil || !slices.Contains(result.Bound, "games") {
		t.Fatalf("retry prepare = %+v %v", result, err)
	}
	if got, err := os.ReadFile(filepath.Join(cfg.LibraryDataRoot, "zdir", "b.sms")); err != nil || string(got) != "second" {
		t.Fatalf("retry did not restore game missing from partial p3 tree: %q %v", got, err)
	}
	if pending, err := gamesFallbackPending(cfg.LibraryFATRoot); err != nil || pending {
		t.Fatalf("successful retry did not clear marker: pending=%v err=%v", pending, err)
	}
}

func TestPrepareRecordsFallbackBeforeRemovingStaleGameBind(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	fatGame := filepath.Join(cfg.LibraryFATRoot, "sms", "kept.sms")
	if err := os.MkdirAll(filepath.Dir(fatGame), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fatGame, []byte("fat-game"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(cfg); err != nil {
		t.Fatalf("initial prepare: %v", err)
	}

	// Replacing the p3 source makes its existing game bind stale. Observe the
	// fallback marker at the instant Prepare attempts to remove that bind.
	if err := os.RemoveAll(cfg.LibraryDataRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.LibraryDataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	unbound := false
	mounter.onUnbind = func(target string) {
		if target != cfg.LibraryFATRoot {
			return
		}
		unbound = true
		pending, err := gamesFallbackPending(cfg.LibraryFATRoot)
		if err != nil || !pending {
			t.Errorf("fallback marker at stale game unbind = pending:%v err:%v, want pending", pending, err)
		}
	}
	result, err := Prepare(cfg)
	if err != nil || !slices.Contains(result.Bound, "games") {
		t.Fatalf("repair prepare = %+v %v", result, err)
	}
	if !unbound {
		t.Fatal("stale game library bind was not removed")
	}
	if got, err := os.ReadFile(filepath.Join(cfg.LibraryDataRoot, "sms", "kept.sms")); err != nil || string(got) != "fat-game" {
		t.Fatalf("repair did not restore the game from FAT: %q %v", got, err)
	}
	if pending, err := gamesFallbackPending(cfg.LibraryFATRoot); err != nil || pending {
		t.Fatalf("successful repair did not clear fallback marker: pending=%v err=%v", pending, err)
	}
}

func TestPrepareKeepsFallbackMarkerUntilGameBindSucceeds(t *testing.T) {
	t.Parallel()
	absentMounter := newRecordingMounter()
	cfg := testConfig(t, absentMounter)
	fatGame := filepath.Join(cfg.LibraryFATRoot, "sms", "kept.sms")
	if err := os.MkdirAll(filepath.Dir(fatGame), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fatGame, []byte("fat-game"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.DevicePresent = func(string) bool { return false }
	if _, err := Prepare(cfg); err != nil {
		t.Fatal("absent prepare:", err)
	}

	failedMounter := newRecordingMounter()
	t.Cleanup(func() { _ = failedMounter.Close() })
	failedMounter.bindErr = errors.New("game bind failed")
	failedMounter.bindErrOn = cfg.LibraryFATRoot
	cfg.DevicePresent = pathExists
	cfg.Mounter = failedMounter
	cfg.Mounted = func(target string) bool { return failedMounter.mounted[target] != "" }
	cfg.BindMatches = failedMounter.BindMatches
	if _, err := Prepare(cfg); !errors.Is(err, failedMounter.bindErr) {
		t.Fatalf("failed games bind = %v", err)
	}
	if pending, err := gamesFallbackPending(cfg.LibraryFATRoot); err != nil || !pending {
		t.Fatalf("failed bind cleared FAT fallback marker: pending=%v err=%v", pending, err)
	}

	workingMounter := newRecordingMounter()
	t.Cleanup(func() { _ = workingMounter.Close() })
	cfg.Mounter = workingMounter
	cfg.Mounted = func(target string) bool { return workingMounter.mounted[target] != "" }
	cfg.BindMatches = workingMounter.BindMatches
	result, err := Prepare(cfg)
	if err != nil || !slices.Contains(result.Bound, "games") {
		t.Fatalf("retry after bind failure = %+v %v", result, err)
	}
	if pending, err := gamesFallbackPending(cfg.LibraryFATRoot); err != nil || pending {
		t.Fatalf("successful bind did not clear marker: pending=%v err=%v", pending, err)
	}
}

func TestCopyMarkerAtomicallyFallsBackWhenHardLinksAreUnsupported(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	marker := filepath.Join(dir, gamesFallbackMarker)
	linkFailure := errors.New("hard links are unsupported")
	linkCalls := 0
	renameCalls := 0
	err := copyMarkerAtomicallyWithOps(strings.NewReader("pending\n"), marker,
		func(string, string) error {
			linkCalls++
			return linkFailure
		},
		func(old, new string) error {
			renameCalls++
			return os.Rename(old, new)
		},
	)
	if err != nil {
		t.Fatalf("copy marker with unsupported hard links = %v", err)
	}
	if linkCalls != 1 || renameCalls != 1 {
		t.Fatalf("publication calls = link:%d rename:%d, want one each", linkCalls, renameCalls)
	}
	if got, err := os.ReadFile(marker); err != nil || string(got) != "pending\n" {
		t.Fatalf("published marker = %q, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != gamesFallbackMarker {
		t.Fatalf("marker directory entries = %v, want only the published marker", entries)
	}
}

func TestPrepareRefusesTrialPendingAndCorrupt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		gate   Gate
		reason string
	}{{Gate{Trial: true}, SkipTrial}, {Gate{Pending: true}, SkipPending}, {Gate{Corrupt: true}, SkipCorrupt}, {Gate{Trial: true, Pending: true, Corrupt: true}, SkipCorrupt}} {
		mounter := newRecordingMounter()
		cfg := testConfig(t, mounter)
		cfg.EvaluateGate = func() (Gate, error) { return tc.gate, nil }
		result, err := Prepare(cfg)
		if err != nil || result.Skipped != tc.reason || len(result.Bound) != 0 {
			t.Fatalf("gate %+v = %+v %v, want %s", tc.gate, result, err, tc.reason)
		}
		if len(mounter.volumes) != 0 || len(mounter.binds) != 0 {
			t.Fatalf("blocked bind mutated mounts for %s", tc.reason)
		}
	}
}

func TestPrepareBindsMutableTreesAndCopiesExistingFiles(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	cache := filepath.Join(cfg.FATRoot, "cache")
	if err := os.MkdirAll(filepath.Join(cache, "snes"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "snes", "game.sfc"), []byte("rom"), 0o600); err != nil {
		t.Fatal(err)
	}
	game := filepath.Join(cfg.LibraryFATRoot, "sms", "game.sms")
	if err := os.MkdirAll(filepath.Dir(game), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(game, []byte("game"), 0o600); err != nil {
		t.Fatal(err)
	}
	releases := filepath.Join(cfg.FATRoot, "releases", "images")
	if err := os.MkdirAll(releases, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releases, "known-good.img"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.FATRoot, "agent.toml"), []byte("token = \"secret\""), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Prepare(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Skipped != "" || result.Device == "" {
		t.Fatalf("result = %+v", result)
	}
	wantBound := append(append([]string(nil), DefaultBindNames...), "games")
	if got := strings.Join(result.Bound, ","); got != strings.Join(wantBound, ",") {
		t.Fatalf("bound = %s", got)
	}
	if len(mounter.volumes) != 1 || !strings.Contains(mounter.volumes[0], cfg.MountPoint) {
		t.Fatalf("volume mounts = %v", mounter.volumes)
	}
	if len(mounter.binds) != len(DefaultBindNames)+1 {
		t.Fatalf("binds = %v", mounter.binds)
	}
	gameCopy, err := os.ReadFile(filepath.Join(cfg.LibraryDataRoot, "sms", "game.sms"))
	if err != nil || string(gameCopy) != "game" {
		t.Fatalf("game library was not copied to p3: %q %v", gameCopy, err)
	}
	if got := mounter.mounted[cfg.LibraryFATRoot]; got != cfg.LibraryDataRoot {
		t.Fatalf("game library bind = %q, want %q", got, cfg.LibraryDataRoot)
	}
	copied := filepath.Join(cfg.MountPoint, "cache", "snes", "game.sfc")
	body, err := os.ReadFile(copied)
	if err != nil || !bytes.Equal(body, []byte("rom")) {
		t.Fatalf("copied cache = %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(cfg.MountPoint, "releases")); !os.IsNotExist(err) {
		t.Fatal("releases were copied onto p3")
	}
	keep, err := os.ReadFile(filepath.Join(releases, "known-good.img"))
	if err != nil || string(keep) != "keep" {
		t.Fatalf("known-good wiped: %q %v", keep, err)
	}
	cred, err := os.ReadFile(filepath.Join(cfg.FATRoot, "agent.toml"))
	if err != nil || !strings.Contains(string(cred), "secret") {
		t.Fatalf("credentials wiped: %q %v", cred, err)
	}
	original, err := os.ReadFile(filepath.Join(cache, "snes", "game.sfc"))
	if err != nil || string(original) != "rom" {
		t.Fatalf("FAT original removed: %q %v", original, err)
	}
}

func TestPrepareDoesNotOverwriteExistingP3Files(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	fatSave := filepath.Join(cfg.FATRoot, "saves", "snes", "game.srm")
	p3Save := filepath.Join(cfg.MountPoint, "saves", "snes", "game.srm")
	p3Only := filepath.Join(cfg.MountPoint, "saves", "snes", "kept.srm")
	if err := os.MkdirAll(filepath.Dir(fatSave), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p3Save), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fatSave, []byte("fat-new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p3Save, []byte("p3-known-good"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p3Only, []byte("p3-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	fatOnly := filepath.Join(cfg.FATRoot, "saves", "snes", "trial.srm")
	if err := os.WriteFile(fatOnly, []byte("from-fat"), 0o600); err != nil {
		t.Fatal(err)
	}
	fatGame := filepath.Join(cfg.LibraryFATRoot, "sms", "game.sms")
	p3Game := filepath.Join(cfg.LibraryDataRoot, "sms", "game.sms")
	if err := os.MkdirAll(filepath.Dir(fatGame), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(p3Game), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fatGame, []byte("fat-game"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p3Game, []byte("p3-game"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Prepare(cfg)
	if err != nil || result.Skipped != "" {
		t.Fatalf("prepare = %+v %v", result, err)
	}
	kept, err := os.ReadFile(p3Save)
	if err != nil || string(kept) != "p3-known-good" {
		t.Fatalf("p3 save overwritten: %q %v", kept, err)
	}
	only, err := os.ReadFile(p3Only)
	if err != nil || string(only) != "p3-only" {
		t.Fatalf("p3-only save lost: %q %v", only, err)
	}
	copied, err := os.ReadFile(filepath.Join(cfg.MountPoint, "saves", "snes", "trial.srm"))
	if err != nil || string(copied) != "from-fat" {
		t.Fatalf("missing FAT file not copied: %q %v", copied, err)
	}
	game, err := os.ReadFile(p3Game)
	if err != nil || string(game) != "p3-game" {
		t.Fatalf("existing p3 game was overwritten: %q %v", game, err)
	}
	if got := mounter.mounted[cfg.LibraryFATRoot]; got != cfg.LibraryDataRoot {
		t.Fatalf("game tree is not backed by p3: %q", got)
	}
}

func TestMigrateGamesOnceSyncsDestinationDirectoriesBeforeMarker(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "fat", "games")
	dst := filepath.Join(dir, "p3", "games")
	marker := filepath.Join(dir, "p3", gamesMigrationMarker)
	sourceGame := filepath.Join(src, "sms", "game.sms")
	if err := os.MkdirAll(filepath.Dir(sourceGame), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourceGame, []byte("game"), 0o600); err != nil {
		t.Fatal(err)
	}
	syncFailure := errors.New("destination sync failed")
	err := migrateGamesOnceWithSync(src, dst, marker, func(path string) error {
		if filepath.Clean(path) == filepath.Join(dst, "sms") {
			return syncFailure
		}
		return syncDirectory(path)
	})
	if !errors.Is(err, syncFailure) {
		t.Fatalf("migration with failed destination sync = %v, want sync error", err)
	}
	if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("marker exists after failed destination sync: %v", err)
	}

	markerDirSyncs := 0
	syncedGameDir := false
	syncDir := func(path string) error {
		path = filepath.Clean(path)
		if path == dst || strings.HasPrefix(path, dst+string(os.PathSeparator)) {
			if _, err := os.Lstat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("marker exists before destination sync %s: %v", path, err)
			}
			if path == filepath.Join(dst, "sms") {
				syncedGameDir = true
			}
		}
		if path == filepath.Dir(marker) {
			markerDirSyncs++
			complete, err := migrationComplete(marker)
			if err != nil {
				return err
			}
			if markerDirSyncs == 1 && complete {
				t.Fatal("marker was published before destination directories were synced")
			}
			if markerDirSyncs == 2 && !complete {
				t.Fatal("marker directory was synced before marker publication")
			}
		}
		return syncDirectory(path)
	}
	if err := migrateGamesOnceWithSync(src, dst, marker, syncDir); err != nil {
		t.Fatal(err)
	}
	if !syncedGameDir {
		t.Fatal("game entry directory was not synced")
	}
	if markerDirSyncs != 2 {
		t.Fatalf("marker directory syncs = %d, want pre- and post-publication syncs", markerDirSyncs)
	}
	if complete, err := migrationComplete(marker); err != nil || !complete {
		t.Fatalf("migration marker complete = %v, %v", complete, err)
	}
}

func TestPrepareDoesNotRestoreDeletedGameAfterMigration(t *testing.T) {
	t.Parallel()
	firstMounter := newRecordingMounter()
	cfg := testConfig(t, firstMounter)
	fatGame := filepath.Join(cfg.LibraryFATRoot, "sms", "deleted.sms")
	if err := os.MkdirAll(filepath.Dir(fatGame), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fatGame, []byte("fat-game"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := Prepare(cfg)
	if err != nil || first.Skipped != "" {
		t.Fatalf("initial prepare = %+v %v", first, err)
	}
	p3Game := filepath.Join(cfg.LibraryDataRoot, "sms", "deleted.sms")
	if got, err := os.ReadFile(p3Game); err != nil || string(got) != "fat-game" {
		t.Fatalf("initial migration = %q %v", got, err)
	}
	if err := os.Remove(p3Game); err != nil {
		t.Fatal(err)
	}

	// A fresh mounter models startup after reboot while the p3 files and marker
	// remain on the persistent volume.
	secondMounter := newRecordingMounter()
	t.Cleanup(func() { _ = secondMounter.Close() })
	cfg.Mounter = secondMounter
	cfg.Mounted = func(target string) bool { return secondMounter.mounted[target] != "" }
	second, err := Prepare(cfg)
	if err != nil || second.Skipped != "" {
		t.Fatalf("second prepare = %+v %v", second, err)
	}
	if _, err := os.Lstat(p3Game); !os.IsNotExist(err) {
		t.Fatalf("deleted game was restored from FAT: %v", err)
	}
	if got := secondMounter.mounted[cfg.LibraryFATRoot]; got != cfg.LibraryDataRoot {
		t.Fatalf("game tree was not rebound after reboot: %q", got)
	}
}

func TestPrepareRepairsStaleGameBindSource(t *testing.T) {
	t.Parallel()
	for _, recreate := range []bool{false, true} {
		t.Run(fmt.Sprintf("recreated=%t", recreate), func(t *testing.T) {
			mounter := newRecordingMounter()
			cfg := testConfig(t, mounter)
			fatGame := filepath.Join(cfg.LibraryFATRoot, "sms", "recovered.sms")
			if err := os.MkdirAll(filepath.Dir(fatGame), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fatGame, []byte("fat-copy"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Prepare(cfg); err != nil {
				t.Fatal("initial prepare:", err)
			}

			if err := os.RemoveAll(cfg.LibraryDataRoot); err != nil {
				t.Fatal(err)
			}
			if recreate {
				if err := os.MkdirAll(cfg.LibraryDataRoot, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			result, err := Prepare(cfg)
			if err != nil || !slices.Contains(result.Bound, "games") {
				t.Fatalf("recovery prepare = %+v %v", result, err)
			}
			if len(mounter.unbinds) != 1 || mounter.unbinds[0] != cfg.LibraryFATRoot {
				t.Fatalf("stale bind unmounts = %v", mounter.unbinds)
			}
			if got, err := os.ReadFile(filepath.Join(cfg.LibraryDataRoot, "sms", "recovered.sms")); err != nil || string(got) != "fat-copy" {
				t.Fatalf("recovered p3 game = %q %v", got, err)
			}
			if got := mounter.mounted[cfg.LibraryFATRoot]; got != cfg.LibraryDataRoot {
				t.Fatalf("game tree was not rebound to p3: %q", got)
			}
		})
	}
}

func TestPrepareSkipsSymlinksAndDoesNotFollowThem(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("do-not-copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(cfg.FATRoot, "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cache, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(cfg.MountPoint, "cache", "link")); !os.IsNotExist(err) {
		t.Fatal("symlink was copied onto p3")
	}
}

func TestPrepareRejectsProtectedBindNames(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	cfg.BindNames = []string{"cache", "releases"}
	if _, err := Prepare(cfg); err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("protected bind accepted: %v", err)
	}
	if len(mounter.volumes) != 0 {
		t.Fatal("protected bind mounted p3")
	}
}

func TestPrepareIsIdempotentWhenAlreadyBound(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	first, err := Prepare(cfg)
	if err != nil || len(first.Bound) != len(DefaultBindNames)+1 {
		t.Fatalf("first = %+v %v", first, err)
	}
	second, err := Prepare(cfg)
	if err != nil || len(second.Bound) != len(DefaultBindNames)+1 {
		t.Fatalf("second = %+v %v", second, err)
	}
	if len(mounter.volumes) != 1 {
		t.Fatalf("remounted volume: %v", mounter.volumes)
	}
	if len(mounter.binds) != len(DefaultBindNames)+1 {
		t.Fatalf("rebound directories: %v", mounter.binds)
	}
}

func TestSelectedDevicePrefersLabelOverMMC(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	label := filepath.Join(root, "by-label", DefaultLabel)
	mmc := filepath.Join(root, "mmcblk0p3")
	if err := os.MkdirAll(filepath.Dir(label), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(label, []byte("not-ext4"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mmc, ext4Superblock(DefaultLabel), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := SelectedDevice(Config{Devices: []string{label, mmc}, Label: DefaultLabel})
	if !ok || got != label {
		t.Fatalf("selected %q ok=%v", got, ok)
	}
}

func TestSelectedDeviceAcceptsLabelledNonMMCDevice(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	usb := filepath.Join(root, "sda3")
	mmc := filepath.Join(root, "mmcblk0p3")
	if err := os.WriteFile(usb, ext4Superblock(DefaultLabel), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mmc, ext4Superblock("OTHER"), 0o600); err != nil {
		t.Fatal(err)
	}
	missingLabel := filepath.Join(root, "by-label", DefaultLabel)
	got, ok := SelectedDevice(Config{Devices: []string{missingLabel, usb, mmc}, Label: DefaultLabel})
	if !ok || got != usb {
		t.Fatalf("selected %q ok=%v", got, ok)
	}
}

func TestProductionDevicesPreferLabel(t *testing.T) {
	t.Parallel()
	cfg := ProductionConfig()
	if len(cfg.Devices) != 2 || cfg.Devices[0] != DefaultLabelDevice || cfg.Devices[1] != DefaultMMCDevice {
		t.Fatalf("devices = %v", cfg.Devices)
	}
}

func TestPrepareUsesByLabelWithoutReadingSuperblock(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	labelPath := filepath.Join(t.TempDir(), DefaultLabel)
	if err := os.WriteFile(labelPath, []byte("not-ext4"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Devices = []string{filepath.Join(filepath.Dir(labelPath), DefaultLabel)}
	cfg.ReadLabel = func(string) (string, error) {
		t.Fatal("by-label device should not require a superblock read")
		return "", nil
	}
	result, err := Prepare(cfg)
	if err != nil || result.Skipped != "" || result.Device != labelPath {
		t.Fatalf("by-label = %+v %v", result, err)
	}
}

func TestMMCDeviceRequiresMatchingLabel(t *testing.T) {
	t.Parallel()
	mounter := newRecordingMounter()
	cfg := testConfig(t, mounter)
	other := filepath.Join(t.TempDir(), "mmcblk0p3")
	if err := os.WriteFile(other, ext4Superblock("OTHER"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Devices = []string{other}
	result, err := Prepare(cfg)
	if err != nil || result.Skipped != SkipAbsent {
		t.Fatalf("mismatched label = %+v %v", result, err)
	}
}

func TestProductionBindNamesStayOnMutableData(t *testing.T) {
	t.Parallel()
	cfg := ProductionConfig()
	if err := validateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.BindNames, ","); got != "cache,saves,core-data,launcher-cache,evidence" {
		t.Fatalf("bind names = %s", got)
	}
	if cfg.FATRoot != DefaultFATRoot || cfg.Label != DefaultLabel {
		t.Fatalf("production paths = %+v", cfg)
	}
}

func TestExt4LabelFromSuperblock(t *testing.T) {
	t.Parallel()
	label, err := ext4LabelFromSuperblock(ext4Superblock(DefaultLabel))
	if err != nil || label != DefaultLabel {
		t.Fatalf("label = %q %v", label, err)
	}
	if _, err := ext4LabelFromSuperblock(make([]byte, 64)); err == nil {
		t.Fatal("truncated superblock accepted")
	}
}

func TestSelectedDeviceSkipsEmptyOpticalAndKeepsUSBAndFESDATA(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sysfs := filepath.Join(root, "sys")
	block := filepath.Join(sysfs, "class", "block")
	writeBlock(t, block, "sda", "0", "1", "5")
	writeBlock(t, block, "sdb", "15633408", "1", "0")
	writeBlock(t, block, "mmcblk0p3", "41943040", "1", "")

	optical := filepath.Join(root, "sda")
	if err := syscall.Mkfifo(optical, 0o600); err != nil {
		t.Fatal(err)
	}
	stick := filepath.Join(root, "sdb")
	data := filepath.Join(root, "mmcblk0p3")
	if err := os.WriteFile(stick, ext4Superblock(DefaultLabel), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(data, ext4Superblock(DefaultLabel), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := func(path string) error {
		return blockprobe.RejectsName(sysfs, filepath.Base(path))
	}
	opened := make(chan string, 3)
	readLabel := func(path string) (string, error) {
		opened <- filepath.Base(path)
		return readExt4Label(path)
	}

	done := make(chan struct{})
	var got string
	var ok bool
	go func() {
		defer close(done)
		got, ok = SelectedDevice(Config{
			Devices:   []string{optical, stick, data},
			Label:     DefaultLabel,
			Probe:     probe,
			ReadLabel: readLabel,
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("FESDATA selection blocked on the empty optical device")
	}
	if !ok || got != stick {
		t.Fatalf("selected %q ok=%v, want the USB stick", got, ok)
	}
	select {
	case name := <-opened:
		if name != "sdb" {
			t.Fatalf("first device read was %s", name)
		}
	default:
		t.Fatal("USB stick label was not read")
	}

	got, ok = SelectedDevice(Config{
		Devices:   []string{optical, data},
		Label:     DefaultLabel,
		Probe:     probe,
		ReadLabel: readLabel,
	})
	if !ok || got != data {
		t.Fatalf("selected %q ok=%v, want FESDATA", got, ok)
	}
	select {
	case name := <-opened:
		if name != "mmcblk0p3" {
			t.Fatalf("FESDATA read was %s", name)
		}
	default:
		t.Fatal("FESDATA label was not read")
	}
}

func writeBlock(t *testing.T, block, name, size, removable, scsiType string) {
	t.Helper()
	dir := filepath.Join(block, name, "device")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(block, name, "size"), []byte(size+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(block, name, "removable"), []byte(removable+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if scsiType != "" {
		if err := os.WriteFile(filepath.Join(dir, "type"), []byte(scsiType+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCopyMissingLeavesDestinationUntouchedOnExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a"), []byte("src"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "a"), []byte("dst"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyMissing(src, dst); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dst, "a"))
	if err != nil || string(body) != "dst" {
		t.Fatalf("overwrite = %q %v", body, err)
	}
}

type interruptedCopyReader struct{ sent bool }

func (r *interruptedCopyReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, []byte("partial")), nil
	}
	return 0, errors.New("copy interrupted")
}

func TestInterruptedMigrationCopyDoesNotPublishPartialDestination(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "fat")
	dst := filepath.Join(dir, "p3")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("complete FAT library file")
	if err := os.WriteFile(filepath.Join(src, "game.sms"), want, 0o600); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dst, "game.sms")
	if err := copyFileAtomically(&interruptedCopyReader{}, final); err == nil {
		t.Fatal("interrupted copy succeeded")
	}
	if _, err := os.Lstat(final); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial final path remains after interruption: %v", err)
	}

	// A later startup retries from the intact FAT source instead of treating a
	// partial p3 path as complete and binding it over the library.
	if err := copyMissing(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(final)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("retried copy = %q %v, want %q", got, err, want)
	}
}
