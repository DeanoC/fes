// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	stdimage "image"
	"io"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
)

// ProgramMenuPlatform presents the actual software device's completed frame.
// The existing input/state platform stays responsible for input and diagnostics.
type ProgramMenuPlatform struct {
	service     *ProgramService
	inner       tenfoot.Platform
	framebuffer func() *stdimage.RGBA
	dmaPollAt   time.Time
	dmaNotified uint64
	dmaArmed    uint64
}

func NewProgramMenuPlatform(service *ProgramService, inner tenfoot.Platform, framebuffer func() *stdimage.RGBA) (*ProgramMenuPlatform, error) {
	if service == nil || service.state == nil || inner == nil || framebuffer == nil {
		return nil, ErrProgramSession
	}
	p := service.state
	p.operation.Lock()
	defer p.operation.Unlock()
	if p.menu == nil || p.menu.presentationClaimed || service.Status().State != ProgramReady {
		return nil, ErrProgramSession
	}
	p.menu.presentationClaimed = true
	return &ProgramMenuPlatform{service: service, inner: inner, framebuffer: framebuffer}, nil
}

func (p *ProgramMenuPlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	input, err := p.inner.Next(ctx)
	if err != nil && !errors.Is(err, io.EOF) {
		return input, p.service.Retire(err)
	}
	if err != nil {
		return input, err
	}
	now := time.Now()
	if p.dmaArmed != 0 && p.dmaArmed != p.dmaNotified && !now.Before(p.dmaPollAt) {
		p.dmaPollAt = now.Add(25 * time.Millisecond)
		generation, rejected, faultErr := p.service.pollDMAReject(ctx)
		if faultErr != nil {
			return input, p.service.Retire(faultErr)
		}
		if rejected && generation == p.dmaArmed {
			p.dmaNotified = generation
			for _, command := range input.Commands {
				if command == tenfoot.CmdStop || command == tenfoot.CmdQuit {
					return input, nil
				}
			}
			input.Commands = append(input.Commands, tenfoot.CmdStop)
		}
	}
	return input, nil
}

func (p *ProgramMenuPlatform) Present(ctx context.Context, snapshot tenfoot.Snapshot) error {
	p.dmaArmed = 0
	if snapshot.Session.State == "active" {
		if view := p.service.Status(); view.State == ProgramRunning {
			p.dmaArmed = view.Generation
		}
	}
	if err := p.inner.Present(ctx, snapshot); err != nil {
		return p.service.Retire(err)
	}
	var serviceInput func(context.Context) error
	if input, ok := p.inner.(interface{ serviceMenuInput(context.Context) error }); ok {
		serviceInput = input.serviceMenuInput
	}
	if err := p.service.presentMenu(ctx, p.framebuffer(), serviceInput); err != nil {
		return p.service.Retire(err)
	}
	return nil
}
