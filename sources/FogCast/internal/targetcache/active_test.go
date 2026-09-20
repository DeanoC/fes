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

	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/protocol"
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

func TestActiveSystemReturnsValidatedPendingRecordSystem(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	content := []byte("persisted active")
	identity := contentIdentity(content, "gg")
	manager := openUploadManager(t, config)
	putBytes(t, manager, protocol.SystemGameGear, content, "gg")
	if apiErr := manager.PinForLaunch(protocol.SystemGameGear, identity); apiErr != nil {
		t.Fatalf("PinForLaunch: %v", apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemGameGear, identity); apiErr != nil {
		t.Fatalf("CommitLaunch: %v", apiErr)
	}

	manager = openUploadManager(t, config)
	systems, ok, apiErr := manager.ActiveRecordSystems(context.Background())
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if !ok || systems.Candidate.System != protocol.SystemGameGear || systems.Candidate.Content != identity || systems.Interrupted || systems.Previous != nil {
		t.Fatalf("ActiveRecordSystems() = %#v, %v; want validated Game Gear record", systems, ok)
	}
}

func TestActiveSystemRejectsRecordWithMissingCachedObject(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	content := []byte("stale active")
	identity := contentIdentity(content, "gg")
	manager := openUploadManager(t, config)
	putBytes(t, manager, protocol.SystemGameGear, content, "gg")
	if apiErr := manager.PinForLaunch(protocol.SystemGameGear, identity); apiErr != nil {
		t.Fatalf("PinForLaunch: %v", apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemGameGear, identity); apiErr != nil {
		t.Fatalf("CommitLaunch: %v", apiErr)
	}
	if err := os.Remove(cacheDestination(root, protocol.SystemGameGear, identity)); err != nil {
		t.Fatalf("remove cached object: %v", err)
	}

	manager = openUploadManager(t, config)
	if systems, ok, apiErr := manager.ActiveRecordSystems(context.Background()); apiErr != nil || ok {
		t.Fatalf("ActiveRecordSystems() = %#v, %v, %v for missing cached object", systems, ok, apiErr)
	}
}

func TestCanceledActiveVerificationPreservesRecordAndEvictionPin(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	activeBytes := []byte("persisted gg")
	newBytes := []byte("new content")
	config := uploadManagerConfig(root, int64(len(activeBytes)+len(newBytes)-1))
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	active := putBytes(t, manager, protocol.SystemGameGear, activeBytes, "gg")
	if apiErr := manager.PinForLaunch(protocol.SystemGameGear, active); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemGameGear, active); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if systems, ok, apiErr := manager.ActiveRecordSystems(canceled); apiErr == nil || apiErr.Code != protocol.CodeInternal || ok || systems != (targetcache.ActiveRecords{}) {
		t.Fatalf("ActiveRecordSystems() = %#v, %v, %#v; want indeterminate cancellation", systems, ok, apiErr)
	}
	_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes))
	assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, config.ActiveRecord)
	if _, err := os.Lstat(config.ActiveRecord); err != nil {
		t.Fatalf("active record was removed: %v", err)
	}
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemGameGear, active)); err != nil {
		t.Fatalf("active content was removed: %v", err)
	}
}

func TestLaunchingRecordSurvivesRestartAsAmbiguousAndProtectsBothContents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	previousBytes := []byte("previous sms")
	candidateBytes := []byte("candidate gg")
	newBytes := []byte("new content")
	config := uploadManagerConfig(root, int64(len(previousBytes)+len(candidateBytes)+len(newBytes)-1))
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	previous := putBytes(t, manager, protocol.SystemSMS, previousBytes, "sms")
	candidate := putBytes(t, manager, protocol.SystemGameGear, candidateBytes, "gg")
	if apiErr := manager.PinForLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatalf("pin previous: %v", apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatalf("commit previous: %v", apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemGameGear, candidate); apiErr != nil {
		t.Fatalf("pin candidate: %v", apiErr)
	}
	if _, apiErr := manager.RecordLaunchIntent(protocol.SystemGameGear, candidate); apiErr != nil {
		t.Fatalf("record candidate intent: %v", apiErr)
	}

	manager = openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	if apiErr := manager.AbortLaunch(protocol.SystemGameGear, candidate, targetcache.LaunchIntent{}); apiErr != nil {
		t.Fatal(apiErr)
	}
	if systems, ok, apiErr := manager.ActiveRecordSystems(context.Background()); apiErr != nil || !ok || !systems.Interrupted || systems.Candidate != (targetcache.ActiveRecordEntry{System: protocol.SystemGameGear, Content: candidate}) || systems.Previous == nil || *systems.Previous != (targetcache.ActiveRecordEntry{System: protocol.SystemSMS, Content: previous}) {
		t.Fatalf("ActiveRecordSystems() = %#v, %v, %#v; want SMS/Game Gear alternatives", systems, ok, apiErr)
	}
	_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes))
	assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, config.ActiveRecord)
	for system, identity := range map[protocol.System]protocol.ContentIdentity{
		protocol.SystemSMS: previous, protocol.SystemGameGear: candidate,
	} {
		if _, err := os.Lstat(cacheDestination(root, system, identity)); err != nil {
			t.Fatalf("protected %s content was removed: %v", system, err)
		}
	}
	if _, err := os.Lstat(config.ActiveRecord); err != nil {
		t.Fatalf("launching record was removed: %v", err)
	}
}

