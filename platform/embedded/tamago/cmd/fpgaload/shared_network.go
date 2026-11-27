//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/rtc"
	"github.com/DeanoC/fes/platform/embedded/tamago/xgmac"
)

// Opt-in build-time linker flags (-X main.expectedNetworkDHCP=1, etc.).
// When empty (the default), native Ethernet preserves static 1000BASE-T FD
// fail-closed addressing with no DHCP lease renewal or SNTP clock synchronization.
//
// Dormant: build.py refuses every flag that sets these, because fpgaload builds
// only from nano/sd-profile.json (network: null) and closed-bridge SD ownership
// excludes Ethernet. No image links them today, so the DHCP, renewal, 100M,
// link-flap and SNTP code below never runs; a hand-linked image still fails
// closed in prepareProgramNetwork while NetworkEnabled() is false. See network/README.md.
var (
	expectedNetworkDHCP            string
	expectedNetworkDHCPHostname    string
	expectedNetworkSNTPServer      string
	expectedNetworkSNTPPort        string
	expectedNetworkAllow100M       string
	expectedNetworkRecoverLinkFlap string
)

// This owner is captured under the original accepted cold context, before USB,
// SD or menu ownership. A source/catalogue cannot select or replace it.
var programNetworkBootOwner uint64
var programWallClock *rtc.Clock

func parseProgramNetworkSpec() (network.StartupSpec, error) {
	return network.ParseStartupFlags(network.StartupFlags{
		DHCP:            expectedNetworkDHCP,
		DHCPHostname:    expectedNetworkDHCPHostname,
		SNTPServer:      expectedNetworkSNTPServer,
		SNTPPort:        expectedNetworkSNTPPort,
		Allow100Full:    expectedNetworkAllow100M,
		RecoverLinkLoss: expectedNetworkRecoverLinkFlap,
	})
}

func prepareProgramNetwork(v *nativefabric.HPSContextVerifier) error {
	spec, err := parseProgramNetworkSpec()
	if err != nil {
		return err
	}
	if !de25nano.NetworkEnabled() {
		if spec.DHCP || spec.SNTPServer != "" || spec.Allow100Full || spec.RecoverLinkLoss {
			return errors.New("native Ethernet opt-in options require a network-enabled profile")
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

func startProgramNetwork(g *gic.Controller, provision *agentapi.Provisioning) (func(), error) {
	spec, err := parseProgramNetworkSpec()
	if err != nil {
		return func() {}, err
	}
	if !de25nano.NetworkEnabled() {
		if provision != nil {
			fmt.Println("FES_NATIVE_AGENT_UNAVAILABLE reason=ethernet-disabled")
		}
		return func() {}, nil
	}
	// CNTPCT may repeat after cold reset. The physical target uses one fresh
	// UUID for this boot, independent of the stable SD-provisioned target ID.
	var bootID string
	if provision != nil {
		if id := de25nano.BootID(); id != ([16]byte{}) {
			bootID = uuid.UUID(id).String()
		}
		if bootID == "" {
			fmt.Println("FES_NATIVE_AGENT_UNAVAILABLE reason=boot-entropy-unavailable")
			provision = nil
		}
	}
	var activeRenewer atomic.Pointer[network.LeaseRenewer]
	netOpts := de25nano.NetworkOptions{
		Allow100Full:    spec.Allow100Full,
		RecoverLinkLoss: spec.RecoverLinkLoss,
		ReceiveMDNS:     provision != nil,
		OnLinkChange: func(up bool, speed xgmac.LinkSpeed) {
			fmt.Printf("FES_NATIVE_NETWORK_LINK up=%t speed=%d\n", up, speed)
			if up {
				if ren := activeRenewer.Load(); ren != nil {
					ren.NotifyLinkUp()
				}
			}
		},
	}
	var iface *network.Interface
	var networkLifetime context.Context
	err = programService.StartNetworkWithTimeout(context.Background(), de25nano.NetworkStartupTimeout(spec.DHCP), programNetworkBootOwner, func(ctx, lifetime context.Context, owner uint64) (*network.Controller, error) {
		networkLifetime = lifetime
		var control *network.Controller
		var err error
		if spec.DHCP {
			iface, control, err = de25nano.OpenNetworkWithControlDHCPOptions(ctx, lifetime, owner, network.DHCPConfig{
				Hostname: spec.DHCPHostname,
			}, netOpts)
		} else {
			iface, control, err = de25nano.OpenNetworkWithControlOptions(ctx, lifetime, owner, netOpts)
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
		activeRenewer.Store(ren)
		cleanups = append(cleanups, func() {
			activeRenewer.Store(nil)
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

	if provision != nil {
		target, err := programService.NewTargetService(iface, *provision, programNetworkBootOwner, bootID, func(err error) {
			fmt.Printf("FES_NATIVE_AGENT_ERROR err=%q\n", err)
		})
		if err == nil {
			err = target.Start(networkLifetime)
		}
		if err != nil {
			runCleanups()
			return func() {}, err
		}
		cleanups = append(cleanups, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := target.Stop(ctx); err != nil {
				fmt.Printf("FES_NATIVE_AGENT_STOP_ERROR err=%q\n", err)
			}
		})
		fmt.Printf("FES_NATIVE_AGENT_READY addr=%s target_id=%s\n", target.Addr(), provision.TargetID)
	}
	return runCleanups, nil
}
