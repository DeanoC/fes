//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/usb"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

var expectedBootContext, expectedHPSIO, expectedUSBControl, expectedUSBInput string
var retainedClient *usbcontrol.Client
var retainedSource *usbcontrol.Source
var retainedGamepadSource *usbcontrol.GamepadSource

func run() error {
	if expectedUSBControl != "owned-usb-pio-v1" {
		return errors.New("USB lease was not explicitly selected")
	}
	if expectedUSBInput != "boot-keyboard" && expectedUSBInput != "gamepad" {
		return errors.New("USB input protocol was not explicitly selected")
	}
	if runtime.GOOS != "tamago" || board.ExceptionLevel() != 1 || board.CounterFrequency() != 400000000 {
		return errors.New("USB CPU contract differs")
	}
	if _, err := board.InitDevices(); err != nil {
		return err
	}
	if len(expectedBootContext) > 8192 || len(expectedHPSIO) > 48<<10 {
		return errors.New("oversize USB source policy")
	}
	raw, err := base64.StdEncoding.DecodeString(expectedBootContext)
	if err != nil {
		return err
	}
	var verifier *nativefabric.USBHPSContextVerifier
	verifier, err = nativefabric.NewUSBHPSContextVerifier(de25nano.FabricCPUTransport{}, raw)
	if err != nil {
		return err
	}
	id := verifier.Identity()
	before, err := verifier.Verify(id)
	if err != nil {
		return err
	}
	if !before.Bridges.NeverEnabled || before.Bridges.Generation != 1 {
		return errors.New("USB lost cold scope")
	}
	fmt.Printf("FES_NATIVE_HPS_CONTEXT PASS sha256=%s package_id=%s build_id=%s owner=%x generation=%x programming=unavailable\n", hex.EncodeToString(before.Digest[:]), id.PackageID, id.BuildID, before.Bridges.Owner, before.Bridges.Generation)
	mapping, err := base64.StdEncoding.DecodeString(expectedHPSIO)
	if err != nil {
		return err
	}
	observed, err := verifier.ProbeHPSIO(mapping)
	if err != nil {
		return err
	}
	if before != observed.Source {
		return errors.New("USB pins changed cold scope")
	}
	fmt.Printf("FES_NATIVE_HPS_IO PASS map_sha256=%s count=%d reads=%d owner=%x generation=%x data_held=1 runtime_admission=unavailable\n", hex.EncodeToString(observed.Digest[:]), observed.Count, observed.Count*2, before.Bridges.Owner, before.Bridges.Generation)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	retainedClient, err = usbcontrol.New(ctx, de25nano.FabricCPUTransport{}, before.Bridges.Owner)
	if err != nil {
		return err
	}
	var poll func() ([]byte, error)
	var closeSource func(context.Context) error
	var gamepad *usb.Gamepad
	if expectedUSBInput == "gamepad" {
		var descriptor []byte
		retainedGamepadSource, descriptor, err = de25nano.OpenGamepadWithControl(ctx, retainedClient)
		if len(descriptor) != 0 {
			digest := sha256.Sum256(descriptor)
			fmt.Printf("FES_NATIVE_USB DESCRIPTOR sha256=%x bytes=%d data=%x\n", digest, len(descriptor), descriptor)
		}
		if err == nil {
			gamepad, err = usb.ParseGamepad(descriptor)
			poll, closeSource = retainedGamepadSource.PollGamepad, retainedGamepadSource.Close
		}
	} else {
		retainedSource, err = de25nano.OpenKeyboardWithControl(ctx, retainedClient)
		if err == nil {
			poll, closeSource = retainedSource.PollKeyboard, retainedSource.Close
		}
	}
	if err != nil {
		return err
	}
	fmt.Printf("FES_NATIVE_USB READY transport=pio input=%s data=held\n", expectedUSBInput)
	// A bounded diagnostic. The eventual launcher retains this source for its session.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		report, pollErr := poll()
		if pollErr != nil {
			err = pollErr
			break
		}
		if report != nil {
			if gamepad == nil {
				fmt.Printf("FES_NATIVE_USB REPORT %x\n", report)
			} else {
				state, _, decodeErr := gamepad.Feed(report)
				if decodeErr != nil {
					err = decodeErr
					break
				}
				fmt.Printf("FES_NATIVE_USB GAMEPAD report=%x axes=%d,%d,%d,%d,%d,%d buttons=%08x hat=%d\n", report, state.Axes[0], state.Axes[1], state.Axes[2], state.Axes[3], state.Axes[4], state.Axes[5], state.Buttons, state.Hat)
			}
		}
		time.Sleep(time.Millisecond)
	}
	stop, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	closeErr := closeSource(stop)
	cancelStop()
	if err = errors.Join(err, closeErr); err != nil {
		return err
	}
	receipt, valid := retainedClient.LastReceipt()
	if !valid || receipt.Active || receipt.Confirmed {
		return errors.New("USB stop receipt absent")
	}
	fmt.Printf("FES_NATIVE_USB STOP owner=%x generation=%d data=held\n", receipt.Owner, receipt.Generation)
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Printf("FES_NATIVE_USB FAIL %v\n", err)
		return
	}
	fmt.Println("FES_NATIVE_USB PASS scope=usb-only-pio")
}
