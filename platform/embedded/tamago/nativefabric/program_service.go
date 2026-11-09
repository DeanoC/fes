// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
)

type ProgramServiceState string

const (
	ProgramReady          ProgramServiceState = "ready"
	ProgramLoading        ProgramServiceState = "loading"
	ProgramRunning        ProgramServiceState = "running"
	ProgramReturning      ProgramServiceState = "returning"
	ProgramParked         ProgramServiceState = "parked"
	ProgramRebootRequired ProgramServiceState = "reboot_required"
)

var (
	ErrProgramBusy    = errors.New("native program service: another operation is active")
	ErrProgramSession = errors.New("native program service: stale session or generation")
	ErrProgramRetired = errors.New("native program service: reboot required; resources retained")
)

// Status is a cached application view. Owner identifies this boot, Generation
// identifies this service's requests; neither is a hardware permission.
type ProgramServiceStatus struct {
	State            ProgramServiceState
	Owner            uint64
	Generation       uint64
	Identity         fabricmodel.PackageIdentity
	ROMReceiptSHA256 string `json:"rom_receipt_sha256,omitempty"`
	TapeSHA256       string `json:"tape_sha256,omitempty"`
	TapeBytes        int    `json:"tape_bytes,omitempty"`
	Held             bool
	Error            string
}

// ProgramSession can only be minted by Launch. Copies refer to the same private
// service; a returned, foreign or fabricated handle cannot perform control I/O.
type ProgramSession struct {
	service    *programService
	generation uint64
}

func (s ProgramSession) Generation() uint64 { return s.generation }

// ProgramService serializes request-driven retained loads and control-only
// computer sessions. Its actual platform calls are the same ProgramLoader,
// transfer, ComputerSession and SD-return operations as the bounded diagnostic.
// It supplies no normal programming profile, display grant or boot proof.
// Keep this handle (including after constructor errors) and its platform arena
// until reboot. Copies share all mutable state and cannot replay a transition.
type ProgramService struct{ state *programService }

type programService struct {
	operation      sync.Mutex
	viewMu         sync.Mutex
	view           ProgramServiceStatus
	loader         *ProgramLoader
	store          *packagestore.Store
	reserve        func([]byte) (*Region, error)
	limits         computer.Limits
	plan           *StoredConfigurationPlan
	transfer       *ConfigurationTransfer
	computer       *ComputerSession
	closed         bool
	rearm          bool
	backendClaimed bool
	execution      ProgramFetchObservation
	menu           *programServiceMenu
	preferences    *ProgramPreferences
}

// NewProgramService takes exclusive lifecycle ownership of this verifier once.
// The caller supplies the owned store, permanent arena and bounded platform
// clock/yield hooks. A package cannot choose any of those resources.
func NewProgramService(loader *ProgramLoader, store *packagestore.Store, reserve func([]byte) (*Region, error), limits computer.Limits) (*ProgramService, error) {
	if loader == nil || loader.boot == nil || store == nil || reserve == nil ||
		limits.Now == nil || limits.Yield == nil || limits.Timeout <= 0 || limits.Timeout > time.Second || limits.MaxPolls == 0 || limits.MaxPolls > 64 {
		return nil, errors.New("native program service: owned loader/store/arena and bounded limits required")
	}
	v := loader.boot
	v.computerMu.Lock()
	if v.computerServiceClaimed || v.computerGeneration != 0 {
		v.computerMu.Unlock()
		return nil, errors.New("native program service: lifecycle already claimed")
	}
	v.computerServiceClaimed = true
	v.computerMu.Unlock()
	p := &programService{loader: loader, store: store, reserve: reserve, limits: limits,
		view: ProgramServiceStatus{State: ProgramReady, Held: true}}
	s := &ProgramService{p}
	receipt, err := store.ActiveReceipt()
	if err != nil {
		return s, p.fail(err)
	}
	// The real SiP context service rejects an active SD device lease. This
	// cached owner is only the application's request token. NewTransfer still
	// verifies the immutable boot source and exact owner after SD containment.
	p.view.Owner = receipt.Owner
	return s, nil
}

func (s *ProgramService) Status() ProgramServiceStatus {
	if s == nil || s.state == nil {
		return ProgramServiceStatus{State: ProgramRebootRequired, Error: ErrProgramRetired.Error()}
	}
	p := s.state
	p.viewMu.Lock()
	defer p.viewMu.Unlock()
	return p.view
}

func (p *programService) publish(view ProgramServiceStatus) {
	p.viewMu.Lock()
	p.view = view
	p.viewMu.Unlock()
}

