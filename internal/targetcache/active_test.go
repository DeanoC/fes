package targetcache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/targetcache"
	"github.com/DeanoC/FogCast-POC/protocol"
)

func TestActiveCommitPublishesPrivateAtomicRecordAndTouchesLaunchedEntry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	launchedBytes := []byte("launched content")
	launched := contentIdentity(launchedBytes, "sfc")
	now := time.Unix(1_800_000_000, 123_000_000)
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace), targetcache.WithClock(func() time.Time { return now }))
	if _, apiErr := manager.Put(context.Background(), protocol.SystemSNES, launched, bytes.NewReader(launchedBytes)); apiErr != nil {
		t.Fatalf("Put: %v", apiErr)
	}
	old := now.Add(-24 * time.Hour)
	path := cacheDestination(root, protocol.SystemSNES, launched)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, launched.Key()); apiErr != nil || !response.Present {
		t.Fatalf("refresh launched memo after mtime setup: response=%#v error=%v", response, apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, launched); apiErr != nil {
		t.Fatalf("PinForLaunch: %v", apiErr)
	}

	if apiErr := manager.CommitLaunch(protocol.SystemSNES, launched); apiErr != nil {
		t.Fatalf("CommitLaunch: %v", apiErr)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(now) {
		t.Fatalf("launched mtime = %v, want %v", info.ModTime(), now)
	}
	recordInfo, err := os.Lstat(config.ActiveRecord)
	if err != nil {
		t.Fatal(err)
	}
	if !recordInfo.Mode().IsRegular() || recordInfo.Mode().Perm() != 0o600 {
		t.Fatalf("active record mode = %v, want regular 0600", recordInfo.Mode())
	}
	data, err := os.ReadFile(config.ActiveRecord)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		System  protocol.System          `json:"system"`
		Content protocol.ContentIdentity `json:"content"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode active record: %v", err)
	}
	if record.System != protocol.SystemSNES || record.Content != launched {
		t.Fatalf("active record = %#v, want system=%q content=%#v", record, protocol.SystemSNES, launched)
	}
	assertNoActiveTemps(t, config.ActiveRecord)
}

func TestActiveAndInFlightPinsSurviveCapacityAccounting(t *testing.T) {
	t.Parallel()

	t.Run("current active", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		activeBytes := []byte("active")
		victimBytes := []byte("victim")
		newBytes := []byte("new-one")
		max := int64(len(activeBytes) + len(victimBytes) + len(newBytes) - 1)
		manager := openUploadManager(t, uploadManagerConfig(root, max), targetcache.WithSpaceProbe(unlimitedSpace))
		active := putBytes(t, manager, protocol.SystemSNES, activeBytes, "sfc")
		victim := putBytes(t, manager, protocol.SystemSNES, victimBytes, "sfc")
		old := time.Unix(10, 0)
		if err := os.Chtimes(cacheDestination(root, protocol.SystemSNES, active), old, old); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(cacheDestination(root, protocol.SystemSNES, victim), old.Add(time.Hour), old.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		for _, identity := range []protocol.ContentIdentity{active, victim} {
			if response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key()); apiErr != nil || !response.Present {
				t.Fatalf("refresh eviction memo: response=%#v error=%v", response, apiErr)
			}
		}
		if apiErr := manager.PinForLaunch(protocol.SystemSNES, active); apiErr != nil {
			t.Fatalf("PinForLaunch: %v", apiErr)
		}
		if apiErr := manager.CommitLaunch(protocol.SystemSNES, active); apiErr != nil {
			t.Fatalf("CommitLaunch: %v", apiErr)
		}

		putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")

		if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, active)); err != nil {
			t.Fatalf("active entry was evicted: %v", err)
		}
		if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, victim)); !os.IsNotExist(err) {
			t.Fatalf("inactive victim still exists: %v", err)
		}
	})

	t.Run("in flight", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		pinnedBytes := []byte("pinned")
		victimBytes := []byte("victim")
		newBytes := []byte("new-one")
		max := int64(len(pinnedBytes) + len(victimBytes) + len(newBytes) - 1)
		manager := openUploadManager(t, uploadManagerConfig(root, max), targetcache.WithSpaceProbe(unlimitedSpace))
		pinned := putBytes(t, manager, protocol.SystemSNES, pinnedBytes, "sfc")
		victim := putBytes(t, manager, protocol.SystemSNES, victimBytes, "sfc")
		old := time.Unix(10, 0)
		if err := os.Chtimes(cacheDestination(root, protocol.SystemSNES, pinned), old, old); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(cacheDestination(root, protocol.SystemSNES, victim), old.Add(time.Hour), old.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		for _, identity := range []protocol.ContentIdentity{pinned, victim} {
			if response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key()); apiErr != nil || !response.Present {
				t.Fatalf("refresh eviction memo: response=%#v error=%v", response, apiErr)
			}
		}
		if apiErr := manager.PinForLaunch(protocol.SystemSNES, pinned); apiErr != nil {
			t.Fatalf("PinForLaunch: %v", apiErr)
		}

		putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")

		if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, pinned)); err != nil {
			t.Fatalf("in-flight entry was evicted: %v", err)
		}
		if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, victim)); !os.IsNotExist(err) {
			t.Fatalf("inactive victim still exists: %v", err)
		}
	})
}

func TestActiveAbortLaunchDropsOnlyMatchingTemporaryPin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	activeBytes := []byte("active")
	inFlightBytes := []byte("inflight")
	newBytes := []byte("new-data")
	max := int64(len(activeBytes) + len(inFlightBytes) + len(newBytes) - 1)
	manager := openUploadManager(t, uploadManagerConfig(root, max), targetcache.WithSpaceProbe(unlimitedSpace))
	active := putBytes(t, manager, protocol.SystemSNES, activeBytes, "sfc")
	inFlight := putBytes(t, manager, protocol.SystemSNES, inFlightBytes, "sfc")
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, active); apiErr != nil {
		t.Fatalf("Pin active: %v", apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSNES, active); apiErr != nil {
		t.Fatalf("Commit active: %v", apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, inFlight); apiErr != nil {
		t.Fatalf("Pin in-flight: %v", apiErr)
	}
	manager.AbortLaunch(protocol.SystemMegaDrive, inFlight)
	_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes))
	assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root)

	manager.AbortLaunch(protocol.SystemSNES, inFlight)
	putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, active)); err != nil {
		t.Fatalf("AbortLaunch dropped previous active pin: %v", err)
	}
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, inFlight)); !os.IsNotExist(err) {
		t.Fatalf("aborted in-flight entry was not selected as safe victim: %v", err)
	}
}

func TestClearActiveRemovesRecordAndPinOnlyAfterSuccessfulCleanup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	activeBytes := []byte("active")
	newBytes := []byte("new-data")
	config := uploadManagerConfig(root, int64(len(activeBytes)+len(newBytes)-1))
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	active := putBytes(t, manager, protocol.SystemSNES, activeBytes, "sfc")
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, active); apiErr != nil {
		t.Fatalf("PinForLaunch: %v", apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSNES, active); apiErr != nil {
		t.Fatalf("CommitLaunch: %v", apiErr)
	}

	if apiErr := manager.ClearActive(); apiErr != nil {
		t.Fatalf("ClearActive: %v", apiErr)
	}
	if _, err := os.Lstat(config.ActiveRecord); !os.IsNotExist(err) {
		t.Fatalf("active record still exists: %v", err)
	}
	putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, active)); !os.IsNotExist(err) {
		t.Fatalf("cleared active entry remained pinned: %v", err)
	}
}

func TestReconcileActiveRetainsOnlyMatchingActiveSystemAcrossAgentRestart(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name          string
		status        func() protocol.Status
		wantProtected bool
	}{
		{name: "matching active system", status: func() protocol.Status {
			system := protocol.SystemSNES
			return protocol.Status{State: protocol.StateActive, System: &system}
		}, wantProtected: true},
		{name: "different active system", status: func() protocol.Status {
			system := protocol.SystemMegaDrive
			return protocol.Status{State: protocol.StateActive, System: &system}
		}},
		{name: "menu idle", status: func() protocol.Status { return protocol.Status{State: protocol.StateIdle} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			activeBytes := []byte("persisted-active")
			newBytes := []byte("new-data")
			config := uploadManagerConfig(root, int64(len(activeBytes)+len(newBytes)-1))
			manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
			active := putBytes(t, manager, protocol.SystemSNES, activeBytes, "sfc")
			if apiErr := manager.PinForLaunch(protocol.SystemSNES, active); apiErr != nil {
				t.Fatalf("PinForLaunch: %v", apiErr)
			}
			if apiErr := manager.CommitLaunch(protocol.SystemSNES, active); apiErr != nil {
				t.Fatalf("CommitLaunch: %v", apiErr)
			}

			manager = openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
			manager.ReconcileActive(tt.status())
			_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes))
			if tt.wantProtected {
				assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, config.ActiveRecord)
				if _, err := os.Lstat(config.ActiveRecord); err != nil {
					t.Fatalf("matching record removed: %v", err)
				}
				if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, active)); err != nil {
					t.Fatalf("reconciled active entry evicted: %v", err)
				}
			} else {
				if apiErr != nil {
					t.Fatalf("Put after nonmatching reconcile: %v", apiErr)
				}
				if _, err := os.Lstat(config.ActiveRecord); !os.IsNotExist(err) {
					t.Fatalf("nonmatching record remains: %v", err)
				}
				if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, active)); !os.IsNotExist(err) {
					t.Fatalf("nonmatching active remained pinned: %v", err)
				}
			}
		})
	}
}

func TestReconcileActiveWithoutVolatileRecordStartsUnpinnedAfterFullReboot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	activeBytes := []byte("disk-only-entry")
	newBytes := []byte("new-data")
	active := contentIdentity(activeBytes, "sfc")
	writeCacheFile(t, root, protocol.SystemSNES, active, activeBytes)
	config := uploadManagerConfig(root, int64(len(activeBytes)+len(newBytes)-1))
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	system := protocol.SystemSNES

	manager.ReconcileActive(protocol.Status{State: protocol.StateActive, System: &system})
	putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")

	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, active)); !os.IsNotExist(err) {
		t.Fatalf("entry without volatile record started pinned: %v", err)
	}
}

func TestActiveReconcileRejectsAndRemovesMalformedVolatileRecordWithoutLeak(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	if err := os.MkdirAll(filepath.Dir(config.ActiveRecord), 0o700); err != nil {
		t.Fatal(err)
	}
	private := "private-record-payload"
	if err := os.WriteFile(config.ActiveRecord, []byte(`{"system":"snes","content":"`+private+`","unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	manager := openUploadManager(t, config, targetcache.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	system := protocol.SystemSNES

	manager.ReconcileActive(protocol.Status{State: protocol.StateActive, System: &system})

	if _, err := os.Lstat(config.ActiveRecord); !os.IsNotExist(err) {
		t.Fatalf("malformed active record remains: %v", err)
	}
	if strings.Contains(logs.String(), private) || strings.Contains(logs.String(), config.ActiveRecord) || strings.Contains(logs.String(), root) {
		t.Fatalf("active reconciliation log exposed private data: %s", logs.String())
	}
}

func TestActiveReconcileRejectsAndRemovesWritableVolatileRecord(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	activeBytes := []byte("writable-record-content")
	active := contentIdentity(activeBytes, "sfc")
	writeCacheFile(t, root, protocol.SystemSNES, active, activeBytes)
	newBytes := []byte("new-data")
	config := uploadManagerConfig(root, int64(len(activeBytes)+len(newBytes)-1))
	if err := os.MkdirAll(filepath.Dir(config.ActiveRecord), 0o700); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(struct {
		System  protocol.System          `json:"system"`
		Content protocol.ContentIdentity `json:"content"`
	}{System: protocol.SystemSNES, Content: active})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ActiveRecord, append(record, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config.ActiveRecord, 0o644); err != nil {
		t.Fatal(err)
	}
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	system := protocol.SystemSNES

	manager.ReconcileActive(protocol.Status{State: protocol.StateActive, System: &system})

	if _, err := os.Lstat(config.ActiveRecord); !os.IsNotExist(err) {
		t.Fatalf("writable active record remains: %v", err)
	}
	putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, active)); !os.IsNotExist(err) {
		t.Fatalf("writable active record pinned content: %v", err)
	}
}

