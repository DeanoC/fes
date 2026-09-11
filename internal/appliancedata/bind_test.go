package appliancedata

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordingMounter struct {
	mounted map[string]string
	volumes []string
	binds   []string
}

func newRecordingMounter() *recordingMounter {
	return &recordingMounter{mounted: map[string]string{}}
}

func (m *recordingMounter) MountVolume(source, target string) error {
	m.mounted[target] = source
	m.volumes = append(m.volumes, source+" -> "+target)
	return nil
}

func (m *recordingMounter) Bind(source, target string) error {
	m.mounted[target] = source
	m.binds = append(m.binds, source+" -> "+target)
	return nil
}

func testConfig(t *testing.T, mounter *recordingMounter) Config {
	t.Helper()
	root := t.TempDir()
	device := filepath.Join(root, "FESDATA3")
	if err := os.WriteFile(device, ext4Superblock(DefaultLabel), 0o600); err != nil {
		t.Fatal(err)
	}
	fat := filepath.Join(root, "fat", "fogcast")
	mount := filepath.Join(root, "run", "fesdata3")
	if err := os.MkdirAll(fat, 0o700); err != nil {
		t.Fatal(err)
	}
	return Config{
		Devices:       []string{device},
		Label:         DefaultLabel,
		MountPoint:    mount,
		FATRoot:       fat,
		BindNames:     append([]string(nil), DefaultBindNames...),
		EvaluateGate:  func() (Gate, error) { return Gate{}, nil },
		DevicePresent: pathExists,
		ReadLabel:     readExt4Label,
		Mounted:       func(target string) bool { return mounter.mounted[target] != "" },
		Mounter:       mounter,
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
	cfg.DevicePresent = func(string) bool { return false }
	result, err := Prepare(cfg)
	if err != nil || result.Skipped != SkipAbsent || len(result.Bound) != 0 {
		t.Fatalf("absent = %+v %v", result, err)
	}
	if len(mounter.volumes) != 0 || len(mounter.binds) != 0 {
		t.Fatalf("absent mutated mounts: %+v %+v", mounter.volumes, mounter.binds)
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
	if got := strings.Join(result.Bound, ","); got != strings.Join(DefaultBindNames, ",") {
		t.Fatalf("bound = %s", got)
	}
	if len(mounter.volumes) != 1 || !strings.Contains(mounter.volumes[0], cfg.MountPoint) {
		t.Fatalf("volume mounts = %v", mounter.volumes)
	}
	if len(mounter.binds) != len(DefaultBindNames) {
		t.Fatalf("binds = %v", mounter.binds)
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
	if err != nil || len(first.Bound) != len(DefaultBindNames) {
		t.Fatalf("first = %+v %v", first, err)
	}
	second, err := Prepare(cfg)
	if err != nil || len(second.Bound) != len(DefaultBindNames) {
		t.Fatalf("second = %+v %v", second, err)
	}
	if len(mounter.volumes) != 1 {
		t.Fatalf("remounted volume: %v", mounter.volumes)
	}
	if len(mounter.binds) != len(DefaultBindNames) {
		t.Fatalf("rebound directories: %v", mounter.binds)
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