func (s *ProgramService) begin(ctx context.Context) (*programService, ProgramServiceStatus, error) {
	if s == nil || s.state == nil || ctx == nil {
		return nil, ProgramServiceStatus{}, ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return nil, ProgramServiceStatus{}, err
	}
	p := s.state
	if !p.operation.TryLock() {
		return nil, ProgramServiceStatus{}, ErrProgramBusy
	}
	view := s.Status()
	if view.State == ProgramRebootRequired || view.State == ProgramParked {
		p.operation.Unlock()
		return nil, view, ErrProgramRetired
	}
	return p, view, nil
}

// fail is terminal. Close is attempted at most once if control was opened;
// failed transfers/regions and plans stay retained and SD is never resumed.
func (p *programService) fail(err error) error {
	p.preferences.seal(err)
	if p.computer != nil && !p.closed {
		p.closed = true
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, cleanup := p.computer.Close(ctx)
		cancel()
		err = errors.Join(err, cleanup)
	}
	if p.menu != nil && p.menu.session != nil && !p.menu.closed {
		p.menu.closed = true
		_, cleanup := p.menu.session.Close()
		err = errors.Join(err, cleanup)
	}
	p.viewMu.Lock()
	p.view.State, p.view.Error = ProgramRebootRequired, err.Error()
	p.viewMu.Unlock()
	return errors.Join(ErrProgramRetired, err)
}

// Launch checks the caller's last Ready generation before any file/firmware I/O.
// Content rejection leaves healthy SD usable. After containment, any uncertainty
// retires the service. Success requires live identity, stable held fetch samples
// and independent fetch-counter progress after one Run, within the existing bound.
func (s *ProgramService) Launch(ctx context.Context, generation uint64, request packagestore.Request) (ProgramSession, error) {
	p, view, err := s.begin(ctx)
	if err != nil {
		return ProgramSession{}, err
	}
	defer p.operation.Unlock()
	if view.State != ProgramReady || view.Generation != generation || generation > math.MaxUint64-2 {
		return ProgramSession{}, ErrProgramSession
	}
	view.State, view.Error = ProgramLoading, ""
	p.publish(view)
	if p.menu != nil {
		if err := p.closeMenu(true); err != nil {
			return ProgramSession{}, p.fail(err)
		}
	}
	request.Sources = append([]packagestore.Source(nil), request.Sources...)
	request.Parts = append([]packagestore.Part(nil), request.Parts...)
	err = p.checkpointPreferences(ctx)
	var selection *packagestore.Selection
	if err == nil {
		selection, err = p.store.Retain(ctx, request)
	}
	var plan *StoredConfigurationPlan
	if err == nil {
		plan, err = p.loader.Preflight(selection)
	}
	if err == nil && plan.state.program != nil && plan.state.program.role == programRoleMenu {
		err = errors.New("native program service: menu package is not a playable computer")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if _, storageErr := p.store.ActiveReceipt(); storageErr != nil {
			return ProgramSession{}, p.fail(errors.Join(err, storageErr))
		}
		if p.menu != nil {
			if restoreErr := p.restoreMenu(); restoreErr != nil {
				return ProgramSession{}, p.fail(errors.Join(err, restoreErr))
			}
		}
		view.State, view.Error = ProgramReady, err.Error()
		p.publish(view)
		return ProgramSession{}, err
	}
	p.plan = plan // Pin the plan before containment or a possibly partial reservation.
	view.Generation++
	view.Identity = plan.Identity()
	view.ROMReceiptSHA256 = plan.ROMReceiptSHA256()
	p.publish(view)
	handoff, err := p.store.Contain(selection)
	if err != nil {
		return ProgramSession{}, p.fail(err)
	}
	p.transfer, err = p.newTransfer(plan, handoff)
	if err != nil {
		return ProgramSession{}, p.fail(err)
	}
	if err = p.configure(ctx); err != nil {
		return ProgramSession{}, p.fail(err)
	}
	control, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p.computer, err = p.loader.OpenComputer(control, p.transfer, p.limits)
	p.closed = false
	if err != nil {
		return ProgramSession{}, p.fail(err)
	}
	execution, err := p.startExecution(control, view)
	if err != nil {
		return ProgramSession{}, p.fail(err)
	}
	view.State, view.Held = ProgramRunning, false
	if program := plan.state.program; program != nil && len(program.tape) != 0 {
		sum := sha256.Sum256(program.tape)
		view.TapeSHA256, view.TapeBytes = hex.EncodeToString(sum[:]), len(program.tape)
	}
	p.viewMu.Lock()
	p.execution, p.view = execution, view
	p.viewMu.Unlock()
	return ProgramSession{p, view.Generation}, nil
}

func (p *programService) configure(ctx context.Context) error {
	return p.configureTransfer(ctx, p.transfer)
}

