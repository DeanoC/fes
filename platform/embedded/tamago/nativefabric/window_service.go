// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	stdimage "image"
	"math"
	"sync"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

var (
	ErrWindowServiceBusy    = errors.New("native window service: operation active")
	ErrWindowServiceSession = errors.New("native window service: stale session or generation")
	ErrWindowServiceRetired = errors.New("native window service: reboot required; memory retained")
)

type WindowServiceStatus struct {
	State                     ProgramServiceState
	Owner, Generation, Frames uint64
	PackageID, Error          string
}

// WindowHandle also implements board.Display. Copies retain a private service
// and request generation; a public status receipt cannot manufacture a handle.
type WindowHandle struct {
	service    *windowService
	generation uint64
}

func (h WindowHandle) Generation() uint64 { return h.generation }
func (h WindowHandle) Present(frame *stdimage.RGBA) error {
	_, err := (&WindowService{h.service}).Present(context.Background(), h, frame)
	return err
}

// WindowService owns a fixed compiled window personality, its original SD store
// and pinned frame arena. It is not ordinary package admission or an HDMI grant.
// Keep this handle and its reservations rooted until reboot on every outcome.
type WindowService struct{ state *windowService }
type windowService struct {
	operation                        sync.Mutex
	viewMu                           sync.Mutex
	view                             WindowServiceStatus
	previous, current                *WindowSession
	store                            *packagestore.Store
	control, configuration, contract []byte
	reserve                          func([]byte) (*Region, error)
	limits                           scanout.ReaderLimits
	plan                             *StoredConfigurationPlan
	transfer                         *ConfigurationTransfer
}

// NewWindowService transfers exclusive ownership of a live session after its
// first complete presentation. The caller must cease using initial directly.
// Once adopted, request cancellation cannot revoke the service's lifetime.
// The constructor reads cached ownership only and performs no device/file I/O.
func NewWindowService(initial *WindowSession, store *packagestore.Store, control, configuration, contract []byte,
	reserve func([]byte) (*Region, error), limits scanout.ReaderLimits) (*WindowService, WindowHandle, error) {
	if initial == nil || initial.self != initial || initial.verifier == nil || !store.OwnsHandoff(initial.storage) || reserve == nil ||
		limits.Now == nil || limits.Yield == nil || limits.Timeout <= 0 || limits.Timeout > time.Second || limits.MaxPolls == 0 || limits.MaxPolls > 64 {
		return nil, WindowHandle{}, scanout.ErrReaderArgument
	}
	if !initial.active.CompareAndSwap(false, true) {
		return nil, WindowHandle{}, ErrWindowServiceBusy
	}
	defer initial.active.Store(false)
	if initial.closed || initial.fault != nil || !initial.known || !initial.drained || initial.frames == 0 || initial.surface == nil ||
		initial.context == nil || initial.context.Err() != nil || initial.source.Bridges.Owner == 0 ||
		sha256.Sum256(control) != initial.controlSHA || sha256.Sum256(contract) != initial.accessSHA {
		return nil, WindowHandle{}, ErrWindowServiceSession
	}
	v := initial.verifier
	if _, err := v.configurationMap(configuration); err != nil {
		return nil, WindowHandle{}, err
	}
	if _, err := v.checkedWindowRead(control, contract); err != nil {
		return nil, WindowHandle{}, err
	}
	if err := initial.checkArena(); err != nil {
		return nil, WindowHandle{}, err
	}
	if _, err := initial.storage.Selection(); err != nil {
		return nil, WindowHandle{}, err
	}
	if !v.windowServiceClaimed.CompareAndSwap(false, true) {
		return nil, WindowHandle{}, ErrWindowServiceSession
	}
	initial.context = context.Background()
	p := &windowService{current: initial, store: store, control: bytes.Clone(control), configuration: bytes.Clone(configuration), contract: bytes.Clone(contract),
		reserve: reserve, limits: limits, view: WindowServiceStatus{State: ProgramRunning, Owner: initial.source.Bridges.Owner, Generation: 1,
			Frames: initial.frames, PackageID: v.Identity().PackageID}}
	return &WindowService{p}, WindowHandle{p, 1}, nil
}

func (s *WindowService) Status() WindowServiceStatus {
	if s == nil || s.state == nil {
		return WindowServiceStatus{State: ProgramRebootRequired, Error: ErrWindowServiceRetired.Error()}
	}
	p := s.state
	p.viewMu.Lock()
	defer p.viewMu.Unlock()
	return p.view
}
func (p *windowService) publish(view WindowServiceStatus) {
	p.viewMu.Lock()
	p.view = view
	p.viewMu.Unlock()
}
func (s *WindowService) begin(ctx context.Context) (*windowService, WindowServiceStatus, error) {
	if s == nil || s.state == nil || ctx == nil {
		return nil, WindowServiceStatus{}, ErrWindowServiceSession
	}
	if err := ctx.Err(); err != nil {
		return nil, WindowServiceStatus{}, err
	}
	p := s.state
	if !p.operation.TryLock() {
		return nil, WindowServiceStatus{}, ErrWindowServiceBusy
	}
	view := s.Status()
	if view.State == ProgramRebootRequired || view.State == ProgramParked {
		p.operation.Unlock()
		return nil, view, ErrWindowServiceRetired
	}
	return p, view, nil
}
func (p *windowService) matches(view WindowServiceStatus, h WindowHandle) bool {
	return view.State == ProgramRunning && h.service == p && h.generation == view.Generation && p.current != nil
}
func (p *windowService) fail(err error) error {
	if p.current != nil {
		_, closed := p.current.Close()
		err = errors.Join(err, closed)
	}
	p.viewMu.Lock()
	p.view.State, p.view.Error = ProgramRebootRequired, err.Error()
	p.viewMu.Unlock()
	return errors.Join(ErrWindowServiceRetired, err)
}

