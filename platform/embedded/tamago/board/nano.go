//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package board

import (
	"context"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/usbarmory/tamago/dma"
)

const Name = "de25-nano"

func ExceptionLevel() uint64   { return de25nano.ExceptionLevel() }
func CounterFrequency() uint64 { return de25nano.CounterFrequency() }

// PIO storage needs no DMA. Ethernet maps only its profile-owned DMA window.
func InitDevices() (*dma.Region, error)  { return nil, de25nano.InitDevices() }
func OpenStorage() (block.Device, error) { return de25nano.OpenStorage() }
func OpenKeyboard() (*input.USBKeyboard, error) {
	return de25nano.OpenKeyboard()
}

type nanoNetwork struct{ *network.Interface }

func (n nanoNetwork) Stats() string { return fmt.Sprintf("%+v", n.Statistics()) }
func OpenNetwork(ctx context.Context) (Network, error) {
	iface, err := de25nano.OpenNetwork(ctx)
	if err != nil {
		return nil, err
	}
	return nanoNetwork{iface}, nil
}
func OpenDisplay(int, int, *dma.Region) (Display, error) {
	return nil, fmt.Errorf("de25-nano: FPGA display/ownership adapter unavailable")
}