func TestDirectLaunchingRecordSurvivesRestartWithPreviousCachedAlternative(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	previous := putBytes(t, manager, protocol.SystemSMS, []byte("previous sms"), "sms")
	if apiErr := manager.PinForLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
	wantPrevious := targetcache.ActiveRecordEntry{System: protocol.SystemSMS, Content: previous}
	if apiErr != nil || !ok || !records.Interrupted || records.Candidate != (targetcache.ActiveRecordEntry{System: protocol.SystemGameGear, Direct: true}) || records.Previous == nil || *records.Previous != wantPrevious {
		t.Fatalf("direct active records = %#v, %v, %#v", records, ok, apiErr)
	}
}

func TestCommittedDirectLaunchSurvivesRepeatedRestartReconciliation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	intent, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitDirectLaunch(protocol.SystemGameGear, intent); apiErr != nil {
		t.Fatal(apiErr)
	}

	for restart := 1; restart <= 2; restart++ {
		manager = openUploadManager(t, config)
		records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
		want := targetcache.ActiveRecordEntry{System: protocol.SystemGameGear, Direct: true}
		if apiErr != nil || !ok || records.Interrupted || records.Candidate != want || records.Previous != nil {
			t.Fatalf("restart %d active records = %#v, %v, %#v; want %#v", restart, records, ok, apiErr, want)
		}
		system := protocol.SystemGameGear
		if apiErr := manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateActive, System: &system}, &want); apiErr != nil {
			t.Fatalf("restart %d ReconcileActive: %v", restart, apiErr)
		}
	}
	if apiErr := manager.ClearActive(); apiErr != nil {
		t.Fatal(apiErr)
	}
	manager = openUploadManager(t, config)
	if records, ok, apiErr := manager.ActiveRecordSystems(context.Background()); apiErr != nil || ok || records != (targetcache.ActiveRecords{}) {
		t.Fatalf("active records after stop cleanup = %#v, %v, %#v", records, ok, apiErr)
	}
}

func TestCommitDirectLaunchFinalizesMatchingRetainedIntentAfterRestart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	if _, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	retryIntent, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitDirectLaunch(protocol.SystemGameGear, retryIntent); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
	want := targetcache.ActiveRecordEntry{System: protocol.SystemGameGear, Direct: true}
	if apiErr != nil || !ok || records.Interrupted || records.Candidate != want || records.Previous != nil {
		t.Fatalf("active records = %#v, %v, %#v; want %#v", records, ok, apiErr, want)
	}
}

func TestAbortDirectLaunchBeforeDispatchRestoresPreviousDurableActive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	previous := putBytes(t, manager, protocol.SystemSMS, []byte("previous sms"), "sms")
	if apiErr := manager.PinForLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	intent, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.AbortDirectLaunch(protocol.SystemGameGear, intent); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
	if apiErr != nil || !ok || records.Interrupted || records.Candidate != (targetcache.ActiveRecordEntry{System: protocol.SystemSMS, Content: previous}) {
		t.Fatalf("restored active records = %#v, %v, %#v", records, ok, apiErr)
	}
}

func TestAbortDirectLaunchWithoutPreviousRemovesIntent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	intent, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.AbortDirectLaunch(protocol.SystemGameGear, intent); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	if records, ok, apiErr := manager.ActiveRecordSystems(context.Background()); apiErr != nil || ok || records != (targetcache.ActiveRecords{}) {
		t.Fatalf("active records after abort = %#v, %v, %#v", records, ok, apiErr)
	}
}

