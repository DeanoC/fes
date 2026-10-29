//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"context"
	"fmt"
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
func OpenNetwork(ctx context.Context) (*network.Interface, error) {
	if !profileNetworkEnabled {
		return nil, fmt.Errorf("de25-nano: Ethernet explicitly disabled in firmware profile")
	}
	if err := InitDevices(); err != nil {
		return nil, err
	}
	return network.OpenDevice(ctx, profileNetworkAddress, profileNetworkGateway, func() (network.Device, error) {
		return xgmac.Open(macRegisters{}, networkMemory{}, xgmac.Config{MAC: profileNetworkMAC, PHYAddress: profileNetworkPHY, PHYID: profileNetworkPHYID, CSRClockHz: profileNetworkClock, RXBufferShift: profileNetworkRXShift})
	})
}