func (p *programService) configureTransfer(ctx context.Context, transfer *ConfigurationTransfer) error {
	start := p.limits.Now()
	last := start
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := p.limits.Now()
		elapsed, step := now.Sub(start), now.Sub(last)
		if now.Before(last) {
			return fmt.Errorf("native program service: configuration backwards clock: elapsed=%s step=%s limit=20s", elapsed, step)
		}
		if elapsed >= 20*time.Second {
			return fmt.Errorf("native program service: configuration deadline: elapsed=%s step=%s limit=20s", elapsed, step)
		}
		last = now
		return nil
	}
	for {
		if err := check(); err != nil {
			return transfer.Cancel(err)
		}
		done, busy, err := transfer.Advance()
		if late := check(); late != nil {
			return transfer.Cancel(late)
		}
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		p.limits.Yield()
		if busy {
			time.Sleep(time.Millisecond)
		}
	}
}

func (p *programService) session(view ProgramServiceStatus, session ProgramSession) bool {
	return view.State == ProgramRunning && session.service == p && session.generation == view.Generation && p.computer != nil && !p.closed
}

// Return completes Hold/closure and fresh SD resumption before publishing Ready.
// Once accepted, cleanup has its own bounded context and finishes even if the
// requester disconnects. An unknown return or SD grant is never retried.
func (s *ProgramService) Return(ctx context.Context, session ProgramSession) (ComputerObservation, error) {
	return s.finish(ctx, session, true)
}

// Park is the terminal bounded-test handoff from a running session. It closes
// that core once and leaves SD contained; no subsequent request can reactivate it.
func (s *ProgramService) Park(ctx context.Context, session ProgramSession) (ComputerObservation, error) {
	return s.finish(ctx, session, false)
}

func (s *ProgramService) finish(ctx context.Context, session ProgramSession, resume bool) (ComputerObservation, error) {
	p, view, err := s.begin(ctx)
	if err != nil {
		return ComputerObservation{}, err
	}
	defer p.operation.Unlock()
	if !p.session(view, session) {
		return ComputerObservation{}, ErrProgramSession
	}
	view.State = ProgramReturning
	p.publish(view)
	control, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.closed = true
	observed, err := p.computer.Close(control)
	if err != nil || !observed.HoldAcknowledged {
		return observed, p.fail(errors.Join(err, errors.New("native program service: owned Hold/return incomplete")))
	}
	if err = control.Err(); err != nil {
		return observed, p.fail(err)
	}
	if resume {
		if err = p.loader.boot.ResumeStorageAfterReturn(p.store); err != nil {
			return observed, p.fail(err)
		}
		if err = control.Err(); err != nil {
			return observed, p.fail(err)
		}
		active, err := p.store.ActiveReceipt()
		if err != nil || active.Owner != observed.After.Owner || observed.After.Generation > math.MaxUint64-2 || active.Generation != observed.After.Generation+2 {
			return observed, p.fail(errors.Join(err, errors.New("native program service: SD lost the owned return")))
		}
		view.State = ProgramReady
		p.rearm = true
		if p.menu != nil {
			// The known core close/SD return keeps its original five-second
			// bound. A fresh menu load has the existing request budget; its
			// configuration and frame operations retain their own strict limits.
			err = p.restoreMenu()
			if err != nil {
				return observed, p.fail(err)
			}
		}
	} else {
		view.State = ProgramParked
		p.preferences.seal(errors.New("native program UI: storage remains contained at shutdown"))
	}
	view.Generation++
	view.Identity, view.Held = fabricmodel.PackageIdentity{}, true
	view.ROMReceiptSHA256 = ""
	view.TapeSHA256, view.TapeBytes = "", 0
	p.rearm = true
	p.publish(view)
	return observed, nil
}

// Activity, Execution and Keyboard keep live control behind the session token.
// They cannot expose a raw computer pointer outside this serialized owner.
func (s *ProgramService) Activity(ctx context.Context, session ProgramSession) (computer.FetchActivity, error) {
	var activity computer.FetchActivity
	err := s.control(ctx, session, func(c *computer.Computer) error {
		var err error
		activity, err = c.Activity(ctx)
		return err
	}, nil)
	return activity, err
}
func (s *ProgramService) Execution(ctx context.Context, session ProgramSession, run bool) error {
	return s.control(ctx, session, func(c *computer.Computer) error {
		if run {
			return c.Run(ctx)
		}
		return c.Hold(ctx)
	}, &run)
}
func (s *ProgramService) Keyboard(ctx context.Context, session ProgramSession, rows [9]uint16) error {
	return s.control(ctx, session, func(c *computer.Computer) error { return c.Keyboard(ctx, rows) }, nil)
}
func (s *ProgramService) control(ctx context.Context, session ProgramSession, perform func(*computer.Computer) error, running *bool) error {
	p, view, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer p.operation.Unlock()
	if !p.session(view, session) {
		return ErrProgramSession
	}
	if err := perform(p.computer.Computer()); err != nil {
		if errors.Is(err, computer.ErrArgument) {
			return err // Driver argument validation precedes I/O.
		}
		return p.fail(err)
	}
	if running != nil {
		view.Held = !*running
		p.publish(view)
	}
	return nil
}
