package metadata

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestContractCloseIsBoundedBeforeContendedPublicationAndReaps(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	started := make(chan struct{})
	provider := &fakeProvider{
		started: started,
		result: ProviderResult{
			PlatformID: "58",
			Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}}},
		},
	}
	runtimeValue, err := Open(context.Background(), RuntimeConfig{
		Root: root, Configured: true, Enabled: true,
		ProviderName: ProviderIGDB, Provider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	runtimeValueImpl := runtimeValue.(*metadataRuntime)

	syncStarted := make(chan struct{})
	releaseSync := make(chan struct{})
	var syncOnce chan struct{} = syncStarted
	restore := runtimeValueImpl.cache.sqliteVFS.setHooksForTest(sqliteVFSHooks{
		beforeSync: func(_ string) {
			if syncOnce != nil {
				close(syncOnce)
				syncOnce = nil
			}
			<-releaseSync
		},
	})
	defer restore()

	lookupDone := make(chan struct{})
	var lookupErr error
	go func() {
		_, lookupErr = runtimeValue.Lookup(context.Background(), LookupInput{Title: "Sonic", System: "megadrive"})
		close(lookupDone)
	}()
	<-started
	select {
	case <-syncStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not reach the rooted VFS sync hook")
	}

	began := time.Now()
	firstCloseErr := runtimeValue.Close()
	elapsed := time.Since(began)
	if elapsed > 2500*time.Millisecond {
		t.Fatalf("Close exceeded its two-second bound before contended publication: %s (err=%v)", elapsed, firstCloseErr)
	}
	if secondCloseErr := runtimeValue.Close(); opCode(secondCloseErr) != opCode(firstCloseErr) {
		t.Fatalf("repeated Close changed result: first=%v second=%v", firstCloseErr, secondCloseErr)
	}

	close(releaseSync)
	select {
	case <-lookupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("contended lookup was not reaped after the VFS sync was released")
	}
	if lookupErr == nil || (!errors.Is(lookupErr, context.Canceled) && opCode(lookupErr) != ErrCanceled) {
		var operation *OpError
		if errors.As(lookupErr, &operation) {
			t.Logf("lookup operation cause: %v", operation.cause)
		}
		t.Fatalf("lookup after Close = %v; want cancellation", lookupErr)
	}
	waitForCacheClosed(t, runtimeValueImpl.cache)

	reopened, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: ""})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	key, err := BuildCacheKey(ProviderIGDB, "58", "sonic", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.Get(key); err != nil {
		t.Fatal(err)
	} else if ok {
		t.Fatal("publication that was canceled by Close remained in the cache")
	}
}

func waitForCacheClosed(t *testing.T, cache *Cache) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cache.closeMu.RLock()
		closed := cache.closed
		cache.closeMu.RUnlock()
		if closed {
			return
		}
		runtime.Gosched()
	}
	t.Fatal("cache close reaper did not finish")
}

func TestContractPurgeFailsClosedOnParentSwapForDisabledAndUnconfigured(t *testing.T) {
	for _, config := range []RuntimeConfig{
		{Configured: false, Enabled: false},
		{Configured: true, Enabled: false},
	} {
		name := "unconfigured"
		if config.Configured {
			name = "disabled"
		}
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			parent := filepath.Join(base, "private-parent")
			root := filepath.Join(parent, "metadata")
			if err := os.MkdirAll(filepath.Join(root, "artwork"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "cache.sqlite3"), []byte("derived"), 0o600); err != nil {
				t.Fatal(err)
			}

			external := t.TempDir()
			externalArtwork := filepath.Join(external, "artwork")
			if err := os.MkdirAll(externalArtwork, 0o700); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(externalArtwork, "must-remain")
			if err := os.WriteFile(sentinel, []byte("redirected"), 0o600); err != nil {
				t.Fatal(err)
			}

			restore := setPurgeBeforeDeleteHookForTest(func() {
				moved := parent + ".moved"
				if err := os.Rename(parent, moved); err != nil {
					t.Fatalf("move validated parent: %v", err)
				}
				if err := os.Symlink(external, parent); err != nil {
					t.Fatalf("swap validated parent: %v", err)
				}
			})
			runtimeValue, err := Open(context.Background(), RuntimeConfig{
				Root: root, Configured: config.Configured, Enabled: config.Enabled, ProviderName: ProviderIGDB,
			})
			restore()
			if runtimeValue != nil || opCode(err) != ErrStorage {
				t.Fatalf("parent-swap purge = runtime:%v err:%v", runtimeValue, err)
			}
			if content, err := os.ReadFile(sentinel); err != nil || string(content) != "redirected" {
				t.Fatalf("redirected artwork changed: %q err=%v", content, err)
			}
		})
	}
}
