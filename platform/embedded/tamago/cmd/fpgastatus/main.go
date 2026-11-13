//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
	"runtime"
)

var expectedBootContext, expectedControlMap string

func main() {
	if runtime.GOOS != "tamago" || board.ExceptionLevel() != 1 || board.CounterFrequency() != 400000000 || len(expectedBootContext) > 8192 || len(expectedControlMap) > 8192 {
		panic("unexpected FPGA status CPU/source contract")
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
	observed, err := v.Configuration(control)
	if err != nil {
		panic(err)
	}
	if observed.Source != source {
		panic("FPGA status query changed source ownership")
	}
	ready := 0
	if observed.Configured {
		ready = 1
	}
	fmt.Printf("FES_NATIVE_FPGA_STATUS CLOSED ready=%d owner=%x generation=1 programming=unavailable\n", ready, source.Bridges.Owner)
}
