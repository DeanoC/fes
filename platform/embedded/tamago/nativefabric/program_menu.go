// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	stdimage "image"
	"math"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/computer"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
)

// ProgramMenuOptions belongs to trusted board startup, not a launch request.
// Request names retained menu assets; an omitted ID selects the compiled menu.
// The complete uncached arena remains owned until reboot, including failures.
type ProgramMenuOptions struct {
	Request packagestore.Request
	Reserve func([]byte) (*Region, error)
	Limits  scanout.MenuLimits
	// Observe must be bounded and must not reenter service operations. Its
	// failure retires the owner; observations cannot authorize any transition.
	Observe func(ProgramMenuObservation) error
}

type programServiceMenu struct {
	request                               packagestore.Request
	reserve                               func([]byte) (*Region, error)
	limits                                scanout.MenuLimits
	plan                                  *StoredConfigurationPlan
	transfer                              *ConfigurationTransfer
	session                               *MenuSession
	returned                              *MenuSession
	frame                                 *stdimage.RGBA
	observe                               func(ProgramMenuObservation) error
	index                                 uint64
	reserved, closed, presentationClaimed bool
}

// NewProgramServiceWithMenu opens and presents an initial frame before Ready.
// Catalog reads must precede construction: Ready now owns an active menu and
// contained SD. Launch/Return perform all menu/core/SD transitions internally.
// As with NewProgramService, keep a returned service rooted after any error.
func NewProgramServiceWithMenu(ctx context.Context, loader *ProgramLoader, store *packagestore.Store,
	reserve func([]byte) (*Region, error), limits computer.Limits, options ProgramMenuOptions) (*ProgramService, error) {
	if ctx == nil || loader == nil || loader.menu == nil || options.Reserve == nil || options.Limits.Validate() != nil {
		return nil, errors.New("native program menu: compiled companion and bounded platform hooks required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	options.Request.Sources = append([]packagestore.Source(nil), options.Request.Sources...)
	options.Request.Parts = append([]packagestore.Part(nil), options.Request.Parts...)
	if options.Request.PackageID == "" {
		options.Request.PackageID = loader.menu.context.PackageID
	}
	s, err := NewProgramService(loader, store, reserve, limits)
	if err != nil {
		return s, err
	}
	p := s.state
	p.menu = &programServiceMenu{request: options.Request, reserve: options.Reserve, limits: options.Limits, observe: options.Observe,
		frame: stdimage.NewRGBA(stdimage.Rect(0, 0, 1280, 720))}
	p.operation.Lock()
	defer p.operation.Unlock()
	view := s.Status()
	view.State = ProgramLoading
	p.publish(view)
	if err := p.loadMenu(ctx); err != nil {
		return s, p.fail(err)
	}
	view.State = ProgramReady
	p.publish(view)
	return s, nil
}

func (p *programService) loadMenu(ctx context.Context) error {
	m := p.menu
	if m == nil || m.index == math.MaxUint64 || (m.session != nil && !m.closed) {
		return ErrProgramSession
	}
	if err := p.checkpointPreferences(ctx); err != nil {
		return err
	}
	selection, err := p.store.Retain(ctx, m.request)
	if err != nil {
		return err
	}
	plan, err := p.loader.Preflight(selection)
	if err != nil {
		return err
	}
	if plan.state == nil || plan.state.program == nil || plan.state.program.role != programRoleMenu {
		return errors.New("native program menu: internal request is not a menu")
	}
	m.plan = plan
	handoff, err := p.store.Contain(selection)
	if err != nil {
		return err
	}
	if !m.reserved {
		if _, err = p.loader.ReserveMenuMemory(m.reserve); err != nil {
			return err
		}
		m.reserved = true
	}
	m.transfer, err = p.newTransfer(plan, handoff)
	if err != nil {
		return err
	}
	if err = p.configureTransfer(ctx, m.transfer); err != nil {
		return err
	}
	m.session, err = p.loader.openMenu(ctx, context.Background(), m.transfer, m.limits)
	m.closed = false
	if err != nil {
		return err
	}
	m.index++
	if _, err = m.session.PresentFrame(ctx, m.frame); err != nil {
		return err
	}
	return p.observeMenu(nil)
}

func (p *programService) restoreMenu() error {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	return p.loadMenu(ctx)
}

func (p *programService) closeMenu(resume bool) error {
	m := p.menu
	if m == nil || m.session == nil || m.closed {
		return ErrProgramSession
	}
	m.closed = true
	returned, err := m.session.Close()
	if err != nil {
		return err
	}
	if err = p.observeMenu(&returned); err != nil {
		return err
	}
	if !resume {
		return nil
	}
	if err = m.session.ResumeStorageAfterReturn(); err != nil {
		return err
	}
	active, err := p.store.ActiveReceipt()
	if err != nil || active.Owner != returned.After.Owner || returned.After.Generation > math.MaxUint64-2 ||
		active.Generation != returned.After.Generation+2 {
		return errors.Join(err, errors.New("native program menu: storage lost its owned return"))
	}
	p.rearm = true
	m.returned = m.session
	return nil
}

// A menu return carries the pinned arena and original store identity through
// the next transfer, including restoring the menu after content rejection.
func (p *programService) newTransfer(plan *StoredConfigurationPlan, handoff *packagestore.Handoff) (*ConfigurationTransfer, error) {
	var transfer *ConfigurationTransfer
	var err error
	if p.menu != nil && p.menu.returned != nil {
		returned := p.menu.returned
		p.menu.returned = nil
		transfer, _, err = returned.NewStoredReconfigurationTransfer(plan, handoff, p.reserve)
	} else if p.rearm {
		transfer, _, err = plan.NewReconfigurationTransfer(handoff, p.reserve)
	} else {
		transfer, _, err = plan.NewTransfer(handoff, p.reserve)
	}
	return transfer, err
}

// PresentMenu consumes only completed launcher frames. Busy/loading/running
// transitions keep the previous launcher cache and perform no scanout I/O.
func (s *ProgramService) PresentMenu(ctx context.Context, frame *stdimage.RGBA) error {
	return s.presentMenu(ctx, frame, nil)
}

func (s *ProgramService) presentMenu(ctx context.Context, frame *stdimage.RGBA, serviceInput func(context.Context) error) error {
	if s == nil || s.state == nil || ctx == nil {
		return ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p := s.state
	if !p.operation.TryLock() {
		return nil
	}
	defer p.operation.Unlock()
	view := s.Status()
	if view.State == ProgramRebootRequired || view.State == ProgramParked {
		return ErrProgramRetired
	}
	if p.menu == nil {
		return ErrProgramSession
	}
	if view.State != ProgramReady {
		return nil
	}
	if frame == nil || frame.Bounds() != stdimage.Rect(0, 0, 1280, 720) || frame.Stride != 5120 ||
		len(frame.Pix) < 5120*720 {
		return errors.New("native program menu: complete tightly packed 1280x720 frame required")
	}
	for row := 0; row < 720; row++ {
		if serviceInput != nil && row%4 == 0 {
			if err := serviceInput(ctx); err != nil {
				return p.fail(err)
			}
		}
		copy(p.menu.frame.Pix[row*5120:(row+1)*5120], frame.Pix[row*frame.Stride:row*frame.Stride+5120])
	}
	if p.menu.session == nil || p.menu.closed {
		return p.fail(ErrProgramSession)
	}
	if _, err := p.menu.session.presentFrame(ctx, p.menu.frame, serviceInput); err != nil {
		return p.fail(err)
	}
	if err := p.observeMenu(nil); err != nil {
		return p.fail(err)
	}
	return nil
}

// ParkMenu is final application shutdown from a displayed Ready menu. It
// independently closes once and leaves SD contained, with no later launch.
func (s *ProgramService) ParkMenu(ctx context.Context) error {
	p, view, err := s.begin(ctx)
	if err != nil {
		return err
	}
	defer p.operation.Unlock()
	if p.menu == nil || view.State != ProgramReady || view.Generation == math.MaxUint64 {
		return ErrProgramSession
	}
	p.preferences.seal(nil)
	checkpoint := false
	if p.preferences != nil {
		state := p.preferences.PreferenceSaveState()
		checkpoint = state.Pending && !state.Failed
	}
	if err := p.closeMenu(checkpoint); err != nil {
		return p.fail(err)
	}
	if checkpoint {
		write, cancel := context.WithTimeout(ctx, 100*time.Second)
		err := p.checkpointPreferences(write)
		cancel()
		if _, activeErr := p.store.ActiveReceipt(); activeErr != nil {
			return p.fail(errors.Join(err, activeErr))
		}
		err = errors.Join(err, p.store.Quiesce())
		if err != nil {
			return p.fail(err)
		}
	}
	view.State = ProgramParked
	view.Generation++
	p.publish(view)
	return nil
}

// Retire is an unrecoverable frontend failure. Wait for any accepted owned
// transition to finish, then close known sessions once, without resuming SD.
func (s *ProgramService) Retire(cause error) error {
	if s == nil || s.state == nil || cause == nil {
		return ErrProgramSession
	}
	p := s.state
	p.operation.Lock()
	defer p.operation.Unlock()
	if s.Status().State == ProgramRebootRequired {
		return errors.Join(ErrProgramRetired, cause)
	}
	return p.fail(cause)
}
