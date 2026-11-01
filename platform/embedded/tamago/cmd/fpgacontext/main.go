//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

var expectedBootContext, expectedControlMap string

func main() {
	if runtime.GOOS != "tamago" || board.ExceptionLevel() != 1 || board.CounterFrequency() != 400000000 ||
		len(expectedBootContext) > 8192 || len(expectedControlMap) > 8192 {
		panic("unexpected FPGA identity CPU/source contract")
	}
	raw, err := base64.StdEncoding.DecodeString(expectedBootContext)
	if err != nil {
		panic(err)
	}
	v, err := nativefabric.NewHPSContextVerifier(de25nano.FabricCPUTransport{}, raw)
	if err != nil {
		panic(err)
	}
	id := v.Identity()
	source, err := v.Verify(id)
	if err != nil {
		panic(err)
	}
	fmt.Printf("FES_NATIVE_HPS_CONTEXT PASS sha256=%s package_id=%s build_id=%s owner=%x generation=%x programming=unavailable\n",
		hex.EncodeToString(source.Digest[:]), id.PackageID, id.BuildID, source.Bridges.Owner, source.Bridges.Generation)
	control, err := base64.StdEncoding.DecodeString(expectedControlMap)
	if err != nil {
		panic(err)
	}
	observed, err := v.ProbeIdentity(control)
	if errors.Is(err, nativefabric.ErrIdentityNotReady) {
		after, check := v.Verify(id)
		if check != nil || after != source || !after.Bridges.NeverEnabled || after.Bridges.Generation != 1 {
			panic("unready FPGA probe lost closed source ownership")
		}
		fmt.Printf("FES_NATIVE_FPGA_UNREADY CLOSED owner=%x generation=1 programming=unavailable\n", source.Bridges.Owner)
		return
	}
	if err != nil {
		panic(err)
	}
	if observed.Source != source {
		panic("FPGA identity probe changed source ownership")
	}
	fmt.Printf("FES_NATIVE_FPGA_IDENTITY PASS package_id=%s build_id=%s abi=%x base=%x map_sha256=%s owner=%x generation=%x data_held=1 programming=unavailable\n",
		id.PackageID, id.BuildID, observed.Identity.ABI, observed.CPUBase, hex.EncodeToString(observed.ControlMapSHA256[:]), observed.After.Owner, observed.After.Generation)
}
