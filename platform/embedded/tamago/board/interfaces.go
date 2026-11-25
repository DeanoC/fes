//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
// Package board selects the boot hooks and peripherals at build time. The
// de25nano tag excludes the QEMU BSP; FAT configuration cannot switch hardware.
package board

import (
	"context"
	"image"
	"net/netip"

	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

// Architectural ARMv8 Generic Timer PPI interrupt IDs shared by all boards.
const (
	IRQVirtTimer uint32 = 27 // PPI 11 (EL1 virtual timer)
	IRQPhysTimer uint32 = 30 // PPI 14 (EL1 non-secure physical timer)
)

type Display interface{ Present(*image.RGBA) error }
type Network interface {
	Err() error
	Address() string
	Stats() string
	Lease() (network.DHCPLease, bool)
	NewLeaseRenewer(network.RenewConfig) (*network.LeaseRenewer, error)
	RenewLeaseOnce(context.Context, bool, network.RenewConfig) (network.DHCPLease, error)
	ResolveA(context.Context, string, network.DNSConfig) ([]netip.Addr, error)
	QuerySNTP(context.Context, network.SNTPConfig) (network.SNTPResult, error)
	NewSNTPSyncer(network.SNTPSyncConfig) (*network.SNTPSyncer, error)
	EnableIRQ(*gic.Controller) error
	DisableIRQ(*gic.Controller)
	IRQCount() uint64
}
