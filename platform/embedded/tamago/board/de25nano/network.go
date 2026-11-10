//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/xgmac"
)

type macRegisters struct{}

func (macRegisters) Read32(offset uint32) uint32  { return load32(profileNetworkBase + uintptr(offset)) }
func (macRegisters) Write32(offset, value uint32) { store32(profileNetworkBase+uintptr(offset), value) }
func (macRegisters) Now() time.Time               { return time.Now() }

type networkMemory struct{}

func (networkMemory) Base() uint64 { return profileNetworkDMAStart }
func (networkMemory) Size() uint64 { return profileNetworkDMABytes }
func (networkMemory) Read32(offset uint32) uint32 {
	return atomic.LoadUint32((*uint32)(unsafe.Pointer(uintptr(profileNetworkDMAStart) + uintptr(offset))))
}
func (networkMemory) Write32(offset, value uint32) {
	atomic.StoreUint32((*uint32)(unsafe.Pointer(uintptr(profileNetworkDMAStart)+uintptr(offset))), value)
}
func (networkMemory) Read(offset uint32, dst []byte) {
	copy(dst, unsafe.Slice((*byte)(unsafe.Pointer(uintptr(profileNetworkDMAStart)+uintptr(offset))), len(dst)))
}
func (networkMemory) Write(offset uint32, src []byte) {
	copy(unsafe.Slice((*byte)(unsafe.Pointer(uintptr(profileNetworkDMAStart)+uintptr(offset))), len(src)), src)
}
func (networkMemory) Barrier() { memoryBarrier() }

// The generic diagnostic opener has no independently accepted boot owner.
// Native startup must explicitly select OpenNetworkWithControl instead.
func OpenNetwork(ctx context.Context) (*network.Interface, error) {
	if !profileNetworkEnabled {
		return nil, fmt.Errorf("de25-nano: Ethernet explicitly disabled in firmware profile")
	}
	return nil, fmt.Errorf("de25-nano: Ethernet requires trusted boot ownership and controlled startup")
}

var networkLock sync.Mutex
var networkAttempted bool
var ownedNetworkDevice *xgmac.Device // Never reclaim or replace failed DMA resources.
var ownedNetworkInterface *network.Interface
var ownedNetworkController *network.Controller

// OpenNetworkWithControl requires the owner from the trusted boot admission,
// not from package metadata or a previous MAC receipt. Return the controller to
// ProgramService.AttachNetwork before frontend ownership. This hook supplies
// no secure MAC qualification and leaves unqualified profiles disabled.
func OpenNetworkWithControl(ctx context.Context, bootOwner uint64) (*network.Interface, *network.Controller, error) {
	if !profileNetworkEnabled || ctx == nil || bootOwner == 0 {
		return nil, nil, fmt.Errorf("de25-nano: controlled Ethernet profile and trusted boot owner required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	networkLock.Lock()
	defer networkLock.Unlock()
	if networkAttempted {
		return nil, nil, fmt.Errorf("de25-nano: Ethernet initialization already attempted")
	}
	networkAttempted = true
	if err := InitDevices(); err != nil {
		return nil, nil, err
	}
	var control network.DMAControl
	var err error
	ownedNetworkInterface, err = network.OpenDevice(ctx, profileNetworkAddress, profileNetworkGateway, func() (network.Device, error) {
		ownedNetworkDevice, control, err = xgmac.OpenControlled(ctx, FabricCPUTransport{}, bootOwner, macRegisters{}, networkMemory{}, xgmac.Config{MAC: profileNetworkMAC, PHYAddress: profileNetworkPHY, PHYID: profileNetworkPHYID, CSRClockHz: profileNetworkClock, RXBufferShift: profileNetworkRXShift})
		return ownedNetworkDevice, err
	})
	if err != nil {
		return nil, nil, err
	}
	ownedNetworkController, err = ownedNetworkInterface.BindControl(control)
	if err != nil {
		return nil, nil, err
	}
	return ownedNetworkInterface, ownedNetworkController, nil
}
