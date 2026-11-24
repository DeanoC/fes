//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/gic"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
)

// This owner is captured under the original accepted cold context, before USB,
// SD or menu ownership. A source/catalogue cannot select or replace it.
var programNetworkBootOwner uint64

func prepareProgramNetwork(v *nativefabric.HPSContextVerifier) error {
	if !de25nano.NetworkEnabled() {
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
	if !de25nano.NetworkEnabled() {
		return func() {}, nil
	}
	var iface *network.Interface
	err := programService.StartNetwork(context.Background(), programNetworkBootOwner, func(ctx, lifetime context.Context, owner uint64) (*network.Controller, error) {
		var control *network.Controller
		var err error
		iface, control, err = de25nano.OpenNetworkWithControl(ctx, lifetime, owner)
		return control, err
	})
	if err != nil {
		return func() {}, err
	}
	if g != nil && iface != nil {
		if err := iface.EnableIRQ(g); err != nil {
			if !errors.Is(err, gic.ErrUnsupported) {
				return func() {}, err
			}
		} else {
			return func() { iface.DisableIRQ(g) }, nil
		}
	}
	return func() {}, nil
}
