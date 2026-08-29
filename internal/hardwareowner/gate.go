package hardwareowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// MaintenanceFence reports whether a durable maintenance operation has
// reached a terminal state. The check is made while the hardware-owner lock
// is held so a writer cannot create a non-terminal journal between the check
// and a normal hardware transition.
type MaintenanceFence interface {
	Clear() error
}

// MaintenanceObserver is the read-only half of a maintenance fence. It is
// deliberately separate from MaintenanceFence: Clear may acquire journal
// locks or mutate recovery state, neither of which is safe from a public
// status/health read while a transition already holds the owner lock.
type MaintenanceObserver interface {
	Observe() error
}

// NormalGate is the narrow admission capability given to a normal core
// transition. Enter returns with the owner lock still held; the caller must
// release it only after its transition and durable teardown have completed.
type NormalGate interface {
	Enter(context.Context) (Unlock, error)
}

// OwnerStore is the portion of Store needed by Gate. Keeping this seam small
// lets host tests inject a durable-store fake without changing the production
// Store implementation.
type OwnerStore interface {
	Load() (record Record, exists bool, err error)
}

// OwnerLocker is the portion of Locker needed by Gate.
type OwnerLocker interface {
	Lock(context.Context) (Unlock, error)
}

// AdmissionVerifier is the narrow cross-package seam for boot-proof and live
// process admission. Gate invokes it while both locks remain held.
type AdmissionVerifier interface {
	Verify(context.Context, Record) error
}

// Gate serializes normal admission with development-owner transitions. The
// zero value is intentionally unusable and fails closed.
type Gate struct {
	Store  OwnerStore
	Locker OwnerLocker

	// InstallLocker is the shared install flock. Production admission acquires
	// it before Locker and releases it after Locker, matching the global
	// install-then-owner order. It is optional for legacy fixture gates; a nil
	// value keeps those tests entirely in-memory while production constructors
	// always provide it.
	InstallLocker     OwnerLocker
	AdmissionVerifier AdmissionVerifier

	// Fence is the preferred field. Maintenance and MaintenanceJournal are
	// compatibility aliases for callers that want the field to describe the
	// journal explicitly; the first non-nil value is used.
	Fence              MaintenanceFence
	Maintenance        MaintenanceFence
	MaintenanceJournal MaintenanceFence

	// CurrentBootID is useful for deterministic host tests. Production gates
	// read the kernel boot ID when it is empty. BootID, when supplied, takes
	// precedence and permits a platform-specific boot-ID reader.
	CurrentBootID string
	BootID        func() (string, error)
}

// NoMaintenanceFence is the terminal empty journal used by a profile that
// has no install operation. A development installer supplies its concrete
// durable journal instead.
type NoMaintenanceFence struct{}

func (NoMaintenanceFence) Clear() error   { return nil }
func (NoMaintenanceFence) Observe() error { return nil }

var (
	ErrGateConfiguration      = errors.New("normal admission gate is not configured")
	ErrMaintenanceFence       = errors.New("maintenance fence is not clear")
	ErrOwnerRecordAbsent      = errors.New("hardware-owner record is absent")
	ErrOwnerNotNormal         = errors.New("hardware owner is not normal_main")
	ErrOwnerWrongBoot         = errors.New("hardware-owner record belongs to another boot")
	ErrMaintenanceObservation = errors.New("maintenance fence cannot be observed read-only")
)

// NewGate constructs an admission gate around the Task 1 durable store and
// lock. An optional boot ID makes tests independent of the host kernel.
func NewGate(store OwnerStore, locker OwnerLocker, fence MaintenanceFence, currentBootID ...string) *Gate {
	gate := &Gate{Store: store, Locker: locker, Fence: fence}
	if len(currentBootID) != 0 {
		gate.CurrentBootID = currentBootID[0]
	}
	return gate
}

// NewGateWithBootID is the function-reader variant for platform tests and
// boot-ID providers that cannot be represented by a fixed string.
func NewGateWithBootID(store OwnerStore, locker OwnerLocker, fence MaintenanceFence, bootID func() (string, error)) *Gate {
	return &Gate{Store: store, Locker: locker, Fence: fence, BootID: bootID}
}

// NewProductionGate returns the root-owned gate used by the target agent.
func NewProductionGate(fence MaintenanceFence) *Gate {
	return &Gate{
		Store:         NewProductionStore(),
		Locker:        NewProductionLocker(),
		InstallLocker: NewLocker("/var/lock/fogcast/fpgadev-install.lock"),
		Fence:         fence,
	}
}