func TestAbortReplacementBeforeDispatchRestoresCommittedDirectIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		replace func(*testing.T, *targetcache.Manager)
	}{
		{
			name: "cached replacement",
			replace: func(t *testing.T, manager *targetcache.Manager) {
				content := putBytes(t, manager, protocol.SystemSNES, []byte("candidate snes"), "sfc")
				if apiErr := manager.PinForLaunch(protocol.SystemSNES, content); apiErr != nil {
					t.Fatal(apiErr)
				}
				intent, apiErr := manager.RecordLaunchIntent(protocol.SystemSNES, content)
				if apiErr != nil {
					t.Fatal(apiErr)
				}
				if apiErr := manager.AbortLaunch(protocol.SystemSNES, content, intent); apiErr != nil {
					t.Fatal(apiErr)
				}
			},
		},
		{
			name: "direct replacement",
			replace: func(t *testing.T, manager *targetcache.Manager) {
				intent, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear)
				if apiErr != nil {
					t.Fatal(apiErr)
				}
				if apiErr := manager.AbortDirectLaunch(protocol.SystemGameGear, intent); apiErr != nil {
					t.Fatal(apiErr)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			config := uploadManagerConfig(root, 64<<20)
			manager := openUploadManager(t, config)
			intent, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemSMS)
			if apiErr != nil {
				t.Fatal(apiErr)
			}
			if apiErr := manager.CommitDirectLaunch(protocol.SystemSMS, intent); apiErr != nil {
				t.Fatal(apiErr)
			}

			test.replace(t, manager)

			manager = openUploadManager(t, config)
			records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
			want := targetcache.ActiveRecordEntry{System: protocol.SystemSMS, Direct: true}
			if apiErr != nil || !ok || records.Interrupted || records.Candidate != want || records.Previous != nil {
				t.Fatalf("restored records = %#v, %v, %#v; want %#v", records, ok, apiErr, want)
			}
		})
	}
}

func TestReconcileInterruptedDirectLaunchDistinguishesCandidateFromPrevious(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name            string
		selected        protocol.System
		selectCandidate bool
		wantRecord      targetcache.ActiveRecordEntry
	}{
		{name: "unique direct candidate commits durable identity", selected: protocol.SystemGameGear, selectCandidate: true, wantRecord: targetcache.ActiveRecordEntry{System: protocol.SystemGameGear, Direct: true}},
		{name: "unique previous restores cache pin", selected: protocol.SystemSNES},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			config := uploadManagerConfig(root, 64<<20)
			manager := openUploadManager(t, config)
			previous := putBytes(t, manager, protocol.SystemSNES, []byte("previous snes"), "sfc")
			if apiErr := manager.PinForLaunch(protocol.SystemSNES, previous); apiErr != nil {
				t.Fatal(apiErr)
			}
			if apiErr := manager.CommitLaunch(protocol.SystemSNES, previous); apiErr != nil {
				t.Fatal(apiErr)
			}
			if _, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear); apiErr != nil {
				t.Fatal(apiErr)
			}

			manager = openUploadManager(t, config)
			selection := &targetcache.ActiveRecordEntry{System: protocol.SystemSNES, Content: previous}
			if test.selectCandidate {
				selection = &targetcache.ActiveRecordEntry{System: protocol.SystemGameGear, Direct: true}
			} else {
				test.wantRecord = *selection
			}
			if apiErr := manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateActive, System: &test.selected}, selection); apiErr != nil {
				t.Fatalf("ReconcileActive: %v", apiErr)
			}

			manager = openUploadManager(t, config)
			records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
			if apiErr != nil || !ok || records.Interrupted || records.Candidate != test.wantRecord || records.Previous != nil {
				t.Fatalf("reconciled records = %#v, %v, %#v; want %#v", records, ok, apiErr, test.wantRecord)
			}
		})
	}
}

func TestReconcileInterruptedDirectReplacementCanRestorePreviousDirectIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	previousIntent, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemSMS)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitDirectLaunch(protocol.SystemSMS, previousIntent); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := manager.RecordDirectLaunchIntent(protocol.SystemGameGear); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	previous := targetcache.ActiveRecordEntry{System: protocol.SystemSMS, Direct: true}
	system := protocol.SystemSMS
	if apiErr := manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateActive, System: &system}, &previous); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
	if apiErr != nil || !ok || records.Interrupted || records.Candidate != previous || records.Previous != nil {
		t.Fatalf("restored records = %#v, %v, %#v; want %#v", records, ok, apiErr, previous)
	}
}

