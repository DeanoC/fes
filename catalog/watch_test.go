package catalog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/protocol"
)

func TestFolderWatcherReconcileAddsAndRemovesSNESROMs(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	store := openScannerStore(t)
	root := Root{ID: "snes-main", System: protocol.SystemSNES, Path: rootPath}
	watcher := FolderWatcher{
		Scan:  Scanner{Store: store, Platforms: DefaultPlatforms()}.Scan,
		Roots: func() []Root { return []Root{root} },
	}

	mustWriteScannerFile(t, filepath.Join(rootPath, "Axelay.sfc"), []byte("axelay"))
	if _, err := watcher.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile(add): %v", err)
	}
	games := scannerGames(t, store)
	if len(games) != 1 || games[0].State != SourceStateAvailable || games[0].RelativePath != "Axelay.sfc" {
		t.Fatalf("after add = %+v", games)
	}

	if err := os.Remove(filepath.Join(rootPath, "Axelay.sfc")); err != nil {
		t.Fatal(err)
	}
	if _, err := watcher.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile(remove): %v", err)
	}
	games = scannerGames(t, store)
	if len(games) != 1 || games[0].State != SourceStateMissing || games[0].RelativePath != "Axelay.sfc" {
		t.Fatalf("after remove = %+v", games)
	}
}

func TestFolderWatcherRootsCallbackChangesWatchedFolder(t *testing.T) {
	ctx := context.Background()
	first := t.TempDir()
	second := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(first, "First.sfc"), []byte("first"))
	mustWriteScannerFile(t, filepath.Join(second, "Second.sfc"), []byte("second"))
	store := openScannerStore(t)
	scan := Scanner{Store: store, Platforms: DefaultPlatforms()}.Scan
	firstWatcher := FolderWatcher{
		Scan:  scan,
		Roots: func() []Root { return []Root{{ID: "watch-a", System: protocol.SystemSNES, Path: first}} },
	}
	secondWatcher := FolderWatcher{
		Scan:  scan,
		Roots: func() []Root { return []Root{{ID: "watch-b", System: protocol.SystemSNES, Path: second}} },
	}

	if _, err := firstWatcher.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile(first): %v", err)
	}
	if paths := scannerGamePaths(scannerGames(t, store)); len(paths) != 1 || paths[0] != "First.sfc" {
		t.Fatalf("first watch paths = %v", paths)
	}

	if _, err := secondWatcher.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile(second): %v", err)
	}
	games := scannerGames(t, store)
	available := map[string]string{}
	for _, game := range games {
		if game.State == SourceStateAvailable {
			available[game.LibraryID] = game.RelativePath
		}
	}
	if available["watch-a"] != "First.sfc" || available["watch-b"] != "Second.sfc" {
		t.Fatalf("watch roots did not stay config-driven: %v from %+v", available, games)
	}
}

func TestFolderWatcherRootsKeepOnlySupportedSystems(t *testing.T) {
	var watched []Root
	watcher := FolderWatcher{
		Scan: func(_ context.Context, roots []Root) (ScanReport, error) {
			watched = append([]Root(nil), roots...)
			return ScanReport{}, nil
		},
		Roots: func() []Root {
			return []Root{
				{ID: "snes-main", System: protocol.SystemSNES},
				{ID: "genesis-main", System: protocol.SystemMegaDrive},
				{ID: "unknown-main", System: protocol.System("mystery")},
			}
		},
	}
	if _, err := watcher.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(watched) != 2 || watched[0].System != protocol.SystemSNES || watched[1].System != protocol.SystemMegaDrive {
		t.Fatalf("watched roots = %#v", watched)
	}
}

