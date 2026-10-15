package kitpackages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/corepackage"
)

func fixture(t *testing.T, version, core string) ([]byte, corepackage.Inspection) {
	t.Helper()
	directory := t.TempDir()
	manifest, err := os.ReadFile("../../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifest = bytes.ReplaceAll(manifest, []byte(`version = "0.1.0"`), []byte(`version = "`+version+`"`))
	manifest = bytes.ReplaceAll(manifest, []byte(`id = "fes.pong"`), []byte(`id = "`+core+`"`))
	payload, err := os.ReadFile("../../corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.toml"), manifest, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "core.rbf"), payload, 0o400); err != nil {
		t.Fatal(err)
	}
	archive, err := corepackage.CanonicalArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := corepackage.InspectPackage(directory)
	if err != nil {
		t.Fatal(err)
	}
	return archive, inspection
}

func newStore(t *testing.T) *Store {
	t.Helper()
	temporary := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(temporary, func(path string, entry os.DirEntry, err error) error {
			if err == nil && entry.IsDir() {
				return os.Chmod(path, 0o700)
			}
			return err
		})
	})
	s, err := New(filepath.Join(temporary, "library"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func install(t *testing.T, s *Store, archive []byte) corepackage.Inspection {
	t.Helper()
	inspection, _, err := s.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	return inspection
}

func TestInstallRetainsSealedMembersAndNoSelectionAcrossReopen(t *testing.T) {
	archive, expected := fixture(t, "0.1.0", "fes.pong")
	s := newStore(t)
	got, created, err := s.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || !created || !reflect.DeepEqual(got, expected) {
		t.Fatalf("install=%+v,%v,%v", got, created, err)
	}
	path, err := s.Path(context.Background(), expected.PackageID)
	if err != nil || path != filepath.Join(s.root, "packages", expected.PackageID) {
		t.Fatalf("path=%s,%v", path, err)
	}
	for _, entry := range []struct {
		path string
		mode os.FileMode
	}{{path, 0o500}, {filepath.Join(path, "manifest.toml"), 0o400}, {filepath.Join(path, "core.rbf"), 0o400}, {filepath.Join(s.root, "archives", expected.PackageID+".fcore"), 0o400}} {
		info, err := os.Stat(entry.path)
		if err != nil || info.Mode().Perm() != entry.mode {
			t.Fatalf("sealed %s=%v,%v", entry.path, info, err)
		}
	}
	selections, err := s.Selections(context.Background())
	if err != nil || len(selections) != 0 {
		t.Fatalf("install selected=%+v,%v", selections, err)
	}
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	got, created, err = reopened.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || created || !reflect.DeepEqual(got, expected) {
		t.Fatalf("retry=%+v,%v,%v", got, created, err)
	}
	list, err := reopened.List(context.Background())
	if err != nil || !reflect.DeepEqual(list, []corepackage.Inspection{expected}) {
		t.Fatalf("list=%+v,%v", list, err)
	}
	_, read, err := reopened.Read(context.Background(), expected.PackageID)
	if err != nil || !bytes.Equal(read, archive) {
		t.Fatalf("read=%v", err)
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "packages"))
	if err != nil || len(entries) != 1 || entries[0].Name() != expected.PackageID {
		t.Fatalf("permanent publications=%v,%v", entries, err)
	}
}

func TestInstallROMPackageRetainsAndAuthenticatesROMMap(t *testing.T) {
	directory := t.TempDir()
	for name, fixturePath := range map[string]string{
		"manifest.toml": "manifests/valid-basic.toml",
		"core.rbf":      "payloads/fes-fixture.rbf",
		"rom-map.json":  "maps/valid-basic.json",
	} {
		data, err := os.ReadFile("../../corepackage/testdata/core-bundle-v3/" + fixturePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o400); err != nil {
			t.Fatal(err)
		}
	}
	archive, err := corepackage.CanonicalArchive(directory)
	if err != nil {
		t.Fatal(err)
	}
	s := newStore(t)
	inspection := install(t, s, archive)
	path, err := s.Path(context.Background(), inspection.PackageID)
	if err != nil {
		t.Fatal(err)
	}
	mapPath := filepath.Join(path, "rom-map.json")
	if err := os.Chmod(mapPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapPath, []byte("{}"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(mapPath, 0o400); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Path(context.Background(), inspection.PackageID); err == nil {
		t.Fatal("corrupt ROM map admitted")
	}
}

func TestSelectionCASRetryCoreIdentityAndRetention(t *testing.T) {
	s := newStore(t)
	var packages []corepackage.Inspection
	for _, version := range []string{"0.1.0", "0.2.0", "0.3.0"} {
		archive, _ := fixture(t, version, "fes.pong")
		packages = append(packages, install(t, s, archive))
	}
	ctx := context.Background()
	first := packages[0].PackageID
	second := packages[1].PackageID
	if _, err := s.Select(ctx, "fes.other", "", first); !errors.Is(err, ErrCoreMismatch) {
		t.Fatalf("wrong core=%v", err)
	}
	if _, err := s.Select(ctx, "fes.pong", "", first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Select(ctx, "fes.pong", "", first); err != nil {
		t.Fatalf("lost response retry=%v", err)
	}
	if _, err := s.Select(ctx, "fes.pong", first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Select(ctx, "fes.pong", first, second); err != nil {
		t.Fatalf("upgrade retry=%v", err)
	}
	if _, err := s.Select(ctx, "fes.pong", first, packages[2].PackageID); !errors.Is(err, ErrSelectionConflict) {
		t.Fatalf("stale update=%v", err)
	}
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	selections, err := reopened.Selections(ctx)
	if err != nil || !reflect.DeepEqual(selections, []Selection{{CoreID: "fes.pong", PackageID: second}}) {
		t.Fatalf("selections=%v,%v", selections, err)
	}
	if _, _, err := reopened.Read(ctx, first); err != nil {
		t.Fatalf("earlier version removed=%v", err)
	}
	if _, err := reopened.Select(ctx, "fes.pong", second, first); err != nil {
		t.Fatalf("explicit prior version selection=%v", err)
	}
}

func TestConcurrentSelectionCASAcrossStoreInstances(t *testing.T) {
	s := newStore(t)
	archive, _ := fixture(t, "0.1.0", "fes.pong")
	first := install(t, s, archive)
	if _, err := s.Select(context.Background(), "fes.pong", "", first.PackageID); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, version := range []string{"0.2.0", "0.3.0"} {
		archive, _ := fixture(t, version, "fes.pong")
		ids = append(ids, install(t, s, archive).PackageID)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range ids {
		other, err := New(s.root)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			<-start
			_, err := other.Select(context.Background(), "fes.pong", first.PackageID, id)
			results <- err
		}()
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrSelectionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

func TestStoreProcessHelper(t *testing.T) {
	root := os.Getenv("FES_KITPACKAGES_TEST_ROOT")
	if root == "" {
		return
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(os.Getenv("FES_KITPACKAGES_TEST_ARCHIVE"))
	if err != nil {
		t.Fatal(err)
	}
	_, created, err := s.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Log("CREATED_PACKAGE")
	}
}

func TestConcurrentProcessesPublishExactlyOnce(t *testing.T) {
	archive, expected := fixture(t, "0.1.0", "fes.pong")
	s := newStore(t)
	archivePath := filepath.Join(t.TempDir(), "input.fcore")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	outputs := make(chan string, 6)
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestStoreProcessHelper$", "-test.v")
			cmd.Env = append(os.Environ(), "FES_KITPACKAGES_TEST_ROOT="+s.root, "FES_KITPACKAGES_TEST_ARCHIVE="+archivePath)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("child=%v: %s", err, output)
			}
			outputs <- string(output)
		}()
	}
	wg.Wait()
	close(outputs)
	created := 0
	for output := range outputs {
		if strings.Contains(output, "CREATED_PACKAGE") {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created=%d want1", created)
	}
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 || list[0].PackageID != expected.PackageID {
		t.Fatalf("list=%v,%v", list, err)
	}
}

func TestStoreRejectsTamperedOrMissingRetainedData(t *testing.T) {
	for _, scenario := range []string{"archive-corrupt", "member-corrupt", "member-missing", "member-writable", "member-symlink", "directory-symlink", "selection-missing-package", "selection-core-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			s := newStore(t)
			archive, expected := fixture(t, "0.1.0", "fes.pong")
			install(t, s, archive)
			if _, err := s.Select(context.Background(), "fes.pong", "", expected.PackageID); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.root, "packages", expected.PackageID)
			switch scenario {
			case "archive-corrupt":
				archivePath := filepath.Join(s.root, "archives", expected.PackageID+".fcore")
				if err := os.Chmod(archivePath, 0o600); err != nil {
					t.Fatal(err)
				}
				err := os.WriteFile(archivePath, []byte("broken"), 0o400)
				_ = os.Chmod(archivePath, 0o400)
				if err != nil {
					t.Fatal(err)
				}
			case "member-corrupt":
				if err := os.Chmod(filepath.Join(path, "core.rbf"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(path, "core.rbf"), []byte("broken"), 0o400); err != nil {
					t.Fatal(err)
				}
				_ = os.Chmod(filepath.Join(path, "core.rbf"), 0o400)
			case "member-missing":
				_ = os.Chmod(path, 0o700)
				if err := os.Remove(filepath.Join(path, "core.rbf")); err != nil {
					t.Fatal(err)
				}
				_ = os.Chmod(path, 0o500)
			case "member-writable":
				if err := os.Chmod(filepath.Join(path, "core.rbf"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "member-symlink":
				_ = os.Chmod(path, 0o700)
				if err := os.Remove(filepath.Join(path, "core.rbf")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("manifest.toml", filepath.Join(path, "core.rbf")); err != nil {
					t.Fatal(err)
				}
				_ = os.Chmod(path, 0o500)
			case "directory-symlink":
				if err := os.Rename(path, path+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-old", path); err != nil {
					t.Fatal(err)
				}
			case "selection-missing-package":
				_ = os.Chmod(path, 0o700)
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
			case "selection-core-mismatch":
				data, _ := json.Marshal(selectionFile{Version: 1, Selections: []Selection{{CoreID: "fes.other", PackageID: expected.PackageID}}})
				if err := os.WriteFile(filepath.Join(s.root, "selections.json"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Selections(context.Background()); err == nil {
				t.Fatal("tampered selection admitted")
			}
			if _, _, err := s.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err == nil {
				t.Fatal("reinstall concealed selected-data corruption")
			}
			if scenario != "selection-core-mismatch" {
				if _, err := s.Path(context.Background(), expected.PackageID); err == nil {
					t.Fatal("tampered launch path admitted")
				}
				if _, err := s.List(context.Background()); err == nil {
					t.Fatal("tampered inventory admitted")
				}
			}
		})
	}
}

func TestStoreRejectsChangedRootsLocksAndSymlinkAncestors(t *testing.T) {
	for _, name := range []string{"root", "archives", "packages", ".lock"} {
		t.Run(name, func(t *testing.T) {
			s := newStore(t)
			path := s.root
			if name != "root" {
				path = filepath.Join(path, name)
			}
			if err := os.Rename(path, path+"-old"); err != nil {
				t.Fatal(err)
			}
			if name == ".lock" {
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if _, err := s.List(context.Background()); err == nil {
				t.Fatal("replacement admitted")
			}
		})
	}
	parent := t.TempDir()
	if err := os.Symlink(parent, filepath.Join(parent, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(filepath.Join(parent, "alias", "library")); err == nil {
		t.Fatal("symlink ancestor admitted")
	}
	if _, err := os.Stat(filepath.Join(parent, "library")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created root through symlink")
	}
}

func TestCancelledWaitingForLockAndInvalidInstall(t *testing.T) {
	s := newStore(t)
	_, unlock, err := s.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = s.List(ctx)
	unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked lock cancellation=%v", err)
	}
	archive, _ := fixture(t, "0.1.0", "fes.pong")
	for _, test := range []struct {
		length int64
		data   []byte
	}{{int64(len(archive)), archive[:len(archive)-1]}, {3, []byte("bad")}, {corepackage.MaxArchiveSize + 1, archive}} {
		if _, _, err := s.Install(context.Background(), test.length, bytes.NewReader(test.data)); err == nil {
			t.Fatal("invalid import accepted")
		}
	}
	list, err := s.List(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("invalid import retained=%v,%v", list, err)
	}
}

func TestInterruptedPublicationCanBeCompletedByInstallRetry(t *testing.T) {
	s := newStore(t)
	archive, expected := fixture(t, "0.1.0", "fes.pong")
	if _, _, err := s.archives.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	staged, err := corepackage.Stage(context.Background(), filepath.Join(s.root, "packages"), int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	// Simulate process death after retaining the archive and staging members,
	// before the canonical directory has been published.
	_, created, err := s.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || !created {
		t.Fatalf("complete retry=%v,%v", created, err)
	}
	if _, err := os.Lstat(staged.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("abandoned publication retained")
	}
	entries, err := os.ReadDir(filepath.Join(s.root, "packages"))
	if err != nil || len(entries) != 1 || entries[0].Name() != expected.PackageID {
		t.Fatalf("inventory=%v,%v", entries, err)
	}
}

func TestReopenCompletesUnselectedArchiveOnlyInstall(t *testing.T) {
	s := newStore(t)
	archive, expected := fixture(t, "0.1.0", "fes.pong")
	if _, _, err := s.archives.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	staged, err := corepackage.Stage(context.Background(), filepath.Join(s.root, "packages"), int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	// Opening after process/OS restart completes the interrupted install without
	// requiring the host that uploaded it to retry or claim hardware execution.
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	list, err := reopened.List(context.Background())
	if err != nil || !reflect.DeepEqual(list, []corepackage.Inspection{expected}) {
		t.Fatalf("recovered inventory=%v,%v", list, err)
	}
	selections, err := reopened.SelectedPackages(context.Background())
	if err != nil || len(selections) != 0 {
		t.Fatalf("recovery selected a core=%v,%v", selections, err)
	}
	if _, err := os.Lstat(staged.Directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("abandoned publication was retained")
	}
	_, created, err := reopened.Install(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || created {
		t.Fatalf("lost upload reply retry=%v,%v", created, err)
	}
}

func TestReopenPreservesSelectedMissingOrCorruptData(t *testing.T) {
	for _, scenario := range []string{"missing", "corrupt"} {
		t.Run(scenario, func(t *testing.T) {
			s := newStore(t)
			archive, expected := fixture(t, "0.1.0", "fes.pong")
			install(t, s, archive)
			if _, err := s.Select(context.Background(), "fes.pong", "", expected.PackageID); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(s.root, "packages", expected.PackageID)
			if scenario == "missing" {
				if err := os.Chmod(path, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
			} else {
				member := filepath.Join(path, "core.rbf")
				if err := os.Chmod(member, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(member, []byte("broken"), 0o400); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(member, 0o400); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := New(s.root); err == nil {
				t.Fatal("selected corruption was silently repaired at startup")
			}
			if scenario == "missing" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("selected missing directory recreated")
				}
			} else {
				body, err := os.ReadFile(filepath.Join(path, "core.rbf"))
				if err != nil || string(body) != "broken" {
					t.Fatalf("selected corruption overwritten=%q,%v", body, err)
				}
			}
			if _, err := s.SelectedPackages(context.Background()); err == nil {
				t.Fatal("selected corruption yielded a local launch path")
			}
		})
	}
}

func TestSelectedListingTrustsInstallVerification(t *testing.T) {
	s := newStore(t)
	archive, expected := fixture(t, "0.1.0", "fes.pong")
	install(t, s, archive)
	if _, err := s.Select(context.Background(), "fes.pong", "", expected.PackageID); err != nil {
		t.Fatal(err)
	}
	fullVerificationReads.Store(0)
	for range 3 {
		selected, err := s.SelectedPackages(context.Background())
		if err != nil || len(selected) != 1 || selected[0].PackageID != expected.PackageID {
			t.Fatalf("selected=%+v,%v", selected, err)
		}
		listed, err := s.List(context.Background())
		if err != nil || len(listed) != 1 || listed[0].PackageID != expected.PackageID {
			t.Fatalf("list=%+v,%v", listed, err)
		}
		if _, err := s.Selections(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := fullVerificationReads.Load(); got != 0 {
		t.Fatalf("listing re-read payloads %d times", got)
	}
	reopened, err := New(s.root)
	if err != nil {
		t.Fatal(err)
	}
	fullVerificationReads.Store(0)
	if _, err := reopened.SelectedPackages(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fullVerificationReads.Load(); got != 0 {
		t.Fatalf("reopened listing re-read payloads %d times", got)
	}
	if _, err := s.Path(context.Background(), expected.PackageID); err != nil {
		t.Fatal(err)
	}
	if fullVerificationReads.Load() == 0 {
		t.Fatal("launch path skipped payload inspection")
	}
}

func rewriteSameSizeRestoreMtime(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) == 0 {
		t.Fatal("empty package member")
	}
	rewritten := bytes.Clone(original)
	rewritten[len(rewritten)/2] ^= 0xff
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, rewritten, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

func TestSameSizeRewriteWithRestoredMtimeMissesVerificationCache(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("listing ctime comes from Linux stat")
	}
	for _, member := range []string{"archive", "member"} {
		t.Run(member, func(t *testing.T) {
			s := newStore(t)
			archive, expected := fixture(t, "0.1.0", "fes.pong")
			install(t, s, archive)
			if _, err := s.Select(context.Background(), "fes.pong", "", expected.PackageID); err != nil {
				t.Fatal(err)
			}
			fullVerificationReads.Store(0)
			if _, err := s.List(context.Background()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SelectedPackages(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := fullVerificationReads.Load(); got != 0 {
				t.Fatalf("warm listing re-read payloads %d times", got)
			}
			path := filepath.Join(s.root, "archives", expected.PackageID+".fcore")
			if member == "member" {
				path = filepath.Join(s.root, "packages", expected.PackageID, "core.rbf")
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			beforeDev, beforeIno, beforeCtime := devOf(before), inodeOf(before), ctimeOf(before)
			rewriteSameSizeRestoreMtime(t, path)
			after, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) || after.Mode().Perm() != before.Mode().Perm() {
				t.Fatalf("rewrite changed size, mtime, or mode")
			}
			if devOf(after) != beforeDev || inodeOf(after) != beforeIno {
				t.Fatalf("rewrite changed device or inode: dev %d->%d ino %d->%d", beforeDev, devOf(after), beforeIno, inodeOf(after))
			}
			if ctimeOf(after) == beforeCtime {
				t.Fatal("same-size rewrite left ctime unchanged, so the fingerprint cannot see it")
			}
			root, err := os.OpenRoot(s.root)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			key, err := packageStatKey(root, expected.PackageID)
			if err != nil || !strings.Contains(key, fmt.Sprintf("ctime=%d", ctimeOf(after))) {
				t.Fatalf("stat key omitted the new ctime: %s %v", key, err)
			}
			fullVerificationReads.Store(0)
			if _, err := s.List(context.Background()); err == nil {
				t.Fatal("same-size rewrite stayed in the authenticated list")
			} else if member == "archive" && !strings.HasPrefix(err.Error(), "core package:") {
				t.Fatalf("archive rewrite was rejected before its bytes were checked: %v", err)
			}
			if member == "member" && fullVerificationReads.Load() == 0 {
				t.Fatal("listing trusted the cached verification after a same-size member rewrite")
			}
			if _, err := s.SelectedPackages(context.Background()); err == nil {
				t.Fatal("same-size rewrite stayed in the selected inventory")
			}
			if _, _, err := s.Read(context.Background(), expected.PackageID); err == nil {
				t.Fatal("read accepted rewritten bytes")
			}
		})
	}
}

func TestSelectedPackagesReturnsAuthenticatedIdentityAndLocalPath(t *testing.T) {
	s := newStore(t)
	archive, expected := fixture(t, "0.1.0", "fes.pong")
	install(t, s, archive)
	if _, err := s.Select(context.Background(), "fes.pong", "", expected.PackageID); err != nil {
		t.Fatal(err)
	}
	selected, err := s.SelectedPackages(context.Background())
	want := []SelectedPackage{{Selection: Selection{CoreID: "fes.pong", PackageID: expected.PackageID}, Inspection: expected, Path: filepath.Join(s.root, "packages", expected.PackageID)}}
	if err != nil || !reflect.DeepEqual(selected, want) {
		t.Fatalf("selected local objects=%+v,%v", selected, err)
	}
	wire, err := json.Marshal(selected)
	if err != nil || bytes.Contains(wire, []byte(s.root)) {
		t.Fatalf("local path leaked into JSON=%s,%v", wire, err)
	}
}
