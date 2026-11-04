// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

// ProgramInputPlatform routes one exclusively owned typed USB source through
// the real Tenfoot platform loop. The underlying platform retains pacing/output
// and any explicitly configured diagnostic controls. No USB hardware is opened.
// Keep this platform, backend, service and uncertain device resources until reboot.
type ProgramInputPlatform struct {
	operation         sync.Mutex
	base              tenfoot.Platform
	backend           *ProgramBackend
	source            input.USBReports
	menu              *input.USBKeyboard
	keyboard          ProgramKeyboard
	gamepadSource     input.USBGamepadReports
	gamepadMenu       *input.USBGamepad
	gamepad           ProgramGamepad
	gamepadDescriptor []byte
	gamepadButtons    input.GamepadButtons
	gamepadKeys       ProgramGamepadKeys
	session           ProgramSession
	mode              ProgramServiceState
	generation        uint64
	menuArmed         bool
	fault             error
}

func NewProgramInputPlatform(base tenfoot.Platform, backend *ProgramBackend, source input.USBReports) (*ProgramInputPlatform, error) {
	if source == nil {
		return nil, ErrProgramSession
	}
	menu, err := input.NewUSBKeyboard(source)
	if err != nil {
		return nil, err
	}
	p, err := newProgramInputPlatform(base, backend)
	if err != nil {
		return nil, err
	}
	p.source, p.menu = source, menu
	return p, nil
}

// NewProgramGamepadPlatform binds a descriptor-driven source and explicit menu
// and core mappings. It transfers local frontend input ownership only; firmware
// permission to coexist with SD/FPGA must be supplied independently by the BSP.
func NewProgramGamepadPlatform(base tenfoot.Platform, backend *ProgramBackend, source input.USBGamepadReports, descriptor []byte, buttons input.GamepadButtons, keys ProgramGamepadKeys) (*ProgramInputPlatform, error) {
	if source == nil || !keys.valid() {
		return nil, ErrProgramSession
	}
	menu, err := input.NewUSBGamepad(source, descriptor, buttons)
	if err != nil {
		return nil, err
	}
	p, err := newProgramInputPlatform(base, backend)
	if err != nil {
		return nil, err
	}
	p.gamepadSource, p.gamepadMenu = source, menu
	p.gamepadDescriptor = append([]byte(nil), descriptor...)
	p.gamepadButtons, p.gamepadKeys = buttons, keys
	return p, nil
}
func newProgramInputPlatform(base tenfoot.Platform, backend *ProgramBackend) (*ProgramInputPlatform, error) {
	if base == nil || backend == nil || backend.service == nil {
		return nil, ErrProgramSession
	}
	if !backend.operation.TryLock() {
		return nil, ErrProgramBusy
	}
	defer backend.operation.Unlock()
	if backend.inputAttached || backend.inputFault != nil || backend.service.Status().State != ProgramReady {
		return nil, ErrProgramBusy
	}
	backend.inputAttached = true
	return &ProgramInputPlatform{base: base, backend: backend, menuArmed: backend.service.Status().Generation == 0}, nil
}

func (p *ProgramInputPlatform) Next(ctx context.Context) (tenfoot.PlatformInput, error) {
	if ctx == nil {
		return tenfoot.PlatformInput{}, ErrProgramSession
	}
	if err := ctx.Err(); err != nil {
		return tenfoot.PlatformInput{}, err
	}
	if !p.operation.TryLock() {
		return tenfoot.PlatformInput{}, ErrProgramBusy
	}
	defer p.operation.Unlock()
	if p.fault != nil {
		return tenfoot.PlatformInput{}, p.fault
	}
	frame, err := p.base.Next(ctx)
	if err != nil {
		return frame, err
	}
	events, err := p.poll(ctx)
	if err != nil {
		// A failed source is never polled again. Close only this exact play with a
		// separate bounded control cleanup; do not resume SD or release its memory.
		p.fault = err
		if p.session.Generation() != 0 && p.backend.service.Status().State == ProgramRunning {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			p.fault = errors.Join(err, p.park(cleanup))
			cancel()
		}
		return tenfoot.PlatformInput{}, p.fault
	}
	for _, event := range events {
		if command := tenfoot.CommandFromKey(event.Key); command != tenfoot.CmdNone {
			// Menu shortcuts are delivered only in Ready. Running-core key
			// snapshots never become launcher stop/quit shortcuts.
			frame.Commands = append(frame.Commands, command)
		}
		frame.Text += event.Text
	}
	return frame, nil
}