func TestReconcileInterruptedLaunchFinalizesSelectedUniqueSystem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		previousSystem     protocol.System
		previousExtension  string
		candidateSystem    protocol.System
		candidateExtension string
		selected           protocol.System
	}{
		{name: "Game Boy candidate", previousSystem: protocol.SystemSNES, previousExtension: "sfc", candidateSystem: protocol.SystemGameBoy, candidateExtension: "gb", selected: protocol.SystemGameBoy},
		{name: "Game Boy Advance candidate", previousSystem: protocol.SystemSNES, previousExtension: "sfc", candidateSystem: protocol.SystemGBA, candidateExtension: "gba", selected: protocol.SystemGBA},
		{name: "PC Engine candidate", previousSystem: protocol.SystemSNES, previousExtension: "sfc", candidateSystem: protocol.SystemPCE, candidateExtension: "pce", selected: protocol.SystemPCE},
		{name: "legacy previous", previousSystem: protocol.SystemNES, previousExtension: "nes", candidateSystem: protocol.SystemGBA, candidateExtension: "gba", selected: protocol.SystemNES},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			config := uploadManagerConfig(root, 64<<20)
			manager := openUploadManager(t, config)
			previous := putBytes(t, manager, test.previousSystem, []byte("previous "+test.name), test.previousExtension)
			candidate := putBytes(t, manager, test.candidateSystem, []byte("candidate "+test.name), test.candidateExtension)
			if apiErr := manager.PinForLaunch(test.previousSystem, previous); apiErr != nil {
				t.Fatal(apiErr)
			}
			if apiErr := manager.CommitLaunch(test.previousSystem, previous); apiErr != nil {
				t.Fatal(apiErr)
			}
			if apiErr := manager.PinForLaunch(test.candidateSystem, candidate); apiErr != nil {
				t.Fatal(apiErr)
			}
			if _, apiErr := manager.RecordLaunchIntent(test.candidateSystem, candidate); apiErr != nil {
				t.Fatal(apiErr)
			}

			manager = openUploadManager(t, config)
			selectedContent := previous
			if test.selected == test.candidateSystem {
				selectedContent = candidate
			}
			selection := &targetcache.ActiveRecordEntry{System: test.selected, Content: selectedContent}
			if apiErr := manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateActive, System: &test.selected}, selection); apiErr != nil {
				t.Fatalf("ReconcileActive: %v", apiErr)
			}
			manager = openUploadManager(t, config)
			systems, ok, apiErr := manager.ActiveRecordSystems(context.Background())
			if apiErr != nil || !ok || systems.Interrupted || systems.Previous != nil || systems.Candidate != *selection {
				t.Fatalf("final active systems = %#v, %v, %#v; want %s", systems, ok, apiErr, test.selected)
			}
			if apiErr := manager.PinForLaunch(test.selected, selectedContent); apiErr != nil {
				t.Fatalf("pin after reconciliation: %v", apiErr)
			}
			intent, apiErr := manager.RecordLaunchIntent(test.selected, selectedContent)
			if apiErr != nil {
				t.Fatalf("record intent after reconciliation: %v", apiErr)
			}
			if apiErr := manager.AbortLaunch(test.selected, selectedContent, intent); apiErr != nil {
				t.Fatalf("abort post-reconciliation intent: %v", apiErr)
			}
		})
	}
}

func TestReconcileInterruptedLaunchClearsRecordAfterIdleObservation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	previousBytes := []byte("previous snes")
	candidateBytes := []byte("candidate gba")
	previous := putBytes(t, manager, protocol.SystemSNES, previousBytes, "sfc")
	candidate := putBytes(t, manager, protocol.SystemGBA, candidateBytes, "gba")
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSNES, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemGBA, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := manager.RecordLaunchIntent(protocol.SystemGBA, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}

	newBytes := []byte("new")
	config.MaxBytes = int64(len(newBytes))
	manager = openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	if apiErr := manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateIdle}, nil); apiErr != nil {
		t.Fatalf("ReconcileActive: %v", apiErr)
	}
	if _, err := os.Lstat(config.ActiveRecord); !os.IsNotExist(err) {
		t.Fatalf("interrupted active record remains: %v", err)
	}
	if records, ok, apiErr := manager.ActiveRecordSystems(context.Background()); apiErr != nil || ok || records != (targetcache.ActiveRecords{}) {
		t.Fatalf("ActiveRecordSystems = %#v, %v, %#v; want no record", records, ok, apiErr)
	}
	putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")
	for system, identity := range map[protocol.System]protocol.ContentIdentity{
		protocol.SystemSNES: previous,
		protocol.SystemGBA:  candidate,
	} {
		if _, err := os.Lstat(cacheDestination(root, system, identity)); !os.IsNotExist(err) {
			t.Fatalf("reconciled %s content remained pinned: %v", system, err)
		}
	}
}

