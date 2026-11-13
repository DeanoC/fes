// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	stdimage "image"
	"io"

	"github.com/DeanoC/FogCast/ui/tenfoot"
)

// ProgramMenuPlatform presents the actual software device's completed frame.
// The existing input/state platform stays responsible for input and diagnostics.
type ProgramMenuPlatform struct {
	service     *ProgramService
	inner       tenfoot.Platform
	framebuffer func() *stdimage.RGBA
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
	return &ProgramMenuPlatform{service, inner, framebuffer}, nil
}

func (p *ProgramMenuPlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	input, err := p.inner.Next(ctx)
	if err != nil && !errors.Is(err, io.EOF) {
		return input, p.service.Retire(err)
	}
	return input, err
}

func (p *ProgramMenuPlatform) Present(ctx context.Context, snapshot tenfoot.Snapshot) error {
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