// Present validates the request and geometry before touching a reader. An
// uncertain publication attempts the owned closure once and retires the service.
func (s *WindowService) Present(ctx context.Context, h WindowHandle, frame *stdimage.RGBA) (WindowPresentation, error) {
	p, view, err := s.begin(ctx)
	if err != nil {
		return WindowPresentation{}, err
	}
	defer p.operation.Unlock()
	if !p.matches(view, h) {
		return WindowPresentation{}, ErrWindowServiceSession
	}
	if err := p.current.surface.CheckFrame(frame); err != nil {
		return WindowPresentation{}, err
	}
	result, err := p.current.PresentFrame(ctx, frame)
	if err != nil {
		return WindowPresentation{}, p.fail(err)
	}
	view.Frames++
	p.publish(view)
	return result, nil
}

// Return completes closure and resumes the original SD store before Ready.
// Park closes without resuming SD and is terminal. Once accepted, either finish
// is independent of request cancellation; failed hardware calls are not retried.
func (s *WindowService) Return(ctx context.Context, h WindowHandle) (WindowSessionReturn, error) {
	return s.finish(ctx, h, true)
}
func (s *WindowService) Park(ctx context.Context, h WindowHandle) (WindowSessionReturn, error) {
	return s.finish(ctx, h, false)
}
func (s *WindowService) finish(ctx context.Context, h WindowHandle, resume bool) (WindowSessionReturn, error) {
	p, view, err := s.begin(ctx)
	if err != nil {
		return WindowSessionReturn{}, err
	}
	defer p.operation.Unlock()
	if !p.matches(view, h) || view.Generation == math.MaxUint64 {
		return WindowSessionReturn{}, ErrWindowServiceSession
	}
	view.State = ProgramReturning
	p.publish(view)
	returned, err := p.current.Close()
	if err != nil {
		return returned, p.fail(err)
	}
	if resume {
		if err = p.current.ResumeStorageAfterReturn(); err != nil {
			return returned, p.fail(err)
		}
		active, err := p.store.ActiveReceipt()
		if err != nil || active.Owner != returned.After.Owner || returned.After.Generation > math.MaxUint64-2 || active.Generation != returned.After.Generation+2 {
			return returned, p.fail(errors.Join(err, errors.New("native window service: SD lost owned return")))
		}
		view.State = ProgramReady
	} else {
		view.State = ProgramParked
	}
	p.previous, p.current = p.current, nil
	view.Generation++
	p.publish(view)
	return returned, nil
}

// Launch always freshly retains the selected archive, validates its compiled
// identity while SD is usable, contains SD, rearms and programs, then presents
// the first frame. Only that complete sequence publishes a new display handle.
// A rejected package leaves Ready usable. After containment, failure is terminal.
func (s *WindowService) Launch(ctx context.Context, generation uint64, request packagestore.Request, frame *stdimage.RGBA) (WindowHandle, error) {
	p, view, err := s.begin(ctx)
	if err != nil {
		return WindowHandle{}, err
	}
	defer p.operation.Unlock()
	if view.State != ProgramReady || view.Generation != generation || generation > math.MaxUint64-2 || p.previous == nil {
		return WindowHandle{}, ErrWindowServiceSession
	}
	if err := p.previous.surface.CheckFrame(frame); err != nil {
		return WindowHandle{}, err
	}
	view.State, view.Error = ProgramLoading, ""
	p.publish(view)
	request.Sources = append([]packagestore.Source(nil), request.Sources...)
	request.Parts = append([]packagestore.Part(nil), request.Parts...)
	selection, err := p.store.Retain(ctx, request)
	var plan *StoredConfigurationPlan
	if err == nil {
		plan, err = p.previous.verifier.PreflightStoredConfiguration(p.control, p.configuration, selection)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		if _, leaseErr := p.store.ActiveReceipt(); leaseErr != nil {
			return WindowHandle{}, p.fail(errors.Join(err, leaseErr))
		}
		view.State, view.Error = ProgramReady, err.Error()
		p.publish(view)
		return WindowHandle{}, err
	}
	p.plan = plan
	view.Generation++
	p.publish(view)
	handoff, err := p.store.Contain(selection)
	if err != nil {
		return WindowHandle{}, p.fail(err)
	}
	p.transfer, _, err = p.previous.NewStoredReconfigurationTransfer(plan, handoff, p.reserve)
	if err != nil {
		return WindowHandle{}, p.fail(err)
	}
	if err = p.configure(ctx); err != nil {
		return WindowHandle{}, p.fail(err)
	}
	// The per-request context bounds this launch, not the returned session.
	p.current, err = p.previous.verifier.OpenWindowSession(context.Background(), p.control, p.contract, p.transfer, p.limits)
	if err != nil {
		return WindowHandle{}, p.fail(err)
	}
	if _, err = p.current.PresentFrame(ctx, frame); err != nil {
		return WindowHandle{}, p.fail(err)
	}
	view.State, view.Frames = ProgramRunning, 1
	p.publish(view)
	return WindowHandle{p, view.Generation}, nil
}

func (p *windowService) configure(ctx context.Context) error {
	start := p.limits.Now()
	last := start
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := p.limits.Now()
		if now.Before(last) || now.Sub(start) >= 20*time.Second {
			return errors.New("native window service: configuration deadline or backwards clock")
		}
		last = now
		return nil
	}
	for {
		if err := check(); err != nil {
			return p.transfer.Cancel(err)
		}
		done, busy, err := p.transfer.Advance()
		if late := check(); late != nil {
			return p.transfer.Cancel(late)
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