func TestReconcileInterruptedSameSystemLaunchUsesExactContentIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name              string
		candidateContents []byte
	}{
		{name: "identical relaunch", candidateContents: []byte("same gba")},
		{name: "different content", candidateContents: []byte("new gba")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			config := uploadManagerConfig(root, 64<<20)
			manager := openUploadManager(t, config)
			previous := putBytes(t, manager, protocol.SystemGBA, []byte("same gba"), "gba")
			candidate := previous
			if string(test.candidateContents) != "same gba" {
				candidate = putBytes(t, manager, protocol.SystemGBA, test.candidateContents, "gba")
			}
			if apiErr := manager.PinForLaunch(protocol.SystemGBA, previous); apiErr != nil {
				t.Fatal(apiErr)
			}
			if apiErr := manager.CommitLaunch(protocol.SystemGBA, previous); apiErr != nil {
				t.Fatal(apiErr)
			}
			if apiErr := manager.PinForLaunch(protocol.SystemGBA, candidate); apiErr != nil {
				t.Fatal(apiErr)
			}
			if _, apiErr := manager.RecordLaunchIntent(protocol.SystemGBA, candidate); apiErr != nil {
				t.Fatal(apiErr)
			}

			manager = openUploadManager(t, config)
			selection := &targetcache.ActiveRecordEntry{System: protocol.SystemGBA, Content: candidate}
			system := protocol.SystemGBA
			status := protocol.Status{State: protocol.StateActive, System: &system}
			if apiErr := manager.ReconcileActive(context.Background(), status, selection); apiErr != nil {
				t.Fatalf("ReconcileActive: %v", apiErr)
			}

			manager = openUploadManager(t, config)
			records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
			if apiErr != nil || !ok || records.Interrupted || records.Candidate != *selection || records.Previous != nil {
				t.Fatalf("active records = %#v, %v, %#v; want exact selection %#v", records, ok, apiErr, *selection)
			}
		})
	}
}

func TestReconcileInterruptedLaunchRejectsMissingOrForeignSelectionWithoutDeletingIntent(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		selection func(protocol.ContentIdentity) *targetcache.ActiveRecordEntry
	}{
		{name: "missing selection", selection: func(protocol.ContentIdentity) *targetcache.ActiveRecordEntry { return nil }},
		{name: "foreign selection", selection: func(candidate protocol.ContentIdentity) *targetcache.ActiveRecordEntry {
			candidate.SHA256 = "2123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
			return &targetcache.ActiveRecordEntry{System: protocol.SystemGBA, Content: candidate}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			config := uploadManagerConfig(root, 64<<20)
			manager := openUploadManager(t, config)
			previous := putBytes(t, manager, protocol.SystemGBA, []byte("previous gba"), "gba")
			candidate := putBytes(t, manager, protocol.SystemGBA, []byte("candidate gba"), "gba")
			if apiErr := manager.PinForLaunch(protocol.SystemGBA, previous); apiErr != nil {
				t.Fatal(apiErr)
			}
			if apiErr := manager.CommitLaunch(protocol.SystemGBA, previous); apiErr != nil {
				t.Fatal(apiErr)
			}
			if apiErr := manager.PinForLaunch(protocol.SystemGBA, candidate); apiErr != nil {
				t.Fatal(apiErr)
			}
			if _, apiErr := manager.RecordLaunchIntent(protocol.SystemGBA, candidate); apiErr != nil {
				t.Fatal(apiErr)
			}

			manager = openUploadManager(t, config)
			system := protocol.SystemGBA
			status := protocol.Status{State: protocol.StateActive, System: &system}
			if apiErr := manager.ReconcileActive(context.Background(), status, test.selection(candidate)); apiErr == nil || apiErr.Code != protocol.CodeInternal {
				t.Fatalf("ReconcileActive error = %#v; want internal", apiErr)
			}
			records, ok, apiErr := manager.ActiveRecordSystems(context.Background())
			if apiErr != nil || !ok || !records.Interrupted || records.Candidate.Content != candidate || records.Previous == nil || records.Previous.Content != previous {
				t.Fatalf("preserved records = %#v, %v, %#v", records, ok, apiErr)
			}
			if _, err := os.Lstat(config.ActiveRecord); err != nil {
				t.Fatalf("launch intent removed: %v", err)
			}
		})
	}
}

