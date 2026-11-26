//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package de25nano

import (
	"sync"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/rtc"
)

var (
	wallClockOnce sync.Once
	wallClock     *rtc.Clock
)

// WallClock returns DE25-Nano's monotonic-anchored UTC clock. It starts
// unsynced (DE25-Nano has no on-chip PL031 RTC) until SyncWallTime or SNTP
// establishes a UTC epoch; runtime/goos.Nanotime remains strictly monotonic.
func WallClock() *rtc.Clock {
	wallClockOnce.Do(func() {
		wallClock = rtc.NewUnsyncedClock(timerNanos)
	})
	return wallClock
}

// WallTime returns the current synchronized UTC wall-clock time, or false if
// no wall-clock epoch has been synchronized yet.
func WallTime() (time.Time, bool) {
	return WallClock().WallTime()
}

// SyncWallTime anchors DE25-Nano's UTC wall clock to wall using CNTPCT_EL0 as
// the monotonic timebase.
func SyncWallTime(wall time.Time) {
	WallClock().Sync(wall)
}
