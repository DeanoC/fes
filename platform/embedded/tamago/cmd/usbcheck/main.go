//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/usbcontrol"
)

var expectedBootContext, expectedHPSIO, expectedUSBControl string
var retainedClient *usbcontrol.Client
var retainedSource *usbcontrol.Source

func run() error {
	if expectedUSBControl != "owned-usb-pio-v1" {
		return errors.New("USB lease was not explicitly selected")
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
	verifier, err := nativefabric.NewHPSContextVerifier(de25nano.FabricCPUTransport{}, raw)
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
	retainedSource, err = de25nano.OpenKeyboardWithControl(ctx, retainedClient)
	if err != nil {
		return err
	}
	fmt.Println("FES_NATIVE_USB READY transport=pio input=boot-keyboard data=held")
	// A bounded diagnostic. The eventual launcher retains this source for its session.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		report, pollErr := retainedSource.PollKeyboard()
		if pollErr != nil {
			err = pollErr
			break
		}
		if report != nil {
			fmt.Printf("FES_NATIVE_USB REPORT %x\n", report)
		}
		time.Sleep(time.Millisecond)
	}
	stop, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	closeErr := retainedSource.Close(stop)
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
