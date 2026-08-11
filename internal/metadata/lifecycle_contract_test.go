package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gosqlite.org/vfs"
)

func TestContractBoundedCloseRetainsOwnershipUntilReaperRelease(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	started := make(chan struct{})
	provider := &fakeProvider{
		started: started,
		result: ProviderResult{
			PlatformID: "58",
			Candidates: []Candidate{{ProviderID: "1", Name: "Sonic", PlatformIDs: []string{"58"}}},
		},
	}
	runtimeValue, err := Open(context.Background(), RuntimeConfig{Root: root, Configured: true, Enabled: true, ProviderName: ProviderIGDB, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	runtimeImpl := runtimeValue.(*metadataRuntime)
	vfsName := runtimeImpl.cache.sqliteVFSName
	syncStarted := make(chan struct{})
	releaseSync := make(chan struct{})
	restoreHooks := runtimeImpl.cache.sqliteVFS.setHooksForTest(sqliteVFSHooks{
		beforeSync: func(_ string) {
			select {
			case <-syncStarted:
			default:
				close(syncStarted)
			}
			<-releaseSync
		},
	})
	defer restoreHooks()

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

	startedAt := time.Now()
	firstCloseErr := runtimeValue.Close()
	if elapsed := time.Since(startedAt); elapsed > 2500*time.Millisecond {
		t.Fatalf("bounded Close exceeded deadline: %s", elapsed)
	}
	if opCode(firstCloseErr) != ErrStorage {
		t.Fatalf("bounded Close error = %v, want storage", firstCloseErr)
	}
	if secondCloseErr := runtimeValue.Close(); opCode(secondCloseErr) != opCode(firstCloseErr) {
		t.Fatalf("repeated Close changed result: first=%v second=%v", firstCloseErr, secondCloseErr)
	}

	runtimeImpl.mu.Lock()
	reaper := runtimeImpl.reaper
	runtimeImpl.mu.Unlock()
	if reaper == nil {
		t.Fatal("bounded Close did not retain an explicit reaper")
	}
	if _, ok := vfs.Find(vfsName); !ok {
		t.Fatalf("VFS %q was released before the blocked operation completed", vfsName)
	}
	if _, err := runtimeImpl.cache.sqliteRoot.Stat("."); err != nil {
		t.Fatalf("metadata root was closed before reaper release: %v", err)
	}

	close(releaseSync)
	select {
	case <-lookupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked lookup was not released")
	}
	if lookupErr == nil || (!errors.Is(lookupErr, context.Canceled) && opCode(lookupErr) != ErrCanceled) {
		t.Fatalf("lookup after Close = %v; want cancellation", lookupErr)
	}
	select {
	case <-reaper.done:
	case <-time.After(2 * time.Second):
		t.Fatal("retained reaper did not complete")
	}
	if _, ok := vfs.Find(vfsName); ok {
		t.Fatalf("VFS %q remained registered after reaper completion", vfsName)
	}
	if _, err := runtimeImpl.cache.sqliteRoot.Stat("."); err == nil {
		t.Fatal("metadata root remained open after reaper completion")
	}
	if laterCloseErr := runtimeValue.Close(); opCode(laterCloseErr) != opCode(firstCloseErr) {
		t.Fatalf("post-release Close changed result: first=%v later=%v", firstCloseErr, laterCloseErr)
	}

	reopened, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatalf("reopen after retained reaper release: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestContractActualGosqliteRetainedRetryReleasesOwnerAndPermitsReopen(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	first, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope-a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	before := sqliteVFSSequence.Load()
	targetSequence := before + 2
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var blocker *sql.DB
	var blockerErr error
	openingBlocker := false
	restore := setSQLiteVFSHooksForTest(sqliteVFSHooks{
		afterOpen: func(name string, _ *os.File) {
			if !strings.HasSuffix(name, "cache.sqlite3") || sqliteVFSSequence.Load() != targetSequence {
				return
			}
			mu.Lock()
			if blocker != nil || openingBlocker {
				mu.Unlock()
				return
			}
			openingBlocker = true
			mu.Unlock()

			vfsName := fmt.Sprintf("fogcast-cache-%d", targetSequence)
			candidate, openErr := sql.Open("sqlite", "file:cache.sqlite3?vfs="+vfsName)
			if openErr == nil {
				openErr = candidate.Ping()
			}
			mu.Lock()
			blocker = candidate
			blockerErr = openErr
			openingBlocker = false
			mu.Unlock()
			if openErr == nil {
				cancel()
			}
		},
	})
	second, openErr := OpenCache(ctx, CacheConfig{Root: root, CredentialScope: "scope-b"})
	restore()
	if second != nil || opCode(openErr) != ErrStorage {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("retained retry = cache:%v err:%v", second, openErr)
	}

	mu.Lock()
	retained := blocker
	retainedErr := blockerErr
	mu.Unlock()
	if retained == nil || retainedErr != nil {
		t.Fatalf("actual gosqlite blocker = db:%v err:%v", retained, retainedErr)
	}
	name := fmt.Sprintf("fogcast-cache-%d", targetSequence)
	registered, ok := vfs.Find(name)
	if !ok {
		_ = retained.Close()
		t.Fatalf("retained VFS %q was released before blocker close", name)
	}
	rooted, ok := registered.(*rootedSQLiteVFS)
	if !ok || rooted == nil {
		_ = retained.Close()
		t.Fatalf("registered VFS %q has unexpected type %T", name, registered)
	}
	if _, err := rooted.root.Stat("."); err != nil {
		_ = retained.Close()
		t.Fatalf("retained VFS root was released early: %v", err)
	}
	artworkRoot, err := rooted.root.OpenRoot("artwork")
	if err != nil {
		_ = retained.Close()
		t.Fatalf("retained artwork child was released early: %v", err)
	}
	if _, err := artworkRoot.Stat("."); err != nil {
		_ = artworkRoot.Close()
		_ = retained.Close()
		t.Fatalf("retained artwork child identity is unavailable: %v", err)
	}
	_ = artworkRoot.Close()
	if blocked, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"}); blocked != nil || opCode(err) != ErrStorage {
		if blocked != nil {
			_ = blocked.Close()
		}
		_ = retained.Close()
		t.Fatalf("same-root open while retained = cache:%v err:%v", blocked, err)
	}
	if runtimeValue, err := Open(context.Background(), RuntimeConfig{Root: root, Configured: false, Enabled: false}); runtimeValue != nil || opCode(err) != ErrStorage {
		if runtimeValue != nil {
			_ = runtimeValue.Close()
		}
		_ = retained.Close()
		t.Fatalf("disabled purge while retained = runtime:%v err:%v", runtimeValue, err)
	}

	if err := retained.Close(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := vfs.Find(name); !ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, ok := vfs.Find(name); ok {
		t.Fatalf("retained VFS %q was not eventually unregistered", name)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && sqliteRetirementCount() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if count := sqliteRetirementCount(); count != 0 {
		t.Fatalf("retirement queue retained %d records after release", count)
	}
	reopened, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatalf("reopen after actual gosqlite blocker drained: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestContractRetirementHelperReleasesPrecommitOwnerAfterUnregister(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	owner, err := acquirePrivateRoot(root, true)
	if err != nil {
		t.Fatal(err)
	}
	heldRoot := owner.root
	lease, err := acquireCacheRootLease(owner.info)
	if err != nil {
		_ = owner.abort(true)
		t.Fatal(err)
	}
	artwork, err := openArtworkDirectory(owner, true)
	if err != nil {
		lease.release()
		_ = owner.abort(true)
		t.Fatal(err)
	}
	rooted := newRootedSQLiteVFS(owner.root)
	if err := registerRootedSQLiteVFS(rooted); err != nil {
		_ = artwork.removeCreated()
		_ = artwork.close()
		lease.release()
		_ = owner.abort(true)
		t.Fatal(err)
	}
	for _, name := range sqliteDerivedLeaves {
		file, createErr := owner.root.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0o600)
		if createErr != nil {
			t.Fatal(createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}

	retry, retireErr := retireSQLiteRecord(newSQLiteRetirement(owner, artwork, lease, rooted, true, true))
	if retry || retireErr != nil {
		t.Fatalf("precommit retirement = retry:%v err:%v", retry, retireErr)
	}
	if rootedSQLiteVFSRegistered(rooted) {
		t.Fatal("precommit retirement left VFS registered")
	}
	if _, err := heldRoot.Stat("."); err == nil {
		t.Fatal("precommit retirement left root descriptor open")
	}
	reopened, err := OpenCache(context.Background(), CacheConfig{Root: root, CredentialScope: "scope"})
	if err != nil {
		t.Fatalf("reopen after direct precommit retirement: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestContractRetirementClosesOwnedDatabaseBeforeUnregister(t *testing.T) {
	root := filepath.Join(t.TempDir(), "metadata")
	owner, err := acquirePrivateRoot(root, true)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := acquireCacheRootLease(owner.info)
	if err != nil {
		_ = owner.abort(true)
		t.Fatal(err)
	}
	artwork, err := openArtworkDirectory(owner, true)
	if err != nil {
		lease.release()
		_ = owner.abort(true)
		t.Fatal(err)
	}
	rooted := newRootedSQLiteVFS(owner.root)
	if err := registerRootedSQLiteVFS(rooted); err != nil {
		_ = artwork.removeCreated()
		_ = artwork.close()
		lease.release()
		_ = owner.abort(true)
		t.Fatal(err)
	}

	database, err := sql.Open("sqlite", "file:cache.sqlite3?vfs="+url.QueryEscape(rooted.logicalPrefix))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Ping(); err != nil {
		_ = database.Close()
		_ = unregisterRootedSQLiteVFS(rooted)
		_ = artwork.close()
		lease.release()
		_ = owner.abort(true)
		t.Fatal(err)
	}

	record := &sqliteRetirement{
		owner:   owner,
		artwork: artwork,
		lease:   lease,
		vfs:     rooted,
		db:      database,
		done:    make(chan struct{}),
	}
	heldRoot := owner.root
	retry, retireErr := retireSQLiteRecord(record)
	if retry || retireErr != nil {
		t.Fatalf("owned database retirement = retry:%v err:%v", retry, retireErr)
	}
	if rootedSQLiteVFSRegistered(rooted) {
		t.Fatal("owned database retirement left VFS registered")
	}
	if _, err := heldRoot.Stat("."); err == nil {
		t.Fatal("owned database retirement left root descriptor open")
	}
}
