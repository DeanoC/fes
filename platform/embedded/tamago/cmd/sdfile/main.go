//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/fatcheck"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

var expectedSDWritePlan string
var expectedSDTransferClock string
var expectedSDBusWidth4Bit string
var expectedSDMultiBlock string

func parseOptInFlag(value, name string) bool {
	switch value {
	case "", "0":
		return false
	case "1":
		return true
	default:
		panic(fmt.Sprintf("sdfile: invalid %s flag %q", name, value))
	}
}

type owned struct{ *sdcontrol.Session }

func (s owned) State() (fatcheck.State, bool) {
	r, valid := s.LastReceipt()
	return fatcheck.State{Identity: s.Identity(), Owner: r.Owner, Generation: r.Generation, Active: r.Active, Prepared: r.Prepared}, valid
}

func main() {
	data, err := base64.StdEncoding.DecodeString(expectedSDWritePlan)
	if err != nil {
		panic(err)
	}
	plan, err := fatcheck.ParsePlan(data)
	if err != nil {
		panic(err)
	}
	var rate uint32
	switch expectedSDTransferClock {
	case "":
	case "25000000":
		rate = 25000000
	default:
		panic("sdfile: unsupported trusted transfer clock")
	}
	busWidth4Bit := parseOptInFlag(expectedSDBusWidth4Bit, "expectedSDBusWidth4Bit")
	multiBlock := parseOptInFlag(expectedSDMultiBlock, "expectedSDMultiBlock")
	if err = de25nano.InitDevices(); err != nil {
		panic(err)
	}
	session, err := de25nano.OpenWritableStorageWithOptions(plan.Identity(), de25nano.StorageOptions{
		TransferClockHz: rate,
		BusWidth4Bit:    busWidth4Bit,
		MultiBlock:      multiBlock,
	})
	if err != nil {
		panic(err)
	}
	if rate != 0 || busWidth4Bit || multiBlock {
		r, valid := session.LastReceipt()
		if !valid || !r.Active || !r.Prepared {
			panic("sdfile: mode selected without active ownership")
		}
		// Open checked the selected divider/mode/stability and card readiness.
		// The driver checks this mode again before every data request.
		fmt.Printf("FES_NANO_SDCLOCK SELECTED hz=%d width=%d multiblock=%t owner=%016x generation=%d\n",
			rate, session.BusWidth(), multiBlock, r.Owner, r.Generation)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	if err = fatcheck.Run(ctx, owned{session}, plan, os.Stdout); err != nil {
		panic(err)
	}
}
