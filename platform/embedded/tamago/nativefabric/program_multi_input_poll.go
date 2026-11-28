// SPDX-License-Identifier: GPL-3.0-or-later

package nativefabric

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/FogCast/ui/tenfoot"
	cg "github.com/DeanoC/fes/platform/embedded/tamago/computer/generated"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/usbhub"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
)

func (p *ProgramMultiInputPlatform) serviceMenuInput(ctx context.Context) error {
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

func (p *ProgramMultiInputPlatform) runGameplayFastPath(ctx context.Context) (tenfoot.PlatformInput, bool, error) {
	timer := time.NewTimer(gameplayHousekeepingWindow)
	defer timer.Stop()
	for {
		events, err := p.poll(ctx, false)
		if err != nil {
			frame, faultErr := p.handleInputFault(err)
			return frame, true, faultErr
		}
		if len(events) > 0 || p.returnRequested {
			frame, err := p.applyEvents(ctx, tenfoot.PlatformInput{At: time.Now()}, events)
			return frame, true, err
		}
		if p.backend.service.Status().State != ProgramRunning {
			return tenfoot.PlatformInput{}, false, nil
		}
		select {
		case <-ctx.Done():
			return tenfoot.PlatformInput{}, true, ctx.Err()
		case <-p.inputReady:
			continue
		case <-p.uartReady:
			if w, ok := p.base.(interface{ wakeNext() }); ok {
				w.wakeNext()
			}
			return tenfoot.PlatformInput{}, false, nil
		case <-timer.C:
			return tenfoot.PlatformInput{}, false, nil
		}
	}
}

func (p *ProgramMultiInputPlatform) handleInputFault(err error) (tenfoot.PlatformInput, error) {
	p.fault = err
	if p.session.Generation() != 0 && p.backend.service.Status().State == ProgramRunning {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		p.fault = errors.Join(err, p.park(cleanup))
		cancel()
	}
	return tenfoot.PlatformInput{}, p.fault
}

func (p *ProgramMultiInputPlatform) applyEvents(ctx context.Context, frame tenfoot.PlatformInput, events []input.Event) (tenfoot.PlatformInput, error) {
	var commands []tenfoot.Command
	for _, event := range events {
		if command := tenfoot.CommandFromKey(event.Key); command != tenfoot.CmdNone {
			commands = append(commands, command)
		}
		frame.Text += event.Text
	}
	if observer, ok := p.base.(interface {
		observeProgramCommands(context.Context, string, []tenfoot.Command) error
	}); ok {
		if err := observer.observeProgramCommands(ctx, "usb-multi", commands); err != nil {
			p.fault = err
			return tenfoot.PlatformInput{}, err
		}
	}
	frame.Commands = append(frame.Commands, commands...)
	return frame, nil
}

func (p *ProgramMultiInputPlatform) poll(ctx context.Context, buffer bool) ([]input.Event, error) {
	b := p.backend
	if !b.operation.TryLock() {
		return nil, nil
	}
	defer b.operation.Unlock()
	if b.inputFault != nil {
		return nil, b.inputFault
	}
	v := b.service.Status()
	if v.State != ProgramReady && v.State != ProgramRunning {
		return nil, nil
	}
	boundaryChanged := p.boundary != b.inputBoundary || p.mode != v.State || p.generation != v.Generation
	if boundaryChanged {
		p.pending = nil
		p.returnRequested = false
		p.kbRows = [9]uint16{}
		p.padRows = [9]uint16{}
		p.rawCtrlPorts = [usbhub.MaxPlayerPorts]uint8{}
		p.ctrlPorts = [usbhub.MaxPlayerPorts]uint8{}
		p.nativeControllers = false
		p.nativeMouse = false
		p.mouseButtons = 0
		p.prevHome = false
		p.menuKB = usb.BootKeyboard{}
		p.coreKB = usb.BootKeyboard{}
		p.menuPadPressed = [10]bool{}
		// No source service has run since the boundary drained the queues,
		// so the live state is the state at the boundary. Inputs held (or
		// not yet reported) across it keep the fence closed until a queued
		// all-up snapshot; released ones let the first fresh press through.
		p.kbArmed = p.source.KeyboardNeutral()
		p.padArmed = p.source.GamepadNeutral()
		p.mouseArmed = p.source.MouseNeutral()
	}
	wantGameplay := v.State == ProgramRunning
	if !p.gameplaySynced || p.gameplayActive != wantGameplay {
		if err := p.source.SetGameplayActive(wantGameplay); err == nil {
			p.gameplaySynced = true
			p.gameplayActive = wantGameplay
			if wantGameplay {
				p.refreshBluetoothStatus(false)
			}
		}
	}
	if v.State == ProgramRunning {
		p.session = b.session
		boundNew := false
		if p.keyboard.state == nil || p.keyboard.state.session.Generation() != v.Generation {
			var err error
			p.keyboard, err = b.bindKeyboard(ctx, v.Generation)
			if err != nil {
				b.inputFault = err
				return nil, err
			}
			boundNew = true
		}
		caps := b.service.Capabilities(b.session)
		p.nativeControllers = caps&cg.FesComputerCapabilityGamepadPorts != 0
		p.nativeMouse = caps&cg.FesComputerCapabilityMouseRelative != 0
		if boundNew || boundaryChanged {
			if err := p.neutralizeControllerPorts(ctx); err != nil {
				b.inputFault = err
				return nil, err
			}
			if err := p.neutralizeMouse(ctx); err != nil {
				b.inputFault = err
				return nil, err
			}
		}
	}
	p.mode, p.generation, p.boundary = v.State, v.Generation, b.inputBoundary

	if err := p.source.Service(); err != nil {
		b.inputFault = err
		return nil, err
	}
	if p.cfg.App != nil {
		p.syncTenfootPadTopology(false)
	}

	events := p.pending
	p.pending = nil
	var edgeBuf [28]usb.KeyEvent

	kbDrained := false
	for n := 0; n < 8; n++ {
		if v.State == ProgramRunning && p.returnRequested {
			break
		}
		if err := ctx.Err(); err != nil {
			b.inputFault = err
			return nil, err
		}
		rep, ok := p.source.NextKeyboardReport()
		if !ok {
			kbDrained = true
			break
		}
		if p.traceKeyboard != nil {
			p.traceKeyboard(ctx, v, rep[:], p.kbArmed)
		}
		if v.State == ProgramRunning {
			next := p.coreKB
			edgeCount, err := next.FeedInto(rep[:], edgeBuf[:])
			if errors.Is(err, usb.ErrKeyboardRollover) {
				continue
			}
			if err != nil {
				b.inputFault = err
				return nil, err
			}
			if !p.kbArmed {
				p.coreKB = next
				p.kbArmed = rep == ([8]byte{})
				continue
			}
			for _, edge := range edgeBuf[:edgeCount] {
				if edge.Pressed && edge.Usage == 0x29 && edge.Modifiers&0x11 != 0 && edge.Modifiers&0x44 != 0 {
					if err := p.neutralizeCore(ctx); err != nil {
						b.inputFault = err
						return nil, err
					}
					p.coreKB = next
					p.returnRequested = true
					events = append(events, input.Event{Key: "s"})
					break
				}
			}
			if p.returnRequested {
				continue
			}
			p.kbRows = bootReportToRows(rep)
			if err := p.keyboard.state.writeRows(ctx, combineRows(p.kbRows, p.padRows)); err != nil {
				b.inputFault = err
				return nil, err
			}
			p.coreKB = next
			continue
		}

		// ProgramReady menu keyboard handling.
		edgeCount, err := p.menuKB.FeedInto(rep[:], edgeBuf[:])
		if errors.Is(err, usb.ErrKeyboardRollover) {
			continue
		}
		if err != nil {
			b.inputFault = err
			return nil, err
		}
		if !p.kbArmed {
			p.kbArmed = rep == ([8]byte{})
			continue
		}
		for _, edge := range edgeBuf[:edgeCount] {
			if edge.Pressed && edge.Usage == 0x13 && edge.Modifiers&0x11 != 0 && edge.Modifiers&0x44 != 0 {
				p.togglePairing()
				continue
			}
			if ev, ok := input.USBKeyPress(edge); ok {
				if p.cfg.App != nil {
					p.cfg.App.NoteInput(tenfoot.InputKeyboard, 0)
				}
				events = append(events, ev)
			}
		}
		if len(events) > maxProgramMenuEvents {
			b.inputFault = errors.New("native input: menu event queue exhausted")
			return nil, b.inputFault
		}
	}
	// With the queue drained the live state is the newest snapshot. Startup
	// arms here: devices reporting all-up change nothing, so queue nothing.
	if !p.kbArmed && kbDrained {
		p.kbArmed = p.source.KeyboardNeutral()
	}

	padDrained := false
	usedQueuedPads := false
	pollNow := time.Now()
	var liveBuf [usbhub.MaxGamepadDevices]usbhub.GamepadPadState
	for n := 0; n < 8; n++ {
		if v.State == ProgramRunning && p.returnRequested {
			break
		}
		if err := ctx.Err(); err != nil {
			b.inputFault = err
			return nil, err
		}
		upd, ok := p.source.NextGamepadUpdate()
		if !ok {
			padDrained = true
			break
		}
		if v.State == ProgramRunning {
			if p.cfg.App != nil && upd.PadCount > 0 {
				usedQueuedPads = true
				p.processPadStates(upd.Pads[:upd.PadCount], pollNow)
			}
			if !p.padArmed {
				p.padArmed = input.GamepadNeutral(upd.State) && !upd.HomePressed && upd.Ports == ([usbhub.MaxPlayerPorts]uint8{})
				p.prevHome = upd.HomePressed
				continue
			}
			if upd.HomePressed && !p.prevHome {
				if err := p.neutralizeCore(ctx); err != nil {
					b.inputFault = err
					return nil, err
				}
				p.prevHome = true
				p.returnRequested = true
				events = append(events, input.Event{Key: "s"})
				continue
			}
			p.prevHome = upd.HomePressed
			p.rawCtrlPorts = upd.Ports
			states := upd.Pads[:upd.PadCount]
			if upd.PadCount == 0 && p.cfg.App != nil {
				states = p.livePadStatesInto(liveBuf[:])
			}
			if p.nativeControllers {
				if err := p.writeControllerPorts(ctx, p.effectiveControllerPortsFromStates(p.rawCtrlPorts, states)); err != nil {
					b.inputFault = err
					return nil, err
				}
				continue
			}
			p.padRows = p.effectiveCoreRowsFromStates(upd.State, states)
			if err := p.keyboard.state.writeRows(ctx, combineRows(p.kbRows, p.padRows)); err != nil {
				b.inputFault = err
				return nil, err
			}
			continue
		}

		// ProgramReady menu gamepad handling.
		if p.cfg.App != nil && upd.PadCount > 0 {
			usedQueuedPads = true
			p.processPadStates(upd.Pads[:upd.PadCount], pollNow)
		}
		if !p.padArmed {
			p.padArmed = input.GamepadNeutral(upd.State) && !upd.HomePressed && upd.Ports == ([usbhub.MaxPlayerPorts]uint8{})
			p.prevHome = upd.HomePressed
			p.menuPadPressed = [10]bool{}
			continue
		}
		if upd.HomePressed && !p.prevHome {
			events = append(events, input.Event{Key: "o"})
		}
		p.prevHome = upd.HomePressed
		if p.cfg.App != nil && upd.PadCount > 0 {
			events, p.menuPadPressed = appendPadMenuEdges(events, p.menuPadPressed, p.menuControlsFromPadStates(upd.Pads[:upd.PadCount]))
			if len(events) > maxProgramMenuEvents {
				b.inputFault = errors.New("native input: menu event queue exhausted")
				return nil, b.inputFault
			}
			continue
		}
		if p.cfg.App != nil && p.padCount > 0 {
			continue
		}
		nextPressed := [10]bool{}
		dirs := input.GamepadDirections(upd.State)
		copy(nextPressed[:4], dirs[:])
		nextPressed[4] = upd.State.Buttons&(uint32(1)<<(p.cfg.MenuButtons.Confirm-1)) != 0
		nextPressed[5] = upd.State.Buttons&(uint32(1)<<(p.cfg.MenuButtons.Back-1)) != 0
		events, p.menuPadPressed = appendPadMenuEdges(events, p.menuPadPressed, nextPressed)
		if len(events) > maxProgramMenuEvents {
			b.inputFault = errors.New("native input: menu event queue exhausted")
			return nil, b.inputFault
		}
	}
	if !p.padArmed && padDrained {
		p.padArmed = p.source.GamepadNeutral()
	}
	if p.cfg.App != nil && !usedQueuedPads && (p.padCount > 0 || len(p.pads) > 0) {
		live := p.livePadStatesInto(liveBuf[:])
		p.processPadStates(live, pollNow)
		if v.State == ProgramRunning && p.padArmed && !p.returnRequested && len(live) > 0 {
			if p.nativeControllers {
				if err := p.writeControllerPorts(ctx, p.effectiveControllerPortsFromStates(p.rawCtrlPorts, live)); err != nil {
					b.inputFault = err
					return nil, err
				}
			} else if p.hasActivePadProfile(live) {
				nextRows := p.effectiveCoreRowsFromStates(usb.GamepadState{Hat: -1}, live)
				if nextRows != p.padRows {
					p.padRows = nextRows
					if err := p.keyboard.state.writeRows(ctx, combineRows(p.kbRows, p.padRows)); err != nil {
						b.inputFault = err
						return nil, err
					}
				}
			}
		}
		if v.State == ProgramReady && p.padArmed && len(live) > 0 {
			events, p.menuPadPressed = appendPadMenuEdges(events, p.menuPadPressed, p.menuControlsFromPadStates(live))
			if len(events) > maxProgramMenuEvents {
				b.inputFault = errors.New("native input: menu event queue exhausted")
				return nil, b.inputFault
			}
		}
	}

	mouseDrained := false
	for n := 0; n < 16; n++ {
		if v.State == ProgramRunning && p.returnRequested {
			break
		}
		if err := ctx.Err(); err != nil {
			b.inputFault = err
			return nil, err
		}
		upd, ok := p.source.NextMouseUpdate()
		if !ok {
			mouseDrained = true
			break
		}
		if v.State == ProgramRunning {
			if !p.mouseArmed {
				p.mouseArmed = upd.Buttons == 0
				continue
			}
			if p.nativeMouse {
				if upd.DX == 0 && upd.DY == 0 && upd.Buttons == p.mouseButtons {
					continue
				}
				if err := b.service.MouseRelative(ctx, p.session, upd.DX, upd.DY, upd.Buttons); err != nil {
					b.inputFault = err
					return nil, err
				}
				p.mouseButtons = upd.Buttons
			}
			continue
		}
		if !p.mouseArmed {
			p.mouseArmed = upd.Buttons == 0
		}
	}
	if !p.mouseArmed && mouseDrained {
		p.mouseArmed = p.source.MouseNeutral()
	}

	// Bond writes run between frames from Next, never from serviceMenuInput
	// while the menu copies scanout rows; the source backs off failed writes.
	if v.State == ProgramReady && !buffer {
		if saved, err := p.source.SaveBondsIfDirty(ctx); (saved || err != nil) && p.cfg.OnBondsSaved != nil {
			p.cfg.OnBondsSaved(err)
		}
		p.refreshBluetoothStatus(false)
	}

	if buffer {
		p.pending = events
		return nil, nil
	}
	return events, nil
}

func (p *ProgramMultiInputPlatform) togglePairing() {
	if p.source.PairingActive() {
		err := p.source.StopPairing()
		if p.cfg.OnPairingEvent != nil {
			p.cfg.OnPairingEvent(false, err)
		}
		return
	}
	err := p.source.StartPairing(p.cfg.PairingDuration)
	if p.cfg.OnPairingEvent != nil {
		p.cfg.OnPairingEvent(err == nil && p.source.PairingActive(), err)
	}
}

func (p *ProgramMultiInputPlatform) park(ctx context.Context) error {
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
		b.session, b.play = ProgramSession{}, programPlay{}
	}
	return err
}
