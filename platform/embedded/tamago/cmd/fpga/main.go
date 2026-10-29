//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
)

func main() {
	probe, err := de25nano.OpenFPGAProbe()
	if err != nil {
		panic(err)
	}
	o, err := probe.Probe()
	if err != nil {
		panic(err)
	}
	fmt.Printf("FES_SDM_OBSERVATION idcode=%08x state=%08x version=%08x pins=%08x soft=%08x error_location=%08x error_details=%08x configured=%t\n", o.IDCode, o.State, o.Version, o.Pins, o.Soft, o.ErrorLocation, o.ErrorDetails, o.Configured())
	fmt.Println("FES_SDM_PROBE complete; physical activation unavailable; no live package identity or memory drain proved")
}
