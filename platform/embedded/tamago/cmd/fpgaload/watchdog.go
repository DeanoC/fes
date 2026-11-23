//go:build tamago && arm64 && de25nano && de25tenfootserial

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"fmt"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/wdt"
)

// Opt-in build-time linker flags (-X main.expectedWatchdogTimeout=<seconds>
// -X main.expectedWatchdogClockHz=<hz>). When both are empty (the default),
// watchdog hardware is never probed or armed.
var (
	expectedWatchdogTimeout string
	expectedWatchdogClockHz string
)

func startProgramWatchdog(ctx context.Context) (func(), error) {
	spec, enabled, err := wdt.ParseKeepaliveSpec(expectedWatchdogTimeout, expectedWatchdogClockHz)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return func() {}, nil
	}
	dev, _, err := de25nano.OpenWatchdog(0, spec.ClockHz)
	if err != nil {
		return nil, err
	}
	sup, err := wdt.NewSupervisor(dev, wdt.SupervisorConfig{
		Timeout: spec.Timeout,
		Mode:    wdt.ModeReset,
		OnError: func(err error) {
			fmt.Printf("FES_NATIVE_WATCHDOG_ERROR err=%v\n", err)
		},
	})
	if err != nil {
		return nil, err
	}
	actual, err := sup.Start(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("FES_NATIVE_WATCHDOG_ARMED timeout=%s interval=%s clock_hz=%d\n", actual, sup.Interval(), spec.ClockHz)
	return sup.Stop, nil
}
