// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"context"
	"errors"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

type programInputPolling interface {
	PausePolling(context.Context) error
	ResumePolling(context.Context) error
}

// All calls hold backend.operation. Frontend polling cannot enter while a
// transition drains the source, and the source retains its own driver lock.
func (b *ProgramBackend) withInputPaused(ctx context.Context, perform func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.inputPolling == nil {
		return perform()
	}
	if b.inputBoundary == ^uint64(0) {
		return errors.New("native input: transition boundary exhausted")
	}
	b.inputBoundary++
	if err := waitProgramInput(ctx, b.inputPolling.PausePolling); err != nil {
		b.inputFault = err
		return errors.Join(err, b.parkInputFault())
	}
	err := perform()
	state := b.service.Status().State
	if state != ProgramReady && state != ProgramRunning {
		// An uncertain core stays contained; USB stays drained and reserved.
		b.inputFault = errors.Join(ErrProgramRetired, err)
		return b.inputFault
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if resumeErr := waitProgramInput(cleanup, b.inputPolling.ResumePolling); resumeErr != nil {
		b.inputFault = resumeErr
		return errors.Join(err, resumeErr, b.parkInputFault())
	}
	return err
}
func waitProgramInput(parent context.Context, call func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := call(ctx)
		if !errors.Is(err, usbcontrol.ErrBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}
func (b *ProgramBackend) parkInputFault() error {
	if b.service.Status().State != ProgramRunning {
		return nil
	}
	if b.session.Generation() == 0 {
		return ErrProgramSession
	}
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := b.service.Park(cleanup, b.session)
	if err == nil {
		b.session, b.play = ProgramSession{}, programPlay{}
	}
	return err
}