func TestReconcileInterruptedLaunchPreservesIntentWhenSelectedContentCannotBeVerified(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	previous := putBytes(t, manager, protocol.SystemSNES, []byte("previous snes"), "sfc")
	candidate := putBytes(t, manager, protocol.SystemGBA, []byte("candidate gba"), "gba")
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSNES, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemGBA, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := manager.RecordLaunchIntent(protocol.SystemGBA, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}
	if err := os.Remove(cacheDestination(root, protocol.SystemGBA, candidate)); err != nil {
		t.Fatal(err)
	}

	manager = openUploadManager(t, config)
	selected := protocol.SystemGBA
	selection := &targetcache.ActiveRecordEntry{System: selected, Content: candidate}
	apiErr := manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateActive, System: &selected}, selection)
	if apiErr == nil || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("ReconcileActive error = %#v; want verification failure", apiErr)
	}
	if systems, ok, activeErr := manager.ActiveRecordSystems(context.Background()); activeErr != nil || !ok || !systems.Interrupted || systems.Candidate != *selection {
		t.Fatalf("preserved active systems = %#v, %v, %#v", systems, ok, activeErr)
	}
	if _, err := os.Lstat(config.ActiveRecord); err != nil {
		t.Fatalf("launch intent was removed: %v", err)
	}
}

func TestReconcileInterruptedLaunchPreservesIntentWhenVerificationContextIsCanceled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	previous := putBytes(t, manager, protocol.SystemSNES, []byte("previous snes"), "sfc")
	candidate := putBytes(t, manager, protocol.SystemGBA, []byte("candidate gba"), "gba")
	if apiErr := manager.PinForLaunch(protocol.SystemSNES, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSNES, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemGBA, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := manager.RecordLaunchIntent(protocol.SystemGBA, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	selected := protocol.SystemGBA
	selection := &targetcache.ActiveRecordEntry{System: selected, Content: candidate}
	apiErr := manager.ReconcileActive(ctx, protocol.Status{State: protocol.StateActive, System: &selected}, selection)
	if apiErr == nil || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("ReconcileActive error = %#v; want internal", apiErr)
	}
	if systems, ok, activeErr := manager.ActiveRecordSystems(context.Background()); activeErr != nil || !ok || !systems.Interrupted || systems.Candidate != *selection {
		t.Fatalf("preserved active systems = %#v, %v, %#v", systems, ok, activeErr)
	}
	if _, err := os.Lstat(config.ActiveRecord); err != nil {
		t.Fatalf("launch intent was removed: %v", err)
	}
}

func TestReconcileCommittedLaunchPreservesRecordWhenVerificationContextIsCanceled(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	activeBytes := []byte("active gba")
	newBytes := []byte("new content")
	config := uploadManagerConfig(root, int64(len(activeBytes)+len(newBytes)-1))
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	active := putBytes(t, manager, protocol.SystemGBA, activeBytes, "gba")
	if apiErr := manager.PinForLaunch(protocol.SystemGBA, active); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemGBA, active); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	selected := protocol.SystemGBA
	selection := &targetcache.ActiveRecordEntry{System: selected, Content: active}
	apiErr := manager.ReconcileActive(ctx, protocol.Status{State: protocol.StateActive, System: &selected}, selection)
	if apiErr == nil || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("ReconcileActive error = %#v; want internal", apiErr)
	}
	if systems, ok, activeErr := manager.ActiveRecordSystems(context.Background()); activeErr != nil || !ok || systems.Interrupted || systems.Candidate != *selection {
		t.Fatalf("preserved active systems = %#v, %v, %#v", systems, ok, activeErr)
	}
	if _, err := os.Lstat(config.ActiveRecord); err != nil {
		t.Fatalf("committed active record was removed: %v", err)
	}
	_, putErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes))
	assertSafeAPIError(t, putErr, protocol.CodeCacheFull, root, config.ActiveRecord)
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemGBA, active)); err != nil {
		t.Fatalf("committed active content was removed: %v", err)
	}
}

