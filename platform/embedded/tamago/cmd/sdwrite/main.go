//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/base64"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
	"os"
)

var expectedSDWritePlan string

func main() {
	data, err := base64.StdEncoding.DecodeString(expectedSDWritePlan)
	if err != nil {
		panic(err)
	}
	plan, err := sdcontrol.ParseScratchPlan(data)
	if err != nil {
		panic(err)
	}
	if err = de25nano.InitDevices(); err != nil {
		panic(err)
	}
	session, err := de25nano.OpenWritableStorage(plan.Identity())
	if err != nil {
		panic(err)
	}
	if err = sdcontrol.RunScratch(session, plan, os.Stdout); err != nil {
		panic(err)
	}
}
