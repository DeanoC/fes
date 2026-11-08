//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

var expectedUSBControl, expectedUSBInput, expectedHPSIO string
var programUSBClient *usbcontrol.Client
var programKeyboardSource *usbcontrol.Source
var programGamepadSource *usbcontrol.GamepadSource
var programGamepadDescriptor []byte

// This runs before SD acquisition. Both the boot owner and complete pin map
// come from the sealed source; the shared service is required explicitly.
func openProgramUSB(v *nativefabric.HPSContextVerifier) error {
	if expectedUSBControl == "" {
		if expectedUSBInput != "" || expectedHPSIO != "" {
			return errors.New("unselected shared USB policy")
		}
		return nil
	}
	if expectedUSBControl != "owned-shared-usb-pio-v1" || expectedTenfootSerial != "1" || expectedSelectedPrograms != "1" {
		return errors.New("shared USB requires selected Tenfoot")
	}
	if expectedUSBInput != "gamepad" && expectedUSBInput != "boot-keyboard" {
		return errors.New("shared USB input protocol required")
	}
	before, err := v.Verify(v.Identity())
	if err != nil {
		return err
	}
	if !before.Bridges.NeverEnabled || before.Bridges.Generation != 1 {
		return errors.New("shared USB lost initial cold scope")
	}
	fmt.Printf("FES_NATIVE_HPS_CONTEXT PASS sha256=%s package_id=%s build_id=%s owner=%x generation=%x programming=unavailable\n", hex.EncodeToString(before.Digest[:]), v.Identity().PackageID, v.Identity().BuildID, before.Bridges.Owner, before.Bridges.Generation)
	observed, err := v.ProbeHPSIO(decoded(expectedHPSIO, 48<<10))
	if err != nil {
		return err
	}
	if observed.Source != before {
		return errors.New("shared USB pins changed boot ownership")
	}
	fmt.Printf("FES_NATIVE_HPS_IO PASS map_sha256=%s count=%d reads=%d owner=%x generation=%x data_held=1 runtime_admission=unavailable\n", hex.EncodeToString(observed.Digest[:]), observed.Count, observed.Count*2, before.Bridges.Owner, before.Bridges.Generation)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	programUSBClient, err = usbcontrol.NewSharedPIO(ctx, de25nano.FabricCPUTransport{}, before.Bridges.Owner)
	if err != nil {
		return err
	}
	if expectedUSBInput == "gamepad" {
		programGamepadSource, programGamepadDescriptor, err = de25nano.OpenGamepadWithControl(ctx, programUSBClient)
	} else {
		programKeyboardSource, err = de25nano.OpenKeyboardWithControl(ctx, programUSBClient)
	}
	if err != nil {
		return err
	}
	receipt, valid := programUSBClient.LastReceipt()
	if !valid || !receipt.Active || !receipt.Confirmed || receipt.Generation != 2 || receipt.Owner != before.Bridges.Owner {
		return errors.New("shared USB readiness receipt absent")
	}
	fmt.Printf("FES_NATIVE_SHARED_USB READY input=%s owner=%x generation=%d descriptor_sha256=%x descriptor_bytes=%d\n", expectedUSBInput, receipt.Owner, receipt.Generation, sha256.Sum256(programGamepadDescriptor), len(programGamepadDescriptor))
	return nil
}
func closeProgramUSB() error {
	if expectedUSBControl == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var err error
	if programGamepadSource != nil {
		err = programGamepadSource.Close(ctx)
	} else if programKeyboardSource != nil {
		err = programKeyboardSource.Close(ctx)
	} else {
		return errors.New("shared USB source absent")
	}
	if err != nil {
		return err
	}
	receipt, valid := programUSBClient.LastReceipt()
	if !valid || receipt.Active || receipt.Confirmed {
		return errors.New("shared USB stop receipt absent")
	}
	fmt.Printf("FES_NATIVE_SHARED_USB STOP owner=%x generation=%d reports=%d polls=%d\n", receipt.Owner, receipt.Generation, sharedInputMetrics.reports, sharedInputMetrics.polls)
	return nil
}