// Enter acquires the shared owner lock and keeps it held through the caller's
// entire normal transition. Every check is fail-closed: an absent, malformed,
// stale-boot, fenced, or non-normal record cannot grant admission.
func (g *Gate) Enter(ctx context.Context) (Unlock, error) {
	if g == nil || g.Store == nil || g.Locker == nil {
		return nil, ErrGateConfiguration
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrGateConfiguration)
	}
	var installUnlock Unlock
	var err error
	if g.InstallLocker != nil {
		installUnlock, err = g.InstallLocker.Lock(ctx)
		if err != nil {
			return nil, err
		}
		if installUnlock == nil {
			return nil, ErrGateConfiguration
		}
	}
	unlock, err := g.Locker.Lock(ctx)
	if err != nil {
		if installUnlock != nil {
			_ = installUnlock()
		}
		return nil, err
	}
	if unlock == nil {
		if installUnlock != nil {
			_ = installUnlock()
		}
		return nil, ErrGateConfiguration
	}
	releaseOnError := func(cause error) (Unlock, error) {
		ownerErr := unlock()
		installErr := error(nil)
		if installUnlock != nil {
			installErr = installUnlock()
		}
		if releaseErr := errors.Join(ownerErr, installErr); releaseErr != nil {
			return nil, fmt.Errorf("%w (release admission locks: %v)", cause, releaseErr)
		}
		return nil, cause
	}

	fence := g.maintenanceFence()
	if fence == nil {
		return releaseOnError(fmt.Errorf("%w: no maintenance journal", ErrMaintenanceFence))
	}
	if err := fence.Clear(); err != nil {
		return releaseOnError(fmt.Errorf("%w: %v", ErrMaintenanceFence, err))
	}

	record, exists, err := g.Store.Load()
	if err != nil {
		return releaseOnError(fmt.Errorf("load owner record for admission: %w", err))
	}
	if !exists {
		return releaseOnError(ErrOwnerRecordAbsent)
	}
	// Store.Load validates the record, but keep this invariant local to Gate so
	// injected OwnerStore implementations cannot bypass fail-closed admission.
	if err := record.Validate(); err != nil {
		return releaseOnError(fmt.Errorf("validate owner record for admission: %w", err))
	}
	bootID, err := g.bootID()
	if err != nil {
		return releaseOnError(fmt.Errorf("read current boot ID for admission: %w", err))
	}
	if record.BootID != bootID {
		return releaseOnError(ErrOwnerWrongBoot)
	}
	if record.State != StateNormalMain {
		return releaseOnError(ErrOwnerNotNormal)
	}
	if g.AdmissionVerifier != nil {
		if err := g.AdmissionVerifier.Verify(ctx, record); err != nil {
			return releaseOnError(fmt.Errorf("normal admission verifier rejected transition: %w", err))
		}
	}
	// The owner lock remains held for the complete transition. Releasing it
	// through the returned capability also releases the install lock second,
	// preserving the lock order on every normal-admission path.
	return joinUnlocks(unlock, installUnlock), nil
}

func joinUnlocks(owner, install Unlock) Unlock {
	var once sync.Once
	var releaseErr error
	return func() error {
		once.Do(func() {
			// Release owner before install; an owner transition must never expose
			// a window in which another installer can mutate launch sources while
			// the owner record is still held.
			releaseErr = errors.Join(owner(), func() error {
				if install == nil {
					return nil
				}
				return install()
			}())
		})
		return releaseErr
	}
}

// Observe performs a read-only, point-in-time availability check. It never
// acquires the owner lock and therefore cannot deadlock a public status or
// health read against a transition that is already holding that lock. The
// owner record and maintenance fence are observed independently; either may
// change immediately after its observation. Enter remains the authoritative
// check/use fence for hardware authorization.
func (g *Gate) Observe() error {
	if g == nil || g.Store == nil || g.Locker == nil {
		return ErrGateConfiguration
	}
	fence := g.maintenanceFence()
	if fence == nil {
		return fmt.Errorf("%w: no maintenance journal", ErrMaintenanceFence)
	}
	var installUnlock Unlock
	if g.InstallLocker != nil {
		var err error
		lockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		installUnlock, err = g.InstallLocker.Lock(lockCtx)
		if err != nil {
			return err
		}
		if installUnlock == nil {
			return ErrGateConfiguration
		}
		defer func() { _ = installUnlock() }()
	}
	observer, ok := fence.(MaintenanceObserver)
	if !ok && g.InstallLocker == nil {
		return ErrMaintenanceObservation
	}
	record, exists, err := g.Store.Load()
	if err != nil {
		return fmt.Errorf("load owner record for observation: %w", err)
	}
	if !exists {
		return ErrOwnerRecordAbsent
	}
	if err := record.Validate(); err != nil {
		return fmt.Errorf("validate owner record for observation: %w", err)
	}
	bootID, err := g.bootID()
	if err != nil {
		return fmt.Errorf("read current boot ID for observation: %w", err)
	}
	if record.BootID != bootID {
		return ErrOwnerWrongBoot
	}
	if record.State != StateNormalMain {
		return ErrOwnerNotNormal
	}
	observe := func() error {
		if g.InstallLocker != nil {
			// The concrete Task-8 journal exposes Clear as its no-lock read path;
			// Gate already holds the shared install flock here. Calling Observe
			// would recursively acquire that non-reentrant flock.
			return fence.Clear()
		}
		return observer.Observe()
	}
	if err := observe(); err != nil {
		return fmt.Errorf("%w: %v", ErrMaintenanceFence, err)
	}
	return nil
}

func (g *Gate) maintenanceFence() MaintenanceFence {
	fence := g.Fence
	if fence == nil {
		fence = g.Maintenance
	}
	if fence == nil {
		fence = g.MaintenanceJournal
	}
	return fence
}

func (g *Gate) bootID() (string, error) {
	if g.CurrentBootID != "" {
		return g.CurrentBootID, nil
	}
	if g.BootID != nil {
		return g.BootID()
	}
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	bootID := strings.TrimSpace(string(data))
	if bootID == "" {
		return "", errors.New("kernel boot ID is empty")
	}
	return bootID, nil
}

var _ NormalGate = (*Gate)(nil)
var _ MaintenanceObserver = (*Gate)(nil)
