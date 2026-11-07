// Package applianceupdate coordinates network updates with runtime admission.
package applianceupdate

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	release "github.com/DeanoC/FogCast/appliance"
	"github.com/DeanoC/FogCast/appliance/store"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/appliancesplash"
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
	Board         string `json:"board"`
	RawIdleReady  bool   `json:"raw_idle_ready"`
	FatIdleSHA256 string `json:"fat_idle_sha256,omitempty"`
	IdleSHA256    string `json:"idle_sha256,omitempty"`
	// SplashWarning is the non-trial splash repair error. Status still returns
	// the boot state and both digests when this is set.
	SplashWarning string `json:"splash_warning,omitempty"`
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
	// ticketRepair requests one later rewrite of a boot.json that stayed
	// trial:true after a durable confirmation. It stays set until that
	// rewrite is attempted, so a timed-out status read can try again. A
	// failed rewrite still clears it, so a full volume cannot make every
	// status read rewrite the ticket.
	ticketRepair  bool
	operations    map[*operation]struct{}
	drained       chan struct{}
	splash        *appliancesplash.Installer
	splashWarning string
}
type operation struct{ cancel context.CancelFunc }

func New(store *appliance.Store, coordinator *agent.Coordinator, boot BootIdentity, reboot func(context.Context) error, cleanup func(context.Context) error) *Service {
	return NewWithSplash(store, coordinator, boot, reboot, cleanup, nil)
}

// NewWithSplash is New, and confirmation also publishes the image splash.
// splash is retained for the life of the service.
func NewWithSplash(store *appliance.Store, coordinator *agent.Coordinator, boot BootIdentity, reboot func(context.Context) error, cleanup func(context.Context) error, splash *appliancesplash.Installer) *Service {
	s := &Service{store: store, coordinator: coordinator, boot: boot, reboot: reboot, cleanup: cleanup, operations: make(map[*operation]struct{}), splash: splash}
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
	warning := s.splashWarning
	s.mu.Unlock()
	// Read durable state after the local trial snapshot so a concurrent confirm
	// cannot return trial=false alongside a pre-confirmation known-good image.
	st, err := s.store.StatusContext(ctx)
	status := Status{Status: st, BootIdentity: boot, Board: s.Board(), RawIdleReady: rawIdle, SplashWarning: warning}
	if s.splash != nil {
		status.FatIdleSHA256 = s.splash.FATSHA256()
		status.IdleSHA256 = s.splash.IdleSHA256()
	}
	return status, errors.Join(reconcileErr, err)
}

// Board is pinned by the image-selected store, independently of uploads.
func (s *Service) Board() string { return s.store.Board() }

