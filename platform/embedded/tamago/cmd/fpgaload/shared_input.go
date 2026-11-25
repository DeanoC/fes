//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/input/usbhub"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

// The backend serializes report polls and transition callbacks. These local
// counters distinguish successful polling calls from delivered reports. An
// idle keyboard can NAK every request. Neither counter grants authority.
type programUSBMetrics struct{ reports, polls uint64 }

var sharedInputMetrics programUSBMetrics

func (m *programUSBMetrics) count(report []byte, err error) ([]byte, error) {
	if err == nil {
		m.polls++
		if report != nil {
			m.reports++
		}
	}
	return report, err
}
func (m *programUSBMetrics) countInto(n int, err error) (int, error) {
	if err == nil {
		m.polls++
		if n > 0 {
			m.reports++
		}
	}
	return n, err
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
	fmt.Printf("FES_NATIVE_SHARED_USB INPUT phase=%s state=%s native_generation=%d owner=%x generation=%d reports=%d polls=%d\n", phase, v.State, v.Generation, r.Owner, r.Generation, m.reports, m.polls)
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
func (s *sharedProgramKeyboard) PollKeyboardInto(dst []byte) (int, error) {
	n, e := s.source.PollKeyboardInto(dst)
	return s.metrics.countInto(n, e)
}
func (s *sharedProgramKeyboard) PausePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.source, true)
}
func (s *sharedProgramKeyboard) ResumePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.source, false)
}
func (s *sharedProgramKeyboard) Ready() <-chan struct{} {
	return s.source.Ready()
}

type sharedProgramGamepad struct {
	source  *usbcontrol.GamepadSource
	metrics *programUSBMetrics
}

func (s *sharedProgramGamepad) PollGamepad() ([]byte, error) {
	r, e := s.source.PollGamepad()
	return s.metrics.count(r, e)
}
func (s *sharedProgramGamepad) PollGamepadInto(dst []byte) (int, error) {
	n, e := s.source.PollGamepadInto(dst)
	return s.metrics.countInto(n, e)
}
func (s *sharedProgramGamepad) PausePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.source, true)
}
func (s *sharedProgramGamepad) ResumePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.source, false)
}
func (s *sharedProgramGamepad) Ready() <-chan struct{} {
	return s.source.Ready()
}

type sharedProgramMulti struct {
	hub     *usbhub.Hub
	metrics *programUSBMetrics
}

func (s *sharedProgramMulti) Service() error {
	err := s.hub.Service()
	if err == nil {
		s.metrics.polls++
	}
	return err
}
func (s *sharedProgramMulti) NextKeyboardReport() ([8]byte, bool) {
	rep, ok := s.hub.NextKeyboardReport()
	if ok {
		s.metrics.reports++
	}
	return rep, ok
}
func (s *sharedProgramMulti) NextGamepadUpdate() (usbhub.GamepadUpdate, bool) {
	upd, ok := s.hub.NextGamepadUpdate()
	if ok {
		s.metrics.reports++
	}
	return upd, ok
}
func (s *sharedProgramMulti) KeyboardNeutral() bool { return s.hub.KeyboardNeutral() }
func (s *sharedProgramMulti) GamepadNeutral() bool  { return s.hub.GamepadNeutral() }
func (s *sharedProgramMulti) SetGameplayActive(active bool) error {
	return s.hub.SetGameplayActive(active)
}
func (s *sharedProgramMulti) StartPairing(d time.Duration) error { return s.hub.StartPairing(d) }
func (s *sharedProgramMulti) StopPairing() error                 { return s.hub.StopPairing() }
func (s *sharedProgramMulti) PairingActive() bool                { return s.hub.PairingActive() }
func (s *sharedProgramMulti) PairingRemaining() time.Duration    { return s.hub.PairingRemaining() }
func (s *sharedProgramMulti) BluetoothSnapshot() usbhub.BluetoothSnapshot {
	return s.hub.BluetoothSnapshot()
}
func (s *sharedProgramMulti) BluetoothGeneration() uint64 { return s.hub.BluetoothGeneration() }
func (s *sharedProgramMulti) ForgetBluetoothDevice(address string) error {
	return s.hub.ForgetBluetoothDevice(address)
}
func (s *sharedProgramMulti) SaveBondsIfDirty(ctx context.Context) (bool, error) {
	return s.hub.SaveBondsIfDirty(ctx)
}
func (s *sharedProgramMulti) PausePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.hub, true)
}
func (s *sharedProgramMulti) ResumePolling(ctx context.Context) error {
	return s.metrics.boundary(ctx, s.hub, false)
}
func (s *sharedProgramMulti) Ready() <-chan struct{} { return s.hub.Ready() }
