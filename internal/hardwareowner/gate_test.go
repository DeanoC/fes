package hardwareowner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type gateFenceFunc func() error

func (f gateFenceFunc) Clear() error { return f() }

func TestGateEnterRequiresCurrentCanonicalNormalMainAndClearMaintenance(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		record    Record
		bootID    string
		fenceErr  error
		wantError bool
	}{
		{name: "canonical normal main", record: normalMainRecord(), bootID: validBootID},
		{name: "wrong boot", record: normalMainRecord(), bootID: validBootIDNext, wantError: true},
		{name: "fenced state", record: recoveringIntentRecord(), bootID: validBootID, wantError: true},
		{name: "maintenance journal", record: normalMainRecord(), bootID: validBootID, fenceErr: errors.New("journal pending"), wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := testStore(t)
			if err := store.Replace(test.record); err != nil {
				t.Fatal(err)
			}
			gate := NewGate(store, testLockerForStore(t, store), gateFenceFunc(func() error { return test.fenceErr }), test.bootID)
			unlock, err := gate.Enter(context.Background())
			if test.wantError {
				if err == nil {
					t.Fatal("admission succeeded for rejected owner state")
				}
				if unlock != nil {
					t.Fatal("rejected admission returned an unlock")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if unlock == nil {
				t.Fatal("successful admission returned nil unlock")
			}
			if err := unlock(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGateEnterAndObserveStillRejectPreviousBootNormalMain(t *testing.T) {
	store := testStore(t)
	if err := store.Replace(normalMainRecord()); err != nil {
		t.Fatal(err)
	}
	gate := NewGate(store, testLockerForStore(t, store), gateFenceFunc(func() error { return nil }), validBootIDNext)
	gate.InstallLocker = Locker{Path: store.Path + ".install.lock", ExpectedUID: store.ExpectedUID}
	if unlock, err := gate.Enter(context.Background()); !errors.Is(err, ErrOwnerWrongBoot) || unlock != nil {
		t.Fatalf("Enter() = unlock:%v err:%v, want stale-boot fence", unlock, err)
	}
	if err := gate.Observe(); !errors.Is(err, ErrOwnerWrongBoot) {
		t.Fatalf("Observe() = %v, want stale-boot fence", err)
	}
}

func TestGateEnterKeepsSharedLockUntilReturnedUnlock(t *testing.T) {
	store := testStore(t)
	if err := store.Replace(normalMainRecord()); err != nil {
		t.Fatal(err)
	}
	locker := testLockerForStore(t, store)
	gate := NewGate(store, locker, gateFenceFunc(func() error { return nil }), validBootID)
	unlock, err := gate.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlock() }()

	blocked := make(chan error, 1)
	go func() {
		second, err := locker.Lock(context.Background())
		if err == nil {
			_ = second()
		}
		blocked <- err
	}()
	select {
	case err := <-blocked:
		t.Fatalf("second owner acquired while gate was admitted: %v", err)
	default:
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	if err := <-blocked; err != nil {
		t.Fatalf("second owner failed after gate unlock: %v", err)
	}
}

type recordingGateLocker struct {
	base   *Locker
	name   string
	events *[]string
	mu     *sync.Mutex
}

func (l recordingGateLocker) Lock(ctx context.Context) (Unlock, error) {
	unlock, err := l.base.Lock(ctx)
	if err != nil {
		return nil, err
	}
	return func() error {
		err := unlock()
		l.mu.Lock()
		*l.events = append(*l.events, l.name)
		l.mu.Unlock()
		return err
	}, nil
}

type gateAdmissionVerifierFunc func(context.Context, Record) error

func (f gateAdmissionVerifierFunc) Verify(ctx context.Context, record Record) error {
	return f(ctx, record)
}

func TestGateAdmissionVerifierRunsWithRealInstallThenOwnerLocksAndReleasesOwnerFirst(t *testing.T) {
	if err := os.MkdirAll("/dev/shm/fogcast-task1", 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("/dev/shm/fogcast-task1", "gate-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	store := NewStore(filepath.Join(root, "owner.json"), uid)
	if err := store.Replace(normalMainRecord()); err != nil {
		t.Fatal(err)
	}
	ownerBase := NewLocker(filepath.Join(root, "owner.lock"), uid)
	installBase := NewLocker(filepath.Join(root, "install.lock"), uid)
	events := make([]string, 0, 2)
	var eventsMu sync.Mutex
	ownerLocker := recordingGateLocker{base: ownerBase, name: "owner", events: &events, mu: &eventsMu}
	installLocker := recordingGateLocker{base: installBase, name: "install", events: &events, mu: &eventsMu}
	gate := NewGate(store, ownerLocker, gateFenceFunc(func() error { return nil }), validBootID)
	gate.InstallLocker = installLocker
	gate.AdmissionVerifier = gateAdmissionVerifierFunc(func(ctx context.Context, _ Record) error {
		probeContext, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		defer cancel()
		if unlock, err := installBase.Lock(probeContext); err == nil {
			_ = unlock()
			return errors.New("install lock was not held during verification")
		}
		if unlock, err := ownerBase.Lock(probeContext); err == nil {
			_ = unlock()
			return errors.New("owner lock was not held during verification")
		}
		return nil
	})
	unlock, err := gate.Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(); err != nil {
		t.Fatal(err)
	}
	eventsMu.Lock()
	gotEvents := append([]string(nil), events...)
	eventsMu.Unlock()
	if len(gotEvents) != 2 || gotEvents[0] != "owner" || gotEvents[1] != "install" {
		t.Fatalf("unlock order = %#v, want owner then install", gotEvents)
	}
}

func testLockerForStore(t *testing.T, store Store) Locker {
	t.Helper()
	return Locker{Path: store.Path + ".lock", ExpectedUID: store.ExpectedUID}
}
