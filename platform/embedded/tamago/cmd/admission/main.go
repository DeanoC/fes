//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"encoding/json"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/admissioncheck"
	"github.com/DeanoC/fes/platform/embedded/tamago/block"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/qemuvirt"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/runtimeadmission"
)

func run() error {
	fmt.Printf("FES native runtime admission EL=%d physical_activation=unavailable\n", qemuvirt.ExceptionLevel())
	if _, err := qemuvirt.InitDevices(); err != nil {
		return err
	}
	disk, err := block.ScanVirtIO()
	if err != nil {
		return err
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		return err
	}
	data, err := volume.ReadFile("FES/RUNTIME.JSON")
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("model policy too large")
	}
	var policy runtimeadmission.Policy
	if err = json.Unmarshal(data, &policy); err != nil {
		return err
	}
	data, err = volume.ReadFile("FES/ADMIT.JSON")
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return fmt.Errorf("case index too large")
	}
	var cases []admissioncheck.Case
	if err = json.Unmarshal(data, &cases); err != nil {
		return err
	}
	err = admissioncheck.Run(policy, cases, volume.ReadFile, func(result admissioncheck.Result) {
		encoded, _ := json.Marshal(result)
		fmt.Printf("FES_ADMISSION_CASE %s\n", encoded)
	})
	if err != nil {
		return err
	}
	fmt.Printf("FES_NATIVE_ADMISSION PASS cases=%d runtime_id=%s physical_activation=unavailable\n", len(cases), policy.RuntimeID)
	return nil
}
func main() {
	if err := run(); err != nil {
		panic(err)
	}
}
