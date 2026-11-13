//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"runtime"

	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/nativefabric"
)

var expectedBootContext, expectedHPSIO string

func main() {
	if runtime.GOOS != "tamago" || board.ExceptionLevel() != 1 || board.CounterFrequency() != 400000000 {
		panic("unexpected HPS I/O CPU contract")
	}
	if len(expectedBootContext) > 8192 || len(expectedHPSIO) > 48<<10 {
		panic("oversize compiled HPS I/O context")
	}
	raw, err := base64.StdEncoding.DecodeString(expectedBootContext)
	if err != nil {
		panic(err)
	}
	verifier, err := nativefabric.NewHPSContextVerifier(de25nano.FabricCPUTransport{}, raw)
	if err != nil {
		panic(err)
	}
	id := verifier.Identity()
	before, err := verifier.Verify(id)
	if err != nil {
		panic(err)
	}
	if !before.Bridges.NeverEnabled || before.Bridges.Generation != 1 {
		panic("HPS I/O diagnostic lost cold scope")
	}
	fmt.Printf("FES_NATIVE_HPS_CONTEXT PASS sha256=%s package_id=%s build_id=%s owner=%x generation=%x programming=unavailable\n",
		hex.EncodeToString(before.Digest[:]), id.PackageID, id.BuildID, before.Bridges.Owner, before.Bridges.Generation)
	mapping, err := base64.StdEncoding.DecodeString(expectedHPSIO)
	if err != nil {
		panic(err)
	}
	observed, err := verifier.ProbeHPSIO(mapping)
	if err != nil {
		panic(err)
	}
	if before != observed.Source {
		panic("HPS I/O diagnostic changed cold scope")
	}
	fmt.Printf("FES_NATIVE_HPS_IO PASS map_sha256=%s count=%d reads=%d owner=%x generation=%x data_held=1 runtime_admission=unavailable\n",
		hex.EncodeToString(observed.Digest[:]), observed.Count, observed.Count*2, observed.Source.Bridges.Owner, observed.Source.Bridges.Generation)
}
