package software

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestInventoryMissingMembersAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remove    string
		inventory bool
	}{
		{"missing payload", RequiredExecutables[0], true},
		{"missing manifest", "manifest.json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, archive := fixture(t)
			store, err := New(privateRoot(t), compatibility(m))
			if err != nil {
				t.Fatal(err)
			}
			release, _, err := store.Install(context.Background(), bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(filepath.Join(store.Root(), "releases", release.ID, tc.remove)); err != nil {
				t.Fatal(err)
			}
			listed, err := store.List(context.Background())
			if tc.inventory {
				if err != nil || len(listed) != 1 || listed[0].ID != release.ID {
					t.Fatalf("manifest inventory: %v, %v", listed, err)
				}
			} else if err == nil {
				t.Fatal("inventory accepted a missing manifest")
			}
			if _, err := store.Read(context.Background(), release.ID); err == nil {
				t.Fatal("Read accepted an incomplete release")
			}
			if _, err := store.Path(context.Background(), release.ID); err == nil {
				t.Fatal("Path accepted an incomplete release")
			}
			if _, _, err := store.Install(context.Background(), bytes.NewReader(archive)); err == nil {
				t.Fatal("reinstall accepted the incomplete published release")
			}
		})
	}

	t.Run("directory does not match manifest", func(t *testing.T) {
		m, archive := fixture(t)
		store, err := New(privateRoot(t), compatibility(m))
		if err != nil {
			t.Fatal(err)
		}
		release, _, err := store.Install(context.Background(), bytes.NewReader(archive))
		if err != nil {
			t.Fatal(err)
		}
		other := "0000000000000000000000000000000000000000000000000000000000000000"
		if other == release.ID {
			t.Fatal("fixture identity collides")
		}
		if err := os.Rename(filepath.Join(store.Root(), "releases", release.ID), filepath.Join(store.Root(), "releases", other)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.List(context.Background()); err == nil {
			t.Fatal("inventory accepted a manifest under a different identity")
		}
		if _, err := store.Read(context.Background(), other); err == nil {
			t.Fatal("Read accepted a manifest under a different identity")
		}
		if _, err := store.Path(context.Background(), other); err == nil {
			t.Fatal("Path accepted a manifest under a different identity")
		}
	})
}

type blockedInventoryReader struct {
	reader  io.Reader
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockedInventoryReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.release })
	return r.reader.Read(p)
}

func awaitInventory[T any](t *testing.T, ch <-chan T, name string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(15 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
		var zero T
		return zero
	}
}

func TestInventoryAndUseWaitForInstallPublication(t *testing.T) {
	m, archive := fixture(t)
	store, err := New(privateRoot(t), compatibility(m))
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.ID()
	if err != nil {
		t.Fatal(err)
	}
	reader := &blockedInventoryReader{reader: bytes.NewReader(archive), started: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(reader.release) }) }
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	var worker sync.WaitGroup
	worker.Add(1)
	defer func() {
		unblock()
		cancel()
		done := make(chan struct{})
		go func() { worker.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("timed out waiting for installation to exit")
		}
	}()
	installed := make(chan error, 1)
	go func() { defer worker.Done(); _, _, err := store.Install(runCtx, reader); installed <- err }()
	select {
	case <-reader.started:
	case err := <-installed:
		t.Fatalf("installation returned before reading its archive: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for installation to read its archive")
	}

	for name, call := range map[string]func(context.Context) error{
		"list": func(ctx context.Context) error { _, err := store.List(ctx); return err },
		"read": func(ctx context.Context) error { _, err := store.Read(ctx, id); return err },
		"path": func(ctx context.Context) error { _, err := store.Path(ctx, id); return err },
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		err := call(ctx)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s while installation is incomplete: %v", name, err)
		}
	}
	unblock()
	if err := awaitInventory(t, installed, "installation"); err != nil {
		t.Fatal(err)
	}
	listed, err := store.List(runCtx)
	if err != nil || len(listed) != 1 || listed[0].ID != id {
		t.Fatalf("published inventory: %v, %v", listed, err)
	}
	if _, err := store.Read(runCtx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Path(runCtx, id); err != nil {
		t.Fatal(err)
	}
}
