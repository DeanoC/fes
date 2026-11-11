// SPDX-License-Identifier: GPL-3.0-or-later
package dwc2

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/input"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

func TestDisconnectedStartupStopsPreparedLeaseWithoutEnumerationOrRetry(t *testing.T) {
	hw := newController()
	hw.regs[hprt] = 0 // no connected device; startup may assert only port power
	firmware := &ownedFirmware{t: t, controller: hw}
	client, err := usbcontrol.New(context.Background(), firmware, 17)
	if err != nil {
		t.Fatal(err)
	}
	var partial *Host
	start := hw.tick
	source, err := usbcontrol.Open(context.Background(), client, func() (input.USBReports, error) {
		var e error
		partial, e = Open(hw, Config{PHYClockHz: 60000000})
		return partial, e
	})
	var failure *usbcontrol.StartupFailure
	if source == nil || partial == nil || !errors.As(err, &failure) || !strings.Contains(err.Error(), "address=0x440 value=0x1000 mask=0x1 want=0x1") {
		t.Fatal(err)
	}
	if firmware.confirms != 0 || firmware.stops != 1 || len(hw.starts) != 0 || len(hw.requests) != 0 || hw.tick.Sub(start) > time.Second {
		t.Fatal("empty port enumerated or extended its deadline", firmware, hw.tick.Sub(start))
	}
	if stopped, known := failure.StopReceipt(); !known || stopped.Active || stopped.Confirmed || stopped.Generation != 2 {
		t.Fatal("modeled stop outcome absent", failure, stopped)
	}
	accesses := hw.accesses
	_, _ = source.PollKeyboard()
	_, _ = partial.PollKeyboard()
	_ = source.Close(context.Background())
	if hw.accesses != accesses || firmware.stops != 1 {
		t.Fatal("failed startup retried hardware")
	}
}
