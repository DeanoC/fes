//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/rtc"
)

// Opt-in build-time linker flags (-X main.expectedNetworkDHCP=1, etc.).
// When empty (the default), native Ethernet preserves static profile addressing
// with no DHCP lease renewal or SNTP clock synchronization.
//
// Dormant: build.py refuses every flag that sets these, because fpgaload builds
// only from nano/sd-profile.json (network: null) and closed-bridge SD ownership
// excludes Ethernet. No image links them today, so the DHCP, renewal and SNTP
// code below never runs; a hand-linked image still fails closed in
// prepareProgramNetwork while NetworkEnabled() is false. See network/README.md.
var (
	expectedNetworkDHCP         string
	expectedNetworkDHCPHostname string
	expectedNetworkSNTPServer   string
	expectedNetworkSNTPPort     string
)

// This owner is captured under the original accepted cold context, before USB,
// SD or menu ownership. A source/catalogue cannot select or replace it.
var programNetworkBootOwner uint64
var programWallClock *rtc.Clock

func prepareProgramNetwork(v *nativefabric.HPSContextVerifier) error {
	spec, err := network.ParseStartupSpec(
		expectedNetworkDHCP,
		expectedNetworkDHCPHostname,
		expectedNetworkSNTPServer,
		expectedNetworkSNTPPort,
	)
	if err != nil {
		return err
	}
	if !de25nano.NetworkEnabled() {
		if spec.DHCP || spec.SNTPServer != "" {
			return errors.New("native Ethernet DHCP/SNTP options require a network-enabled profile")
		}
		return nil
	}
	if expectedSelectedPrograms != "1" || expectedTenfootSerial != "1" || expectedCompiledMenuProgram == "" {
		return errors.New("native Ethernet requires the owned paired-menu program")
	}
	before, err := v.Verify(v.Identity())
	if err != nil {
		return err
	}
	if !before.Bridges.NeverEnabled || before.Bridges.Generation != 1 || before.Bridges.Owner == 0 {
		return errors.New("native Ethernet lost initial boot ownership")
	}
	programNetworkBootOwner = before.Bridges.Owner
	return nil
}

func startProgramNetwork(g *gic.Controller) (func(), error) {
	spec, err := network.ParseStartupSpec(
		expectedNetworkDHCP,
		expectedNetworkDHCPHostname,
		expectedNetworkSNTPServer,
		expectedNetworkSNTPPort,
	)
	if err != nil {
		return func() {}, err
	}
	if !de25nano.NetworkEnabled() {
		return func() {}, nil
	}
	startupTimeout := 5 * time.Second
	if spec.DHCP {
		// Give AcquireDHCP its full 10-second retry budget plus XGMAC open margin.
		startupTimeout = 15 * time.Second
	}
	var iface *network.Interface
	err = programService.StartNetworkWithTimeout(context.Background(), startupTimeout, programNetworkBootOwner, func(ctx, lifetime context.Context, owner uint64) (*network.Controller, error) {
		var control *network.Controller
		var err error
		if spec.DHCP {
			iface, control, err = de25nano.OpenNetworkWithControlDHCP(ctx, lifetime, owner, network.DHCPConfig{
				Hostname: spec.DHCPHostname,
			})
		} else {
			iface, control, err = de25nano.OpenNetworkWithControl(ctx, lifetime, owner)
		}
		return control, err
	})
	if err != nil {
		return func() {}, err
	}

	var cleanups []func()
	runCleanups := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	if g != nil && iface != nil {
		if err := iface.EnableIRQ(g); err != nil {
			if !errors.Is(err, gic.ErrUnsupported) {
				runCleanups()
				return func() {}, err
			}
		} else {
			cleanups = append(cleanups, func() { iface.DisableIRQ(g) })
		}
	}

	if spec.DHCP && iface != nil {
		if lease, ok := iface.Lease(); ok {
			fmt.Printf("FES_NATIVE_NETWORK_DHCP address=%s gateway=%s server=%s lease_s=%d\n",
				lease.Address, lease.Gateway, lease.Server, int64(lease.LeaseDuration/time.Second))
		}
		ren, err := iface.NewLeaseRenewer(network.RenewConfig{
			Hostname: spec.DHCPHostname,
			OnRenew: func(l network.DHCPLease) {
				fmt.Printf("FES_NATIVE_NETWORK_DHCP_RENEW address=%s server=%s lease_s=%d\n",
					l.Address, l.Server, int64(l.LeaseDuration/time.Second))
			},
			OnError: func(err error) {
				fmt.Printf("FES_NATIVE_NETWORK_DHCP_ERROR err=%v\n", err)
			},
		})
		if err != nil {
			runCleanups()
			return func() {}, err
		}
		renCtx, renCancel := context.WithCancel(context.Background())
		if err := ren.Start(renCtx); err != nil {
			renCancel()
			runCleanups()
			return func() {}, err
		}
		cleanups = append(cleanups, func() {
			renCancel()
			ren.Stop()
		})
	}

	if spec.SNTPServer != "" && iface != nil {
		// SNTP failures warn rather than aborting startup so an offline NTP
		// server never prevents local native execution.
		sntpCtx, sntpCancel := context.WithTimeout(context.Background(), 5*time.Second)
		res, err := iface.QuerySNTP(sntpCtx, network.SNTPConfig{
			Servers:    []string{spec.SNTPServer},
			ServerPort: spec.SNTPPort,
		})
		sntpCancel()
		if err != nil {
			fmt.Printf("FES_NATIVE_NETWORK_SNTP_WARN server=%s err=%v\n", spec.SNTPServer, err)
		} else {
			programWallClock = rtc.NewClock(res.Time, nil)
			if programClockTarget != nil {
				programClockTarget.SetClock(programWallClock.Now)
			}
			fmt.Printf("FES_NATIVE_NETWORK_SNTP server=%s stratum=%d unix=%d rtt_ms=%d\n",
				res.Server, res.Stratum, res.Time.Unix(), res.RTT.Milliseconds())
		}
	}

	return runCleanups, nil
}
