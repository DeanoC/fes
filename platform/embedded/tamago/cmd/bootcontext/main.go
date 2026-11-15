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

var expectedBootContext string

func main() {
	if runtime.GOOS != "tamago" || board.ExceptionLevel() != 1 || board.CounterFrequency() != 400000000 {
		panic("unexpected HPS context CPU contract")
	}
	if len(expectedBootContext) > 8192 {
		panic("oversize compiled HPS context")
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
	observed, err := verifier.Verify(id)
	if err != nil {
		panic(err)
	}
	if !observed.Bridges.NeverEnabled || observed.Bridges.Generation != 1 {
		panic("HPS context diagnostic lost cold bridge scope")
	}
	fmt.Printf("FES_NATIVE_HPS_CONTEXT PASS sha256=%s package_id=%s build_id=%s owner=%x generation=%x programming=unavailable\n",
		hex.EncodeToString(observed.Digest[:]), id.PackageID, id.BuildID, observed.Bridges.Owner, observed.Bridges.Generation)
}
