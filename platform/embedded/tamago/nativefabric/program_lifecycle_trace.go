// SPDX-License-Identifier: GPL-3.0-or-later

package nativefabric

import (
	"context"
	"fmt"
	"slices"

	"github.com/DeanoC/FogCast/ui/tenfoot"
)

// EnableLifecycleTrace is called before the native App starts. Diagnostic output
// is observational only and grants no device or storage access.
func (p *ProgramSerialPlatform) EnableLifecycleTrace() {
	p.lifecycleTrace = true
	p.service.state.launchTrace = func(status ProgramServiceStatus, stage string, err error) {
		_ = p.emit(context.Background(), fmt.Appendf(nil,
			"FES_NATIVE_LAUNCH_STAGE stage=%s state=%s owner=%x generation=%d failed=%t\n",
			stage, status.State, status.Owner, status.Generation, err != nil))
	}
}

func lifecycleKeyboardObserver(base tenfoot.Platform) func(context.Context, ProgramServiceStatus, []byte, bool) {
	if serial, ok := base.(*ProgramSerialPlatform); ok && serial.lifecycleTrace {
		return serial.traceLifecycleKeyboard
	}
	return nil
}

type lifecycleKeyboardTrace struct {
	state                                       ProgramServiceState
	generation                                  uint64
	armed, control, alt, escape, quit, rollover bool
}

func (p *ProgramSerialPlatform) traceLifecycleKeyboard(ctx context.Context, status ProgramServiceStatus, report []byte, armed bool) {
	const maxTransitions = 256
	if len(report) != 8 || p.traceReports >= maxTransitions {
		return
	}
	keys := report[2:]
	next := lifecycleKeyboardTrace{
		state:      status.State,
		generation: status.Generation,
		armed:      armed,
		control:    report[0]&0x11 != 0,
		alt:        report[0]&0x44 != 0,
		escape:     slices.Contains(keys, 0x29),
		quit:       status.State == ProgramReady && slices.Contains(keys, 0x14),
		rollover:   slices.Contains(keys, 0x01),
	}
	if next == p.keyboardTrace {
		return
	}
	p.keyboardTrace = next
	p.traceReports++
	_ = p.emit(ctx, fmt.Appendf(nil,
		"FES_NATIVE_KEYBOARD_SHORTCUT state=%s owner=%x generation=%d armed=%t control=%t alt=%t escape=%t quit=%t rollover=%t\n",
		status.State, status.Owner, status.Generation, next.armed, next.control, next.alt, next.escape, next.quit, next.rollover))
}
