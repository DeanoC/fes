//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package board

import (
	"context"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/usbarmory/tamago/dma"
)

const Name = "de25-nano"

func ExceptionLevel() uint64   { return de25nano.ExceptionLevel() }
func CounterFrequency() uint64 { return de25nano.CounterFrequency() }

// PIO storage needs no DMA arena. Never install the QEMU arena on the Nano.
func InitDevices() (*dma.Region, error)  { return nil, nil }
func OpenStorage() (block.Device, error) { return de25nano.OpenStorage() }
func OpenKeyboard() (*input.USBKeyboard, error) {
	return de25nano.OpenKeyboard()
}
func OpenNetwork(context.Context) (Network, error) {
	return nil, fmt.Errorf("de25-nano: Ethernet MAC/PHY adapter unavailable")
}
func OpenDisplay(int, int, *dma.Region) (Display, error) {
	return nil, fmt.Errorf("de25-nano: FPGA display/ownership adapter unavailable")
}
