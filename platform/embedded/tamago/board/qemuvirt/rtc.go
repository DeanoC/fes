//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package qemuvirt

import (
	"sync"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/rtc"
)

// QEMU virt ARM PrimeCell PL031 RTC MMIO base (mapped as device memory by cpu.Init).
const PL031Base uint64 = 0x09010000

var (
	rtcOnce  sync.Once
	rtcDev   *rtc.PL031
	rtcClock *rtc.Clock
	rtcErr   error
)

func initRTC() {
	rtcOnce.Do(func() {
		rtcDev, rtcErr = rtc.OpenMMIO(PL031Base)
		if rtcErr == nil {
			rtcClock = rtc.NewClock(rtcDev.Now(), timerNanos)
		}
	})
}

// RTC returns the QEMU virt ARM PrimeCell PL031 real-time clock.
func RTC() (*rtc.PL031, error) {
	initRTC()
	return rtcDev, rtcErr
}

// WallTime returns the current UTC wall-clock time anchored to the PL031 RTC
// epoch and advanced monotonically by CNTPCT_EL0. runtime/goos.Nanotime stays
// strictly monotonic.
func WallTime() (time.Time, bool) {
	initRTC()
	if rtcClock == nil {
		return time.Time{}, false
	}
	return rtcClock.Now(), true
}