// A confirmation write can rename successfully and then report a sync failure.
// Reconcile only after the store proves persistence for this exact trial; merely
// seeing matching fields must not release the admission or watchdog gate. A
// boot.json rewrite failure after that proof opens admission and is retried
// once on a later idle status read.
// A non-trial boot republishes the running image's splash. That repairs an
// interrupted publication and a factory fallback whose staged copy is gone.
// A failed repair is logged and returned in SplashWarning. It does not fail
// Status, so the boot state and splash digests stay visible. A trial boot
// does not use that path: the watchdog stays armed until the FAT publish
// succeeds.
func (s *Service) reconcileConfirmation(ctx context.Context, rawIdle bool) error {
	if !s.mutation.TryLock() {
		return nil
	}
	defer s.mutation.Unlock()
	if !s.boot.Trial {
		return s.noteSplashRepair(s.repairSplash(ctx))
	}
	if !rawIdle {
		return nil
	}
	s.mu.Lock()
	trial := s.trial
	repair := s.ticketRepair
	s.mu.Unlock()
	if !trial && !repair {
		return nil
	}
	if !trial {
		return s.retryBootTicket(ctx)
	}
	confirmed, err := s.store.ConfirmedContext(ctx, s.boot.BootID, s.boot.ImageSHA256)
	if err != nil {
		return err
	}
	if !confirmed {
		pending, pendingErr := s.store.SplashPendingContext(ctx, s.boot.BootID, s.boot.ImageSHA256)
		if pendingErr != nil || !pending {
			return pendingErr
		}
		// Acceptance is durable, but without an installer there is nothing to
		// publish and the watchdog must stay armed.
		if s.splash == nil {
			return nil
		}
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	// A splash miss keeps the admission gate down. Status still succeeds so a
	// client can observe the trial and retry confirm; that call reports the error.
	if err = s.installSplash(ctx, s.boot.ImageSHA256); err != nil {
		return nil
	}
	if !confirmed {
		if err = s.store.CompleteSplashContext(ctx, s.boot.BootID, s.boot.ImageSHA256); err != nil {
			return err
		}
	}
	return s.finishConfirmation(ctx)
}

// finishConfirmation clears the boot ticket and opens admission once the
// release store has durably recorded confirmation. ErrBootTicketRewrite is
// that durable result with a failed ticket rewrite: it is logged, admission
// opens, and the rewrite is retried once on a later idle status read.
func (s *Service) finishConfirmation(ctx context.Context) error {
	err := s.store.ConfirmContext(ctx, s.boot.BootID, s.boot.ImageSHA256)
	repair := false
	if err != nil {
		if !errors.Is(err, appliance.ErrBootTicketRewrite) {
			return err
		}
		confirmed, confirmErr := s.store.ConfirmedContext(ctx, s.boot.BootID, s.boot.ImageSHA256)
		if confirmErr != nil {
			return confirmErr
		}
		if !confirmed {
			return err
		}
		repair = true
	}
	s.mu.Lock()
	s.trial = false
	s.blocked = false
	s.ticketRepair = repair
	s.coordinator.SetUpdateBlocked(false)
	s.mu.Unlock()
	if repair {
		s.logBootTicket(err)
	}
	return nil
}

// retryBootTicket makes the single follow-up rewrite. The repair is consumed
// only once ConfirmContext attempts the boot.json rewrite. Context
// cancellation or a timeout before that attempt, including a store-lock wait,
// leaves the repair pending. A rewrite failure still spends the one retry.
func (s *Service) retryBootTicket(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.store.ConfirmContext(ctx, s.boot.BootID, s.boot.ImageSHA256)
	if err != nil && !errors.Is(err, appliance.ErrBootTicketRewrite) {
		return err
	}
	s.mu.Lock()
	s.ticketRepair = false
	s.mu.Unlock()
	if err != nil {
		s.logBootTicket(err)
	}
	return nil
}

func (s *Service) logBootTicket(err error) {
	slog.Warn("appliance boot ticket rewrite failed after durable confirmation", "boot_id", s.boot.BootID, "err", err)
}

// repairSplash publishes the running image's verified idle core when FAT does
// not already match. It uses the image's own bytes, not a staged copy.
func (s *Service) repairSplash(ctx context.Context) error {
	if s.splash == nil {
		return nil
	}
	want := s.splash.IdleSHA256()
	if want != "" && s.splash.FATSHA256() == want {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.installSplash(ctx, s.boot.ImageSHA256)
}

// noteSplashRepair records a non-trial publication failure for status reads.
// A cancelled repair still returns the context error. The same failure text
// is logged once; a later successful repair clears it.
func (s *Service) noteSplashRepair(err error) error {
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return err
	}
	message := ""
	if err != nil {
		message = splashRepairMessage(err)
	}
	s.mu.Lock()
	previous := s.splashWarning
	s.splashWarning = message
	s.mu.Unlock()
	if message == previous {
		return nil
	}
	if message == "" {
		slog.Info("splash repair succeeded", "image_sha256", s.boot.ImageSHA256)
		return nil
	}
	slog.Warn("splash repair failed", "error", message, "image_sha256", s.boot.ImageSHA256)
	return nil
}

// splashRepairMessage drops the random atomic-publish temporary name so a
// polled status stays comparable. The directory and the OS error remain.
func splashRepairMessage(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && pathErr.Err != nil {
		path := pathErr.Path
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") && strings.HasSuffix(base, ".tmp") {
			path = filepath.Dir(path)
		}
		return pathErr.Op + " " + path + ": " + pathErr.Err.Error()
	}
	return err.Error()
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
	if s.splash == nil {
		return s.finishConfirmation(ctx)
	}
	// Stage and accept before replacing FAT. A digest mismatch must not accept
	// the trial, and a failed trial must still boot the previous splash.
	// Acceptance does not record the watchdog confirmation: that waits until
	// the FAT publish succeeds, so a failed publish leaves the guard armed.
	if err := s.stageSplash(ctx, hash); err != nil {
		return err
	}
	if err := s.store.AcceptTrialContext(ctx, bootID, hash); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.publishSplash(ctx, hash); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.store.CompleteSplashContext(ctx, bootID, hash); err != nil {
		return err
	}
	return s.finishConfirmation(ctx)
}

func (s *Service) installSplash(ctx context.Context, hash string) error {
	if err := s.stageSplash(ctx, hash); err != nil {
		return err
	}
	return s.publishSplash(ctx, hash)
}

func (s *Service) stageSplash(ctx context.Context, hash string) error {
	if s.splash == nil {
		return nil
	}
	return s.splash.Stage(ctx, hash)
}

func (s *Service) publishSplash(ctx context.Context, hash string) error {
	if s.splash == nil {
		return nil
	}
	return s.splash.Publish(ctx, hash)
}
