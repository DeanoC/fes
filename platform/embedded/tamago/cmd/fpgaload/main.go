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
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"github.com/DeanoC/fes/platform/embedded/tamago/packagestore"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

var expectedBootContext, expectedControlMap, expectedConfigurationMap string
var retained *nativefabric.ConfigurationTransfer

func decoded(s string, limit int) []byte {
	if len(s) == 0 || len(s) > limit {
		panic("missing or oversized compiled FPGA source")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func main() {
	if runtime.GOOS != "tamago" || board.ExceptionLevel() != 1 || board.CounterFrequency() != 400000000 {
		panic("unexpected FPGA configuration CPU/source contract")
	}
	cpu := de25nano.FabricCPUTransport{}
	v, err := nativefabric.NewHPSContextVerifier(cpu, decoded(expectedBootContext, 8192))
	if err != nil {
		panic(err)
	}
	id := v.Identity()
	control, configuration := decoded(expectedControlMap, 8192), decoded(expectedConfigurationMap, 32768)
	disk, err := board.OpenStorage()
	if err != nil {
		panic(err)
	}
	lease, ok := disk.(*sdcontrol.Session)
	if !ok {
		panic(errors.New("FPGA load requires owned SD storage"))
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		panic(err)
	}
	store, err := packagestore.New(volume, lease)
	if err != nil {
		panic(err)
	}
	selection, err := store.Retain(context.Background(), packagestore.Request{Path: "FES/PKGTEST.BIN", PackageID: id.PackageID})
	if err != nil {
		panic(err)
	}
	handoff, err := store.Contain(selection)
	if err != nil {
		panic(err)
	}
	closed := handoff.Receipt()
	fmt.Printf("FES_NANO_LOAD_PACKAGE CONTAINED package_id=%s owner=%x generation=%d source=sd storage_active=0\n", id.PackageID, closed.Owner, closed.Generation)
	var source nativefabric.HPSBootObservation
	retained, source, err = v.NewStoredConfigurationTransfer(control, configuration, handoff, de25nano.ReserveConfiguration)
	if err != nil {
		panic(err) // Board reservation remains pinned, including constructor errors.
	}
	size := retained.Status().RetainedBytes
	fmt.Printf("FES_NATIVE_HPS_CONTEXT PASS sha256=%s package_id=%s build_id=%s owner=%x generation=%d runtime_programming=unavailable\n",
		hex.EncodeToString(source.Digest[:]), id.PackageID, id.BuildID, source.Bridges.Owner, source.Bridges.Generation)
	defer func() {
		failure := recover()
		if failure == nil {
			return
		}
		if status := retained.Status(); status.Failed && status.Outstanding > 0 {
			observation, err := v.RetireConfiguration(retained, source, configuration)
			if err != nil {
				fmt.Printf("FES_NATIVE_FPGA_RETIRE REJECT error=%v retained=%d\n", err, status.RetainedBytes)
			} else {
				fmt.Printf("FES_NATIVE_FPGA_RETIRE ACK owner=%x generation=%d status=%d retained=%d activation=unavailable\n",
					observation.Owner, observation.Generation, observation.Status, status.RetainedBytes)
			}
		}
		panic(failure)
	}()
	started := time.Now()
	deadline := started.Add(20 * time.Second)
	for {
		phase := retained.Status().Phase
		if !time.Now().Before(deadline) {
			panic(retained.Cancel(fmt.Errorf("FPGA configuration deadline")))
		}
		done, busy, err := retained.Advance()
		if !time.Now().Before(deadline) {
			_ = retained.Cancel(fmt.Errorf("late FPGA firmware return"))
			status := retained.Status()
			panic(fmt.Sprintf("late FPGA firmware return phase=%s elapsed_us=%d accepted=%d completed=%d outstanding=%d retained=%d",
				phase, time.Since(started).Microseconds(), status.AcceptedBytes, status.CompletedBytes, status.Outstanding, status.RetainedBytes))
		}
		if err != nil {
			panic(err)
		}
		if done {
			break
		}
		if busy {
			time.Sleep(time.Millisecond)
		}
	}
	status := retained.Status()
	after, err := v.Verify(id)
	if err != nil || after != source || status.AcceptedBytes != size || status.CompletedBytes != size || status.Outstanding != 0 || !status.Done || status.Failed {
		panic("FPGA configuration did not retain closed source ownership")
	}
	fmt.Printf("FES_NATIVE_FPGA_PROGRAMMED PASS package_id=%s core_sha256=%s bytes=%d owner=%x generation=%d data_held=1 activation=unavailable\n",
		id.PackageID, id.ProgrammedSHA256, size, source.Bridges.Owner, source.Bridges.Generation)
	observed, err := v.ProbeIdentity(control)
	if err != nil {
		panic(err)
	}
	if observed.Source != source {
		panic("FPGA identity changed source context")
	}
	fmt.Printf("FES_NATIVE_FPGA_IDENTITY PASS package_id=%s build_id=%s abi=%x base=%x map_sha256=%s owner=%x generation=%d data_held=1 runtime_programming=unavailable\n",
		id.PackageID, id.BuildID, observed.Identity.ABI, observed.CPUBase, hex.EncodeToString(observed.ControlMapSHA256[:]), observed.After.Owner, observed.After.Generation)
}