func TestAbortBeforeDispatchRestoresPreviousDurableActive(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	config := uploadManagerConfig(root, 64<<20)
	manager := openUploadManager(t, config)
	previous := putBytes(t, manager, protocol.SystemSMS, []byte("previous sms"), "sms")
	candidate := putBytes(t, manager, protocol.SystemGameGear, []byte("candidate gg"), "gg")
	if apiErr := manager.PinForLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemGameGear, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}
	intent, apiErr := manager.RecordLaunchIntent(protocol.SystemGameGear, candidate)
	if apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.AbortLaunch(protocol.SystemGameGear, candidate, intent); apiErr != nil {
		t.Fatal(apiErr)
	}

	manager = openUploadManager(t, config)
	systems, ok, apiErr := manager.ActiveRecordSystems(context.Background())
	if apiErr != nil || !ok || systems.Candidate != (targetcache.ActiveRecordEntry{System: protocol.SystemSMS, Content: previous}) || systems.Interrupted {
		t.Fatalf("ActiveRecordSystems() = %#v, %v, %#v; want restored SMS", systems, ok, apiErr)
	}
}

func TestRetainedIntentRetryCannotAbortOriginalAndDifferentIntentPreservesPins(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	previousBytes := []byte("previous sms")
	candidateBytes := []byte("candidate gg")
	differentBytes := []byte("different gba")
	newBytes := []byte("new content")
	config := uploadManagerConfig(root, int64(len(previousBytes)+len(candidateBytes)+len(differentBytes)+len(newBytes)-1))
	manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
	previous := putBytes(t, manager, protocol.SystemSMS, previousBytes, "sms")
	candidate := putBytes(t, manager, protocol.SystemGameGear, candidateBytes, "gg")
	different := putBytes(t, manager, protocol.SystemGBA, differentBytes, "gba")
	if apiErr := manager.PinForLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.CommitLaunch(protocol.SystemSMS, previous); apiErr != nil {
		t.Fatal(apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemGameGear, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}
	firstIntent, apiErr := manager.RecordLaunchIntent(protocol.SystemGameGear, candidate)
	if apiErr != nil {
		t.Fatal(apiErr)
	}

	if apiErr := manager.PinForLaunch(protocol.SystemGameGear, candidate); apiErr != nil {
		t.Fatal(apiErr)
	}
	retryIntent, apiErr := manager.RecordLaunchIntent(protocol.SystemGameGear, candidate)
	if apiErr != nil || retryIntent != (targetcache.LaunchIntent{}) {
		t.Fatalf("same-content retry intent = %#v, error = %#v; want admitted zero-authority token", retryIntent, apiErr)
	}
	if apiErr := manager.AbortLaunch(protocol.SystemGameGear, candidate, retryIntent); apiErr != nil {
		t.Fatal(apiErr)
	}
	if systems, ok, apiErr := manager.ActiveRecordSystems(context.Background()); apiErr != nil || !ok || !systems.Interrupted {
		t.Fatalf("active systems after retry abort = %#v, %v, %#v; want original intent", systems, ok, apiErr)
	}
	if apiErr := manager.PinForLaunch(protocol.SystemGBA, different); apiErr != nil {
		t.Fatal(apiErr)
	}
	if _, apiErr := manager.RecordLaunchIntent(protocol.SystemGBA, different); apiErr == nil || apiErr.Code != protocol.CodeInternal {
		t.Fatalf("different intent error = %#v; want rejection", apiErr)
	}
	if _, apiErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes)); apiErr == nil || apiErr.Code != protocol.CodeCacheFull {
		t.Fatalf("Put with retained pins error = %#v; want cache-full", apiErr)
	}
	if systems, ok, apiErr := manager.ActiveRecordSystems(context.Background()); apiErr != nil || !ok || !systems.Interrupted {
		t.Fatalf("active systems after rejected different intent = %#v, %v, %#v; want original intent", systems, ok, apiErr)
	}

	if apiErr := manager.AbortLaunch(protocol.SystemGameGear, candidate, firstIntent); apiErr != nil {
		t.Fatal(apiErr)
	}
	manager.AbortLaunch(protocol.SystemGBA, different, targetcache.LaunchIntent{})
	putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemSMS, previous)); err != nil {
		t.Fatalf("restored previous content was evicted: %v", err)
	}
	if _, err := os.Lstat(cacheDestination(root, protocol.SystemGameGear, candidate)); !os.IsNotExist(err) {
		t.Fatalf("fully aborted candidate remained pinned: %v", err)
	}
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
	manager.AbortLaunch(protocol.SystemMegaDrive, inFlight, targetcache.LaunchIntent{})
	_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes))
	assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root)

	manager.AbortLaunch(protocol.SystemSNES, inFlight, targetcache.LaunchIntent{})
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
			manager.ReconcileActive(context.Background(), tt.status(), nil)
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

	manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateActive, System: &system}, nil)
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

	manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateActive, System: &system}, nil)

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

	manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateActive, System: &system}, nil)

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

	manager.ReconcileActive(context.Background(), protocol.Status{State: protocol.StateIdle}, nil)

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

