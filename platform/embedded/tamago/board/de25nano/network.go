//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/xgmac"
)

type macRegisters struct{}

func (macRegisters) Read32(offset uint32) uint32  { return load32(profileNetworkBase + uintptr(offset)) }
func (macRegisters) Write32(offset, value uint32) { store32(profileNetworkBase+uintptr(offset), value) }
func (macRegisters) Now() time.Time               { return time.Now() }

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
var ownedNetworkMemory *xgmac.PoolMemory // Never reclaim or replace failed DMA resources.
var ownedNetworkDevice *xgmac.Device
var ownedNetworkInterface *network.Interface
var ownedNetworkController *network.Controller

// NetworkEnabled reports the explicit compiled selection, not qualification.
// The controlled opener still requires the independent boot owner and complete
// firmware MAC/DMA contract; disabled profiles must never invoke it.
func NetworkEnabled() bool { return profileNetworkEnabled }

// OpenNetworkWithControl requires the owner from the trusted boot admission,
// not from package metadata or a previous MAC receipt. Return the controller to
// ProgramService.AttachNetwork before frontend ownership. This hook supplies
// no secure MAC qualification and leaves unqualified profiles disabled.
// ctx bounds controller startup; lifetime owns the background socket/IP workers.
// Canceling startup after successful return must not terminate those workers.
func OpenNetworkWithControl(ctx, lifetime context.Context, bootOwner uint64) (*network.Interface, *network.Controller, error) {
	return openControlledNetwork(ctx, lifetime, bootOwner, func(factory func() (network.Device, error)) (*network.Interface, error) {
		return network.OpenDevice(lifetime, profileNetworkAddress, profileNetworkGateway, factory)
	})
}

// OpenNetworkWithControlDHCP is the opt-in RFC 2131 DHCPv4 counterpart to
// OpenNetworkWithControl. It enforces the same trusted boot-owner, single-attempt
// DMA ownership, and network.Controller lifecycle contract, bounding XGMAC open
// and DHCPv4 lease acquisition to ctx while background stack workers run on lifetime.
func OpenNetworkWithControlDHCP(ctx, lifetime context.Context, bootOwner uint64, dhcpCfg network.DHCPConfig) (*network.Interface, *network.Controller, error) {
	if err := network.ValidateDHCPConfig(dhcpCfg); err != nil {
		return nil, nil, err
	}
	return openControlledNetwork(ctx, lifetime, bootOwner, func(factory func() (network.Device, error)) (*network.Interface, error) {
		return network.OpenDeviceDHCPWithStartup(ctx, lifetime, factory, dhcpCfg)
	})
}

func openControlledNetwork(
	ctx, lifetime context.Context,
	bootOwner uint64,
	openIface func(factory func() (network.Device, error)) (*network.Interface, error),
) (*network.Interface, *network.Controller, error) {
	if !profileNetworkEnabled || ctx == nil || lifetime == nil || bootOwner == 0 {
		return nil, nil, fmt.Errorf("de25-nano: controlled Ethernet profile and trusted boot owner required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := lifetime.Err(); err != nil {
		return nil, nil, err
	}
	networkLock.Lock()
	defer networkLock.Unlock()
	if networkAttempted {
		return nil, nil, fmt.Errorf("de25-nano: Ethernet initialization already attempted")
	}
	networkAttempted = true
	pool, err := NewUncachedPool(profileNetworkDMAStart, profileNetworkDMABytes)
	if err != nil {
		return nil, nil, err
	}
	mem, err := xgmac.NewPoolMemory(pool)
	if err != nil {
		return nil, nil, err
	}
	ownedNetworkMemory = mem
	irqID, _ := EMACIRQ(profileNetworkBase)
	var control network.DMAControl
	ownedNetworkInterface, err = openIface(func() (network.Device, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ownedNetworkDevice, control, err = xgmac.OpenControlled(ctx, FabricCPUTransport{}, bootOwner, macRegisters{}, mem, xgmac.Config{
			MAC:           profileNetworkMAC,
			PHYAddress:    profileNetworkPHY,
			PHYID:         profileNetworkPHYID,
			CSRClockHz:    profileNetworkClock,
			RXBufferShift: profileNetworkRXShift,
			IRQ:           irqID,
		})
		if err == nil {
			err = ctx.Err()
		}
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
