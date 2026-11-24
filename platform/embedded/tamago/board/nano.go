//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package board

import (
	"context"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/psci"
	"github.com/DeanoC/fes/platform/embedded/tamago/rtc"
	"github.com/usbarmory/tamago/dma"
)

const Name = "de25-nano"

func ExceptionLevel() uint64            { return de25nano.ExceptionLevel() }
func CounterFrequency() uint64          { return de25nano.CounterFrequency() }
func GIC() *gic.Controller              { return de25nano.GIC() }
func PSCI() *psci.Client                { return de25nano.PSCI() }
func PowerOff() error                   { return de25nano.PowerOff() }
func Reset() error                      { return de25nano.Reset() }
func EntropySource() psci.EntropySource { return de25nano.EntropySource() }
func RTC() (*rtc.PL031, error)          { return nil, rtc.ErrNotFound }
func WallTime() (time.Time, bool)       { return time.Time{}, false }

// PIO storage needs no DMA. Devices map only their explicit profile-owned DMA/display windows.
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
func OpenNetworkDHCP(ctx context.Context) (Network, error) {
	return nil, fmt.Errorf("de25-nano: DHCPv4 requires a DHCP-enabled firmware profile")
}
func OpenDisplay(width, height int, _ *dma.Region) (Display, error) {
	return de25nano.OpenDisplay(width, height)
}
