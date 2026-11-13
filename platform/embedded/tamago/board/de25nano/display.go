//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/scanout"
	"sync/atomic"
	"unsafe"
)

// The profile admits a firmware-owned, fixed scanout allocation. Nothing in
// this adapter proves live FPGA identity, vblank, drain or bridge containment.
// Physical reconfiguration remains unavailable while this boot's pool is pinned.
type displayMemory struct{}

func (displayMemory) Size() uint64 { return profileDisplayBytes }
func (displayMemory) Write(offset uint64, src []byte) error {
	if offset > profileDisplayBytes || uint64(len(src)) > profileDisplayBytes-offset {
		return fmt.Errorf("de25-nano: display copy outside boot-owned window")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(profileDisplayBase)+uintptr(offset))), len(src)), src)
	return nil
}
func (displayMemory) Publish() error { memoryBarrier(); return nil }

var displayAttempted atomic.Bool

func OpenDisplay(width, height int) (*scanout.Surface, error) {
	if !profileDisplayEnabled {
		return nil, fmt.Errorf("de25-nano: display explicitly disabled in firmware profile")
	}
	if width != profileDisplayWidth || height != profileDisplayHeight {
		return nil, fmt.Errorf("de25-nano: display geometry differs from firmware profile")
	}
	if !displayAttempted.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("de25-nano: display already attempted; window remains pinned until reboot")
	}
	if err := InitDevices(); err != nil {
		return nil, err
	}
	surface, err := scanout.New(width, height, profileDisplayStride, displayMemory{})
	if err != nil {
		return nil, err
	}
	fmt.Printf("FES_DISPLAY_READY backend=boot-scanout firmware_declared=%s lifetime=boot drain=unavailable\n", profileDisplayArtifact)
	return surface, nil
}
