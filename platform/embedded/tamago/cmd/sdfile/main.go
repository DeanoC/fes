//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"context"
	"encoding/base64"
	"os"
	"time"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/de25nano"
	"github.com/DeanoC/fes/platform/embedded/tamago/fatcheck"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
)

var expectedSDWritePlan string

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
	if err = de25nano.InitDevices(); err != nil {
		panic(err)
	}
	session, err := de25nano.OpenWritableStorage(plan.Identity())
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	if err = fatcheck.Run(ctx, owned{session}, plan, os.Stdout); err != nil {
		panic(err)
	}
}