func TestActiveCommitFailurePinAbortPreservesEarlierInFlightSafetyPin(t *testing.T) {
	t.Parallel()

	t.Run("different second content", func(t *testing.T) {
		root := t.TempDir()
		activeBytes := []byte("active-a")
		secondBytes := []byte("flight-b")
		newBytes := []byte("new-data")
		config := uploadManagerConfig(root, int64(len(activeBytes)+len(secondBytes)))
		manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
		active := putBytes(t, manager, protocol.SystemSNES, activeBytes, "sfc")
		second := putBytes(t, manager, protocol.SystemSNES, secondBytes, "sfc")
		old := time.Unix(10, 0)
		if err := os.Chtimes(cacheDestination(root, protocol.SystemSNES, active), old, old); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(cacheDestination(root, protocol.SystemSNES, second), old.Add(time.Hour), old.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		for _, identity := range []protocol.ContentIdentity{active, second} {
			if response, apiErr := manager.Probe(context.Background(), protocol.SystemSNES, identity.Key()); apiErr != nil || !response.Present {
				t.Fatalf("refresh eviction memo: response=%#v error=%v", response, apiErr)
			}
		}
		if err := os.MkdirAll(config.ActiveRecord, 0o700); err != nil {
			t.Fatal(err)
		}
		if apiErr := manager.PinForLaunch(protocol.SystemSNES, active); apiErr != nil {
			t.Fatalf("pin active: %v", apiErr)
		}
		assertSafeAPIError(t, manager.CommitLaunch(protocol.SystemSNES, active), protocol.CodeInternal, root, config.ActiveRecord)

		if apiErr := manager.PinForLaunch(protocol.SystemSNES, second); apiErr != nil {
			t.Fatalf("pin second: %v", apiErr)
		}
		manager.AbortLaunch(protocol.SystemSNES, second, targetcache.LaunchIntent{})
		putBytes(t, manager, protocol.SystemSNES, newBytes, "sfc")

		if got, err := os.ReadFile(cacheDestination(root, protocol.SystemSNES, active)); err != nil || !bytes.Equal(got, activeBytes) {
			t.Fatalf("retained active content changed: bytes=%q error=%v", got, err)
		}
		if _, err := os.Lstat(cacheDestination(root, protocol.SystemSNES, second)); !os.IsNotExist(err) {
			t.Fatalf("aborted second content was not evicted: %v", err)
		}
	})

	t.Run("same second content", func(t *testing.T) {
		root := t.TempDir()
		activeBytes := []byte("active-a")
		newBytes := []byte("new-data")
		config := uploadManagerConfig(root, int64(len(activeBytes)+len(newBytes)-1))
		manager := openUploadManager(t, config, targetcache.WithSpaceProbe(unlimitedSpace))
		active := putBytes(t, manager, protocol.SystemSNES, activeBytes, "sfc")
		if err := os.MkdirAll(config.ActiveRecord, 0o700); err != nil {
			t.Fatal(err)
		}
		if apiErr := manager.PinForLaunch(protocol.SystemSNES, active); apiErr != nil {
			t.Fatalf("pin active: %v", apiErr)
		}
		assertSafeAPIError(t, manager.CommitLaunch(protocol.SystemSNES, active), protocol.CodeInternal, root, config.ActiveRecord)

		if apiErr := manager.PinForLaunch(protocol.SystemSNES, active); apiErr != nil {
			t.Fatalf("re-pin active: %v", apiErr)
		}
		manager.AbortLaunch(protocol.SystemSNES, active, targetcache.LaunchIntent{})
		_, apiErr := manager.Put(context.Background(), protocol.SystemSNES, contentIdentity(newBytes, "sfc"), bytes.NewReader(newBytes))
		assertSafeAPIError(t, apiErr, protocol.CodeCacheFull, root, config.ActiveRecord)
		if got, err := os.ReadFile(cacheDestination(root, protocol.SystemSNES, active)); err != nil || !bytes.Equal(got, activeBytes) {
			t.Fatalf("same-content abort dropped retained pin: bytes=%q error=%v", got, err)
		}
	})
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
