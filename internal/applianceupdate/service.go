// Package applianceupdate coordinates network updates with runtime admission.
package applianceupdate

import (
	"context"
	"errors"
	"io"
	"sync"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/appliance/store"
)

var ErrBlocked = errors.New("appliance update or unconfirmed trial blocks this operation")
var ErrIdentity = errors.New("confirmation does not match the running trial")

type BootIdentity struct {
	BootID      string `json:"boot_id"`
	ImageSHA256 string `json:"image_sha256"`
	Trial       bool   `json:"trial"`
}

type Status struct {
	appliance.Status
	BootIdentity
	RawIdleReady bool `json:"raw_idle_ready"`
}

type Service struct {
	store       *appliance.Store
	coordinator *agent.Coordinator
	boot        BootIdentity
	reboot      func(context.Context) error
	cleanup     func(context.Context) error
	mutation    sync.Mutex
	mu          sync.Mutex
	blocked     bool
	trial       bool
	operations  map[*operation]struct{}
	drained     chan struct{}
}
type operation struct{ cancel context.CancelFunc }

func New(store *appliance.Store, coordinator *agent.Coordinator, boot BootIdentity, reboot func(context.Context) error, cleanup func(context.Context) error) *Service {
	s := &Service{store: store, coordinator: coordinator, boot: boot, reboot: reboot, cleanup: cleanup, operations: make(map[*operation]struct{})}
	_, err := store.Status()
	s.trial = boot.Trial
	s.blocked = s.trial || err != nil
	coordinator.SetUpdateBlocked(s.blocked)
	if err == nil {
		_ = s.reconcileConfirmation(context.Background(), coordinator.RawIdleReady(context.Background()))
	}
	return s
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	rawIdle := s.coordinator.RawIdleReady(ctx)
	reconcileErr := s.reconcileConfirmation(ctx, rawIdle)
	s.mu.Lock()
	boot := s.boot
	boot.Trial = s.trial
	s.mu.Unlock()
	// Read durable state after the local trial snapshot so a concurrent confirm
	// cannot return trial=false alongside a pre-confirmation known-good image.
	st, err := s.store.StatusContext(ctx)
	return Status{Status: st, BootIdentity: boot, RawIdleReady: rawIdle}, errors.Join(reconcileErr, err)
}

// A confirmation write can rename successfully and then report a sync failure.
// Reconcile only after the store proves persistence for this exact trial; merely
// seeing matching fields must not release the admission or watchdog gate.
func (s *Service) reconcileConfirmation(ctx context.Context, rawIdle bool) error {
	if !s.boot.Trial || !rawIdle || !s.mutation.TryLock() {
		return nil
	}
	defer s.mutation.Unlock()
	s.mu.Lock()
	trial := s.trial
	s.mu.Unlock()
	if !trial {
		return nil
	}
	confirmed, err := s.store.ConfirmedContext(ctx, s.boot.BootID, s.boot.ImageSHA256)
	if err != nil || !confirmed {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.trial = false
	s.blocked = false
	s.coordinator.SetUpdateBlocked(false)
	s.mu.Unlock()
	return nil
}

// BeginOperation fences HTTP launch, development, input and cast operations.
// Maintenance cancels existing requests and waits for their handlers to leave.
func (s *Service) BeginOperation(parent context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked {
		return nil, nil, ErrBlocked
	}
	ctx, cancel := context.WithCancel(parent)
	op := &operation{cancel: cancel}
	s.operations[op] = struct{}{}
	var once sync.Once
	done := func() {
		once.Do(func() {
			cancel()
			s.mu.Lock()
			delete(s.operations, op)
			if len(s.operations) == 0 && s.drained != nil {
				close(s.drained)
				s.drained = nil
			}
			s.mu.Unlock()
		})
	}
	return ctx, done, nil
}

func (s *Service) Stage(ctx context.Context, m release.Manifest, size int64, r io.Reader) error {
	if !s.mutation.TryLock() {
		return ErrBlocked
	}
	defer s.mutation.Unlock()
	s.mu.Lock()
	blocked := s.blocked
	s.mu.Unlock()
	if blocked {
		return ErrBlocked
	}
	st, err := s.store.StatusContext(ctx)
	if err != nil {
		return err
	}
	if st.Pending != "" {
		return ErrBlocked
	}
	return s.store.Stage(ctx, m, size, r)
}

func (s *Service) Activate(ctx context.Context, hash string) (func(context.Context) error, error) {
	if !release.ValidHash(hash) {
		return nil, ErrIdentity
	}
	return s.prepare(ctx, func() error { return s.store.ActivateContext(ctx, hash) })
}
func (s *Service) Rollback(ctx context.Context) (func(context.Context) error, error) {
	return s.prepare(ctx, func() error { return s.store.RollbackContext(ctx) })
}

func (s *Service) prepare(ctx context.Context, selectImage func() error) (func(context.Context) error, error) {
	if !s.mutation.TryLock() {
		return nil, ErrBlocked
	}
	s.mu.Lock()
	if s.blocked || s.reboot == nil {
		s.mu.Unlock()
		s.mutation.Unlock()
		return nil, ErrBlocked
	}
	s.blocked = true
	s.coordinator.SetUpdateBlocked(true)
	var drained <-chan struct{}
	if len(s.operations) > 0 {
		s.drained = make(chan struct{})
		drained = s.drained
	}
	for op := range s.operations {
		op.cancel()
	}
	s.mu.Unlock()
	var releaseTransition func()
	var once sync.Once
	abort := func() {
		once.Do(func() {
			if releaseTransition != nil {
				releaseTransition()
			}
			s.mu.Lock()
			s.blocked = s.trial
			s.coordinator.SetUpdateBlocked(s.blocked)
			s.mu.Unlock()
			s.mutation.Unlock()
		})
	}
	if drained != nil {
		select {
		case <-drained:
		case <-ctx.Done():
			abort()
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		abort()
		return nil, err
	}
	var err error
	releaseTransition, err = s.coordinator.BeginMaintenance(ctx)
	if err != nil {
		abort()
		return nil, err
	}
	if s.cleanup != nil {
		if err = s.cleanup(ctx); err != nil {
			abort()
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		abort()
		return nil, err
	}
	if err = selectImage(); err != nil {
		abort()
		return nil, err
	}
	// The handler must call this exactly once after flushing its response, even
	// when the response failed (with a canceled context to release admission).
	var finishOnce sync.Once
	var finishErr error
	finish := func(ctx context.Context) error {
		finishOnce.Do(func() {
			if finishErr = ctx.Err(); finishErr != nil {
				abort()
				return
			}
			if finishErr = s.reboot(ctx); finishErr != nil {
				abort()
				return
			}
			// A successful reboot request retains the transition until process death.
		})
		return finishErr
	}
	return finish, nil
}

func (s *Service) Confirm(ctx context.Context, bootID, hash string) error {
	if !s.mutation.TryLock() {
		return ErrBlocked
	}
	defer s.mutation.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !s.boot.Trial || bootID != s.boot.BootID || hash != s.boot.ImageSHA256 {
		return ErrIdentity
	}
	if !s.coordinator.RawIdleReady(ctx) {
		return errors.New("native runtime is not idle")
	}
	if err := s.store.ConfirmContext(ctx, bootID, hash); err != nil {
		return err
	}
	s.mu.Lock()
	s.trial = false
	s.blocked = false
	s.coordinator.SetUpdateBlocked(false)
	s.mu.Unlock()
	return nil
}