func (p *ProgramInputPlatform) poll(ctx context.Context) ([]input.Event, error) {
	b := p.backend
	if !b.operation.TryLock() {
		return nil, nil
	} // An owned transition is in flight.
	defer b.operation.Unlock()
	v := b.service.Status()
	if v.State != ProgramReady && v.State != ProgramRunning {
		return nil, nil
	}
	if v.State == ProgramRunning {
		p.session = b.session
	}
	if v.State == ProgramRunning {
		var err error
		if p.gamepadSource != nil {
			if p.gamepad.state == nil || p.gamepad.state.keys.state.session.Generation() != v.Generation {
				p.gamepad, err = b.bindGamepad(ctx, v.Generation, p.gamepadDescriptor, p.gamepadKeys)
			}
		} else if p.keyboard.state == nil || p.keyboard.state.session.Generation() != v.Generation {
			p.keyboard, err = b.bindKeyboard(ctx, v.Generation)
		}
		if err != nil {
			b.inputFault = err
			return nil, err
		}
	}
	if v.State == ProgramReady && p.mode != "" && (p.mode != ProgramReady || p.generation != v.Generation) {
		if p.gamepadSource != nil {
			p.gamepadMenu, _ = input.NewUSBGamepad(p.gamepadSource, p.gamepadDescriptor, p.gamepadButtons)
		} else {
			p.menu, _ = input.NewUSBKeyboard(p.source)
		}
		p.menuArmed = false
	}
	p.mode, p.generation = v.State, v.Generation
	var events []input.Event
	for n := 0; n < 8; n++ {
		if err := ctx.Err(); err != nil {
			b.inputFault = err
			return nil, err
		}
		var report []byte
		var err error
		if p.gamepadSource != nil {
			report, err = p.gamepadSource.PollGamepad()
		} else {
			report, err = p.source.PollKeyboard()
		}
		if err != nil {
			b.inputFault = err
			return nil, err
		}
		if report == nil {
			break
		}
		var changes []input.Event
		if v.State == ProgramRunning {
			if p.gamepadSource != nil {
				err = p.gamepad.state.report(ctx, report)
			} else {
				err = p.keyboard.state.report(ctx, report)
			}
		} else {
			if p.gamepadSource != nil {
				changes, err = p.gamepadMenu.Decode(report)
			} else {
				changes, err = p.menu.Decode(report)
			}
		}
		if errors.Is(err, usb.ErrKeyboardRollover) {
			continue
		}
		if err != nil {
			b.inputFault = err
			return nil, err
		}
		if v.State == ProgramReady {
			if !p.menuArmed {
				up := false
				if p.gamepadSource != nil {
					up = input.GamepadNeutral(p.gamepadMenu.State())
				} else {
					up = report[0] == 0
					for _, key := range report[2:] {
						up = up && key == 0
					}
				}
				p.menuArmed = up
				continue
			}
			events = append(events, changes...)
		}
	}
	return events, nil
}

// park cannot affect a replacement session even if a stop races with a source
// failure. ProgramService performs the actual hold, master stop and closure.
func (p *ProgramInputPlatform) park(ctx context.Context) error {
	b := p.backend
	if !b.operation.TryLock() {
		return ErrProgramBusy
	}
	defer b.operation.Unlock()
	if b.service.Status().State != ProgramRunning || b.session != p.session || p.session.Generation() == 0 {
		return ErrProgramSession
	}
	defer b.publish()
	_, err := b.service.Park(ctx, p.session)
	if err == nil {
		if p.keyboard.state != nil {
			p.keyboard.state.closed = true
		}
		if p.gamepad.state != nil {
			p.gamepad.state.keys.state.closed = true
		}
		b.session, b.play = ProgramSession{}, programPlay{}
	}
	return err
}

func (p *ProgramInputPlatform) Present(ctx context.Context, snapshot tenfoot.Snapshot) error {
	return p.base.Present(ctx, snapshot)
}
