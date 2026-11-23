// SPDX-License-Identifier: GPL-3.0-or-later

package wdt

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	minKeepaliveTimeoutSec = 1
	maxKeepaliveTimeoutSec = 60
	minKeepaliveClockHz    = 1_000_000
	maxKeepaliveClockHz    = 400_000_000
	maxWDTStageCycles      = uint64(1) << 31
)

// KeepaliveSpec holds validated opt-in watchdog keepalive settings parsed from
// build-time linker flags (timeout in seconds and explicit L4 clock in Hz).
type KeepaliveSpec struct {
	Timeout time.Duration
	ClockHz uint64
}

// ParseKeepaliveSpec parses opt-in build-time watchdog strings. When both
// timeoutSec and clockHz are empty, it returns enabled=false with nil error so
// builds without explicit opt-in never probe or arm watchdog hardware.
// Supplying only one parameter or an out-of-range value returns ErrInvalidConfig.
func ParseKeepaliveSpec(timeoutSec, clockHz string) (KeepaliveSpec, bool, error) {
	tStr := strings.TrimSpace(timeoutSec)
	cStr := strings.TrimSpace(clockHz)
	if tStr == "" && cStr == "" {
		return KeepaliveSpec{}, false, nil
	}
	if tStr == "" || cStr == "" {
		return KeepaliveSpec{}, false, fmt.Errorf("%w: both watchdog timeout seconds and clock Hz are required", ErrInvalidConfig)
	}
	secs, err := strconv.ParseInt(tStr, 10, 64)
	if err != nil || secs < minKeepaliveTimeoutSec || secs > maxKeepaliveTimeoutSec {
		return KeepaliveSpec{}, false, fmt.Errorf("%w: watchdog timeout %q must be integer seconds in [%d, %d]",
			ErrInvalidConfig, tStr, minKeepaliveTimeoutSec, maxKeepaliveTimeoutSec)
	}
	hz, err := strconv.ParseUint(cStr, 10, 64)
	if err != nil || hz < minKeepaliveClockHz || hz > maxKeepaliveClockHz {
		return KeepaliveSpec{}, false, fmt.Errorf("%w: watchdog clock %q must be Hz in [%d, %d]",
			ErrInvalidConfig, cStr, minKeepaliveClockHz, maxKeepaliveClockHz)
	}
	if uint64(secs)*hz > maxWDTStageCycles {
		return KeepaliveSpec{}, false, fmt.Errorf("%w: watchdog timeout %ds at %d Hz exceeds 2^31-cycle hardware maximum",
			ErrInvalidConfig, secs, hz)
	}
	return KeepaliveSpec{
		Timeout: time.Duration(secs) * time.Second,
		ClockHz: hz,
	}, true, nil
}
