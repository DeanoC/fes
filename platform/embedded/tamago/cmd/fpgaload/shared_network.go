//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"errors"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
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

func startProgramNetwork() error {
	if !de25nano.NetworkEnabled() {
		return nil
	}
	return programService.StartNetwork(context.Background(), programNetworkBootOwner, func(ctx, lifetime context.Context, owner uint64) (*network.Controller, error) {
		_, control, err := de25nano.OpenNetworkWithControl(ctx, lifetime, owner)
		return control, err
	})
}
