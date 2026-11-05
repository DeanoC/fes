//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

// The backend serializes report polls and transition callbacks. These local
// counters describe delivered reports; they grant no device or core authority.
type programUSBMetrics struct{ reports uint64 }

var sharedInputMetrics programUSBMetrics

func (m *programUSBMetrics) count(report []byte, err error) ([]byte, error) {
	if err == nil && report != nil {
		m.reports++
	}
	return report, err
}
func (m *programUSBMetrics) boundary(ctx context.Context, source input.PollingControl, pause bool) error {
	var err error
	phase := "resume"
	if pause {
		phase = "pause"
		err = source.PausePolling(ctx)
	} else {
		err = source.ResumePolling(ctx)
	}
	if err != nil {
		return err
	}
	r, valid := programUSBClient.LastReceipt()
	v := programService.Status()
	if !valid || !r.Active || !r.Confirmed || r.Generation != 2 || r.Owner != v.Owner ||
		(v.State != nativefabric.ProgramReady && v.State != nativefabric.ProgramRunning) {
		return errors.New("shared USB boundary lost owned input or native session")
	}
	fmt.Printf("FES_NATIVE_SHARED_USB INPUT phase=%s state=%s native_generation=%d owner=%x generation=%d reports=%d\n", phase, v.State, v.Generation, r.Owner, r.Generation, m.reports)
	return nil
}

type sharedProgramKeyboard struct {
	source  *usbcontrol.Source
	metrics *programUSBMetrics
}

func (s *sharedProgramKeyboard) PollKeyboard() ([]byte, error) {
	r, e := s.source.PollKeyboard()
	return s.metrics.count(r, e)
}
func (s *sharedProgramKeyboard) PausePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.source, true)
}
func (s *sharedProgramKeyboard) ResumePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.source, false)
}

type sharedProgramGamepad struct {
	source  *usbcontrol.GamepadSource
	metrics *programUSBMetrics
}

func (s *sharedProgramGamepad) PollGamepad() ([]byte, error) {
	r, e := s.source.PollGamepad()
	return s.metrics.count(r, e)
}
func (s *sharedProgramGamepad) PausePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.source, true)
}
func (s *sharedProgramGamepad) ResumePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.source, false)
}