func TestActiveReconcileRemovesRecordSymlinkWithoutFollowingIt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	if err := os.MkdirAll(filepath.Dir(config.ActiveRecord), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside-record-target")
	if err := os.WriteFile(outside, []byte("outside-must-remain"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, config.ActiveRecord); err != nil {
		t.Fatal(err)
	}
	manager := openUploadManager(t, config)

	manager.ReconcileActive(protocol.Status{State: protocol.StateIdle})

	if _, err := os.Lstat(config.ActiveRecord); !os.IsNotExist(err) {
		t.Fatalf("invalid active-record symlink remains: %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "outside-must-remain" {
		t.Fatalf("outside active-record target changed: bytes=%q error=%v", got, err)
	}
}

func TestActiveCommitReplacesPriorPinAndMakesItEvictable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	firstBytes := []byte("first-active")
	secondBytes := []byte("second-active")
	newBytes := []byte("new-data")
	max := int64(len(firstBytes) + len(secondBytes) + len(newBytes) - 1)
	manager := openUploadManager(t, uploadManagerConfig(root, max), targetcache.WithSpaceProbe(unlimitedSpace))
	first := putBytes(t, manager, protocol.SystemSNES, firstBytes, "sfc")
	second := putBytes(t, manager, protocol.SystemSNES, secondBytes, "sfc")
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, first); apiErr != nil {
		t.Fatalf("Pin first: %v", apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSNES, first); apiErr != nil {
		t.Fatalf("Commit first: %v", apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, second); apiErr != nil {
		t.Fatalf("Pin second: %v", apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSNES, second); apiErr != nil {
		t.Fatalf("Commit second: %v", apiErr)
	}
	old := time.Unix(1, 0)
	if err := os.Chtimes(cacheDestination(root, protocol.SystemSNES, first), old, old); err != nil {
		t.Fatal(err)
	}
	if response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, first.Key()); apiErr != nil || !response.Present {
		t.Fatalf("refresh prior active memo: response=%#v error=%v", response, apiErr)
	}

	putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")

	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, first)); !os.IsNotExist(err) {
		t.Fatalf("prior active remained pinned: %v", err)
	}
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, second)); err != nil {
		t.Fatalf("current active was evicted: %v", err)
	}
}

