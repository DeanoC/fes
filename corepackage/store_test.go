package corepackage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func storeArchive(t *testing.T) ([]byte, Inspection) {
	t.Helper()
	c := loadCases(t)[0]
	manifest, payload := fixtureBytes(t, c)
	archive := canonicalArchive(manifest, payload)
	inspection, err := InspectPackage(writeArchive(t, archive))
	if err != nil {
		t.Fatal(err)
	}
	return archive, inspection
}

func writeArchive(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.fcore")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStoreImportReadListAndReopen(t *testing.T) {
	archive, expected := storeArchive(t)
	root := filepath.Join(t.TempDir(), "core-packages")
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, created, err := store.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || !created || !reflect.DeepEqual(got, expected) {
		t.Fatalf("Import = %#v, %v, %v", got, created, err)
	}
	info, err := os.Lstat(filepath.Join(root, expected.PackageID+".fcore"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o400 {
		t.Fatalf("installed archive = %#v, %v", info, err)
	}
	listed, err := store.List(context.Background())
	if err != nil || !reflect.DeepEqual(listed, []Inspection{expected}) {
		t.Fatalf("List = %#v, %v", listed, err)
	}
	readInspection, data, err := store.Read(context.Background(), expected.PackageID)
	if err != nil || !reflect.DeepEqual(readInspection, expected) || !bytes.Equal(data, archive) {
		t.Fatalf("Read = %#v, %d bytes, %v", readInspection, len(data), err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, created, err = reopened.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil || created || !reflect.DeepEqual(got, expected) {
		t.Fatalf("idempotent Import = %#v, %v, %v", got, created, err)
	}
}

func TestStoreConcurrentIdenticalImportPublishesOnce(t *testing.T) {
	archive, expected := storeArchive(t)
	store, err := NewStore(filepath.Join(t.TempDir(), "core-packages"))
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		inspection Inspection
		created    bool
		err        error
	}
	start := make(chan struct{})
	results := make(chan result, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			inspection, created, err := store.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive))
			results <- result{inspection, created, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	created := 0
	for result := range results {
		if result.err != nil || !reflect.DeepEqual(result.inspection, expected) {
			t.Fatalf("concurrent Import = %#v, %v", result.inspection, result.err)
		}
		if result.created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("created count = %d, want 1", created)
	}
}

func TestStoreRejectsInvalidCancellationAndLeavesNoPublication(t *testing.T) {
	archive, _ := storeArchive(t)
	for _, test := range []struct {
		name   string
		length int64
		body   []byte
		cancel bool
	}{
		{name: "truncated", length: int64(len(archive)), body: archive[:len(archive)-1]},
		{name: "invalid", length: 3, body: []byte("bad")},
		{name: "oversize", length: MaxArchiveSize + 1, body: archive},
		{name: "cancelled", length: int64(len(archive)), body: archive, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(filepath.Join(t.TempDir(), "core-packages"))
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if test.cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if _, _, err := store.Import(ctx, test.length, bytes.NewReader(test.body)); err == nil {
				t.Fatal("Import succeeded")
			} else if test.cancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Import error = %v, want context.Canceled", err)
				}
			} else if !errors.Is(err, ErrInvalidPackage) {
				t.Fatalf("Import error = %v, want ErrInvalidPackage", err)
			}
			entries, err := os.ReadDir(store.root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("store entries = %v, %v", entries, err)
			}
		})
	}
}

type cancelAfterFirstRead struct {
	reader *bytes.Reader
	cancel context.CancelFunc
	done   bool
}

func (r *cancelAfterFirstRead) Read(p []byte) (int, error) {
	if len(p) > 64 {
		p = p[:64]
	}
	n, err := r.reader.Read(p)
	if !r.done {
		r.done = true
		r.cancel()
	}
	return n, err
}

func TestStoreCancellationDuringReadLeavesNoPublication(t *testing.T) {
	archive, _ := storeArchive(t)
	store, err := NewStore(filepath.Join(t.TempDir(), "core-packages"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterFirstRead{reader: bytes.NewReader(archive), cancel: cancel}
	if _, _, err := store.Import(ctx, int64(len(archive)), reader); !errors.Is(err, context.Canceled) {
		t.Fatalf("Import error = %v, want context.Canceled", err)
	}
	entries, err := os.ReadDir(store.root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("store entries = %v, %v", entries, err)
	}
}

func TestStoreRejectsCorruptExistingAndSymlinkInventory(t *testing.T) {
	archive, expected := storeArchive(t)
	root := filepath.Join(t.TempDir(), "core-packages")
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(root, expected.PackageID+".fcore")
	if err := os.Chmod(installed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("corrupt"), 0o400); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Import(context.Background(), int64(len(archive)), bytes.NewReader(archive)); err == nil {
		t.Fatal("Import replaced corrupt existing archive")
	}
	if _, err := store.List(context.Background()); err == nil {
		t.Fatal("List accepted corrupt installed archive")
	}

	otherRoot := filepath.Join(t.TempDir(), "other")
	if err := os.WriteFile(otherRoot, archive, 0o400); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(t.TempDir(), "links")
	linked, err := NewStore(linkRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(otherRoot, filepath.Join(linkRoot, expected.PackageID+".fcore")); err != nil {
		t.Fatal(err)
	}
	if _, err := linked.List(context.Background()); err == nil {
		t.Fatal("List accepted symlink inventory")
	}
	if _, _, err := linked.Read(context.Background(), expected.PackageID); err == nil {
		t.Fatal("Read accepted symlink archive")
	}
}

func TestStoreRejectsInvalidRootAndPackageID(t *testing.T) {
	if _, err := NewStore("relative"); err == nil {
		t.Fatal("NewStore(relative) succeeded")
	}
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(link); err == nil {
		t.Fatal("NewStore(symlink) succeeded")
	}
	store, err := NewStore(filepath.Join(parent, "store"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Read(context.Background(), "../escape"); !errors.Is(err, ErrInvalidPackageID) {
		t.Fatalf("Read invalid ID error = %v", err)
	}
	if _, _, err := store.Read(context.Background(), strings.Repeat("a", 64)); !errors.Is(err, ErrPackageNotFound) {
		t.Fatalf("Read missing ID error = %v", err)
	}
}
