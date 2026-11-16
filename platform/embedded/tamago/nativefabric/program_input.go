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
	boundary          uint64
	menuArmed         bool
	returnRequested   bool
	fault             error
	pending           []input.Event
}

const maxProgramMenuEvents = 64

func NewProgramInputPlatform(base tenfoot.Platform, backend *ProgramBackend, source input.USBReports) (*ProgramInputPlatform, error) {
	if source == nil {
		return nil, ErrProgramSession
	}
	menu, err := input.NewUSBKeyboard(source)
	if err != nil {
		return nil, err
	}
	control, _ := source.(input.PollingControl)
	p, err := newProgramInputPlatform(base, backend, control)
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
	control, _ := source.(input.PollingControl)
	p, err := newProgramInputPlatform(base, backend, control)
	if err != nil {
		return nil, err
	}
	p.gamepadSource, p.gamepadMenu = source, menu
	p.gamepadDescriptor = append([]byte(nil), descriptor...)
	p.gamepadButtons, p.gamepadKeys = buttons, keys
	return p, nil
}
func newProgramInputPlatform(base tenfoot.Platform, backend *ProgramBackend, control programInputPolling) (*ProgramInputPlatform, error) {
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
	backend.inputPolling = control
	// A held control at boot is not a new menu press. Startup uses the same
	// real neutral-report fence as return; a missing report never arms input.
	return &ProgramInputPlatform{base: base, backend: backend}, nil
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
	events, err := p.poll(ctx, false)
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
	var commands []tenfoot.Command
	for _, event := range events {
		if command := tenfoot.CommandFromKey(event.Key); command != tenfoot.CmdNone {
			// Ordinary menu shortcuts are delivered only in Ready. The running
			// keyboard's explicit return chord also uses the App's Stop command.
			commands = append(commands, command)
		}
		frame.Text += event.Text
	}
	if observer, ok := p.base.(interface {
		observeProgramCommands(context.Context, string, []tenfoot.Command) error
	}); ok {
		source := "usb-boot-keyboard"
		if p.gamepadSource != nil {
			source = "usb-gamepad"
		}
		if err := observer.observeProgramCommands(ctx, source, commands); err != nil {
			p.fault = err
			return tenfoot.PlatformInput{}, err
		}
	}
	frame.Commands = append(frame.Commands, commands...)
	return frame, nil
}

// serviceMenuInput advances the same exclusively owned source while the menu
// copies/checks uncached pixels. Preserve decoded press edges until Next; never
// invoke App commands or reenter the service under its presentation lock.
func (p *ProgramInputPlatform) serviceMenuInput(ctx context.Context) error {
	if !p.operation.TryLock() {
		return ErrProgramBusy
	}
	defer p.operation.Unlock()
	if p.fault != nil {
		return p.fault
	}
	_, err := p.poll(ctx, true)
	if err != nil {
		p.fault = err
	}
	return err
}

func (p *ProgramInputPlatform) poll(ctx context.Context, buffer bool) ([]input.Event, error) {
	b := p.backend
	if !b.operation.TryLock() {
		return nil, nil
	} // An owned transition is in flight.
	defer b.operation.Unlock()
	if b.inputFault != nil {
		return nil, b.inputFault
	}
	v := b.service.Status()
	if v.State != ProgramReady && v.State != ProgramRunning {
		return nil, nil
	}
	if p.boundary != b.inputBoundary || p.mode != v.State || p.generation != v.Generation {
		p.pending = nil
		p.returnRequested = false
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
	if v.State == ProgramReady && (p.boundary != b.inputBoundary || p.mode != "" && (p.mode != ProgramReady || p.generation != v.Generation)) {
		if p.gamepadSource != nil {
			p.gamepadMenu, _ = input.NewUSBGamepad(p.gamepadSource, p.gamepadDescriptor, p.gamepadButtons)
		} else {
			p.menu, _ = input.NewUSBKeyboard(p.source)
		}
		p.menuArmed = false
	}
	p.mode, p.generation, p.boundary = v.State, v.Generation, b.inputBoundary
	events := p.pending
	p.pending = nil
	for n := 0; n < 8; n++ {
		// Once return is requested, keep the core neutral until the App starts
		// its owned transition. Repeated reports cannot queue another Stop.
		if v.State == ProgramRunning && p.returnRequested {
			break
		}
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
				sequence := p.gamepad.state.observation.Sequence
				err = p.gamepad.state.report(ctx, report)
				if err == nil && p.gamepad.state.observation.Sequence != sequence {
					if observer, ok := p.base.(interface {
						ObserveProgramGamepad(context.Context, ProgramGamepadObservation) error
					}); ok {
						err = observer.ObserveProgramGamepad(ctx, p.gamepad.state.observation)
					}
				}
			} else {
				var stop bool
				stop, err = p.coreKeyboard(ctx, report)
				if err == nil && stop {
					p.returnRequested = true
					changes = []input.Event{{Key: "s"}}
				}
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
		}
		events = append(events, changes...)
		if len(events) > maxProgramMenuEvents {
			b.inputFault = errors.New("native input: menu event queue exhausted")
			return nil, b.inputFault
		}
	}
	if buffer {
		p.pending = events
		return nil, nil
	}
	return events, nil
}

// Ctrl+Alt+Escape is a deliberate return to the launcher, separate from core
// typing and future in-play menus. Only a fresh Escape press after this play's
// all-up fence may request it. Validate with the same decoder before any write;
// the reserved chord itself is never forwarded to the emulated keyboard.
// Caller holds both input-platform and backend operation locks.
func (p *ProgramInputPlatform) coreKeyboard(ctx context.Context, report []byte) (bool, error) {
	k := p.keyboard.state
	next := k.decoder
	edges, err := next.Feed(report)
	if err != nil {
		return false, err
	}
	if k.armed {
		for _, edge := range edges {
			if edge.Pressed && edge.Usage == 0x29 && edge.Modifiers&0x11 != 0 && edge.Modifiers&0x44 != 0 {
				if err := k.writeRows(ctx, [9]uint16{}); err != nil {
					return false, err
				}
				k.decoder = next
				return true, nil
			}
		}
	}
	return false, k.report(ctx, report)
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