func TestActiveCommitRecordFailureRetainsInFlightPinAndSanitizesError(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pinnedBytes := []byte("pinned")
	newBytes := []byte("new-data")
	config := uploadManagerConfig(root, int64(len(pinnedBytes)+len(newBytes)-1))
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	pinned := putBytes(t, manager, protocol.SystemSNES, pinnedBytes, "sfc")
	if err := os.MkdirAll(config.ActiveRecord, 0o700); err != nil {
		t.Fatal(err)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, pinned); apiErr != nil {
		t.Fatalf("PinForLaunch: %v", apiErr)
	}

	apiErr := manager.CommitLaunch(protocol.SystemSNES, pinned)
	assertSafeAPIError(t, apiErr, protocol.CodeInternal, root, config.ActiveRecord)
	_, apiErr = manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes))
	assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, config.ActiveRecord)
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, pinned)); err != nil {
		t.Fatalf("commit failure allowed in-flight eviction: %v", err)
	}
}

func TestActivePinForLaunchRejectsMissingContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	manager := openUploadManager(t, uploadManagerConfig(root, 64<<20))
	missing := contentIdentity([]byte("missing"), "sfc")

	apiErr := manager.PinForLaunch(protocol.SystemSNES, missing)

	assertSafeAPIError(t, apiErr, protocol.CodeContentNotCached, root, missing.SHA256)
}

func putBytes(t *testing.T, manager *targetcache.Manager, system protocol.System, content []byte, extension string) protocol.ContentIdentity {
	t.Helper()
	identity := contentIdentity(content, extension)
	response, apiErr := manager.Put(context.Background(), system, identity, bytes.NewReader(content))
	if apiErr != nil {
		t.Fatalf("Put %q: %v", content, apiErr)
	}
	assertUploadResponse(t, response, protocol.CacheUploadCreated, system, identity)
	return identity
}

func assertNoActiveTemps(t *testing.T, activeRecord string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(activeRecord))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(activeRecord) {
			t.Fatalf("active-record temporary file remains: %s", entry.Name())
		}
	}
}
