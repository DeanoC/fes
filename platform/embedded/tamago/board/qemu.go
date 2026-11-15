//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package board

import (
	"context"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/qemuvirt"
	"github.com/DeanoC/fes/platform/embedded/tamago/display"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
	"github.com/usbarmory/tamago/dma"
)

const Name = "qemu"

func ExceptionLevel() uint64                                    { return qemuvirt.ExceptionLevel() }
func CounterFrequency() uint64                                  { return qemuvirt.CounterFrequency() }
func InitDevices() (*dma.Region, error)                         { return qemuvirt.InitDevices() }
func OpenStorage() (block.Device, error)                        { return block.ScanVirtIO() }
func OpenDisplay(w, h int, memory *dma.Region) (Display, error) { return display.Open(w, h, memory) }
func OpenKeyboard() (*input.USBKeyboard, error) {
	base, err := qemuvirt.OpenUSBPCI()
	if err != nil {
		return nil, err
	}
	host, err := usb.OpenXHCI(base)
	if err != nil {
		return nil, err
	}
	fmt.Printf("FES_USB_READY device=%+v configuration=%+v\n", host.DeviceInfo(), host.Configuration())
	return input.NewUSBKeyboard(host)
}

type qemuNetwork struct{ *network.Interface }

func (n qemuNetwork) Stats() string { return fmt.Sprintf("%+v", n.Statistics()) }
func OpenNetwork(ctx context.Context) (Network, error) {
	n, err := network.Open(ctx, network.Config{})
	if err != nil {
		return nil, err
	}
	return qemuNetwork{n}, nil
}