func TestFolderWatcherIgnoresUnsupportedRoots(t *testing.T) {
	ctx := context.Background()
	rootPath := t.TempDir()
	mustWriteScannerFile(t, filepath.Join(rootPath, "game.rom"), []byte("unsupported"))
	store := openScannerStore(t)
	watcher := FolderWatcher{
		Scan: Scanner{Store: store, Platforms: DefaultPlatforms()}.Scan,
		Roots: func() []Root {
			return []Root{{ID: "unknown-main", System: protocol.System("mystery"), Path: rootPath}}
		},
	}
	if _, err := watcher.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if games := scannerGames(t, store); len(games) != 0 {
		t.Fatalf("non-SNES watch indexed %v", scannerGamePaths(games))
	}
}

func TestFolderWatcherRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	watcher := FolderWatcher{
		Scan: func(context.Context, []Root) (ScanReport, error) {
			calls++
			return ScanReport{}, nil
		},
		Roots:    func() []Root { return []Root{{ID: "snes-main", System: protocol.SystemSNES, Path: t.TempDir()}} },
		Interval: 20 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() { done <- watcher.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Run = %v, want canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
	if calls < 1 {
		t.Fatal("Run never reconciled")
	}
}

func TestFolderWatcherRunReportsPersistentErrorsAndKeepsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var calls int32
	var failures int32
	watcher := FolderWatcher{
		Scan: func(context.Context, []Root) (ScanReport, error) {
			atomic.AddInt32(&calls, 1)
			return ScanReport{}, errors.New("transient smb")
		},
		Roots:    func() []Root { return []Root{{ID: "snes-main", System: protocol.SystemSNES, Path: t.TempDir()}} },
		Interval: 15 * time.Millisecond,
		OnError:  func() { atomic.AddInt32(&failures, 1) },
	}
	done := make(chan error, 1)
	go func() { done <- watcher.Run(ctx) }()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && (atomic.LoadInt32(&calls) < 2 || atomic.LoadInt32(&failures) < 2) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if atomic.LoadInt32(&calls) < 2 || atomic.LoadInt32(&failures) < 2 {
		t.Fatalf("calls=%d failures=%d, want retries after persistent errors", atomic.LoadInt32(&calls), atomic.LoadInt32(&failures))
	}
}

func TestFolderWatcherRunReportsOfflineRootsAndKeepsRetrying(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var calls int32
	var failures int32
	watcher := FolderWatcher{
		Scan: func(context.Context, []Root) (ScanReport, error) {
			atomic.AddInt32(&calls, 1)
			return ScanReport{Roots: []RootReport{{RootID: "snes-main", System: protocol.SystemSNES, Offline: true}}}, nil
		},
		Roots:    func() []Root { return []Root{{ID: "snes-main", System: protocol.SystemSNES, Path: t.TempDir()}} },
		Interval: 15 * time.Millisecond,
		OnError:  func() { atomic.AddInt32(&failures, 1) },
	}
	done := make(chan error, 1)
	go func() { done <- watcher.Run(ctx) }()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && (atomic.LoadInt32(&calls) < 2 || atomic.LoadInt32(&failures) < 2) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if atomic.LoadInt32(&calls) < 2 || atomic.LoadInt32(&failures) < 2 {
		t.Fatalf("calls=%d failures=%d, want offline roots to count as reconcile failures", atomic.LoadInt32(&calls), atomic.LoadInt32(&failures))
	}
}

func TestFolderWatcherRunWaitsFullIntervalAfterSlowReconcile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var mu sync.Mutex
	var starts []time.Time
	watcher := FolderWatcher{
		Scan: func(context.Context, []Root) (ScanReport, error) {
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(80 * time.Millisecond)
			return ScanReport{}, nil
		},
		Roots:    func() []Root { return []Root{{ID: "snes-main", System: protocol.SystemSNES, Path: t.TempDir()}} },
		Interval: 50 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() { done <- watcher.Run(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(starts)
		mu.Unlock()
		if n >= 3 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatalf("reconciles = %d, want 3", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	mu.Lock()
	gap := starts[2].Sub(starts[1])
	mu.Unlock()
	if gap < 100*time.Millisecond {
		t.Fatalf("loop reconcile gap %s, want interval after a slow scan", gap)
	}
}
