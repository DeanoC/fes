//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/DeanoC/fes/platform/embedded/tamago/agentapi"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/maccontrol"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"github.com/DeanoC/fes/platform/embedded/tamago/rtc"
	"github.com/DeanoC/fes/platform/embedded/tamago/xgmac"
)

// Opt-in network settings from the selected board build.
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
	adapter := de25nano.NetworkAdapter()
	if adapter != "" && provision == nil {
		fmt.Println("FES_NATIVE_AGENT_UNAVAILABLE reason=pairing-required")
		return func() {}, nil
	}
	var bootID string
	if provision != nil {
		if id := de25nano.BootID(); id != ([16]byte{}) {
			bootID = uuid.UUID(id).String()
		}
		if bootID == "" {
			fmt.Println("FES_NATIVE_AGENT_UNAVAILABLE reason=boot-entropy-unavailable")
			if adapter != "" {
				return func() {}, nil
			}
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
	if adapter != "" {
		// The first MENU frame has completed. A contained query cannot start
		// DMA. Only an explicit unsupported reply permits local-only use.
		available, err := maccontrol.Available(context.Background(), de25nano.FabricCPUTransport{}, programNetworkBootOwner)
		if err != nil {
			return func() {}, err
		}
		if !available {
			fmt.Println("FES_NATIVE_AGENT_UNAVAILABLE reason=mac-supplier-unavailable")
			return func() {}, nil
		}
		netOpts.TargetID = provision.TargetID
		fmt.Println("FES_NATIVE_NETWORK_ADAPTER", adapter)
	}
	var iface *network.Interface
	var controller *network.Controller
	var networkLifetime context.Context
	err = programService.StartNetworkWithTimeout(context.Background(), de25nano.NetworkStartupTimeout(spec.DHCP), programNetworkBootOwner, func(ctx, lifetime context.Context, owner uint64) (*network.Controller, error) {
		networkLifetime = lifetime

		var err error
		if spec.DHCP {
			iface, controller, err = de25nano.OpenNetworkWithControlDHCPOptions(ctx, lifetime, owner, network.DHCPConfig{
				Hostname: spec.DHCPHostname,
			}, netOpts)
		} else {
			iface, controller, err = de25nano.OpenNetworkWithControlOptions(ctx, lifetime, owner, netOpts)
		}
		return controller, err
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

	// These services bind one leased IP. Replacements only occur behind the
	// original MAC controller, after all old sockets/taps have been retired.
	var currentTarget *agentapi.Service
	var servicesMu sync.Mutex
	var stopServices func()
	withdraw := func() {
		servicesMu.Lock()
		defer servicesMu.Unlock()
		if stopServices != nil {
			stopServices()
			stopServices = nil
		}
	}
	publish := func(endpoint *network.Interface) error {
		servicesMu.Lock()
		defer servicesMu.Unlock()
		if stopServices != nil {
			return errors.New("native network endpoint still serving")
		}
		var cleanup []func()
		stop := func() {
			for n := len(cleanup) - 1; n >= 0; n-- {
				cleanup[n]()
			}
		}
		if spec.DHCP && endpoint != nil {
			if lease, ok := endpoint.Lease(); ok {
				fmt.Printf("FES_NATIVE_NETWORK_DHCP address=%s gateway=%s server=%s lease_s=%d\n",
					lease.Address, lease.Gateway, lease.Server, int64(lease.LeaseDuration/time.Second))
			}
			ren, err := endpoint.NewLeaseRenewer(network.RenewConfig{
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
				stop()
				return err
			}
			renCtx, renCancel := context.WithCancel(networkLifetime)
			if err := ren.Start(renCtx); err != nil {
				renCancel()
				stop()
				return err
			}
			activeRenewer.Store(ren)
			cleanup = append(cleanup, func() {
				activeRenewer.Store(nil)
				renCancel()
				ren.Stop()
			})
		}

		if provision != nil {
			var next *agentapi.Service
			var err error
			if currentTarget == nil {
				next, err = programService.NewTargetService(endpoint, *provision, programNetworkBootOwner, bootID, func(err error) {
					fmt.Printf("FES_NATIVE_AGENT_ERROR err=%q\n", err)
				})
			} else {
				next, err = currentTarget.OnNewEndpoint(endpoint)
			}
			if err == nil {
				err = next.Start(networkLifetime)
			}
			if err != nil {
				stop()
				return err
			}
			cleanup = append(cleanup, func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := next.Stop(ctx); err != nil {
					fmt.Printf("FES_NATIVE_AGENT_STOP_ERROR err=%q\n", err)
				}
			})
			currentTarget = next
			fmt.Printf("FES_NATIVE_AGENT_READY addr=%s target_id=%s\n", next.Addr(), provision.TargetID)
		}
		stopServices = stop
		return nil
	}
	if err = publish(iface); err != nil {
		runCleanups()
		return func() {}, err
	}
	cleanups = append(cleanups, withdraw)
	if spec.DHCP && adapter != "" && provision != nil {
		err = iface.RecoverDHCPAfterPause(controller, networkLifetime, network.DHCPConfig{Hostname: spec.DHCPHostname},
			withdraw, publish, func(err error) { fmt.Printf("FES_NATIVE_AGENT_UNAVAILABLE reason=dhcp-reacquisition err=%q\n", err) })
		if err != nil {
			runCleanups()
			return func() {}, err
		}
	}
	return runCleanups, nil
}
