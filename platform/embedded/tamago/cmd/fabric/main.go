//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/qemuvirt"
	"github.com/DeanoC/fes/platform/embedded/tamago/fabricmodel"
)

func main() {
	fmt.Printf("FES fabric lifecycle model EL=%d hardware=none\n", qemuvirt.ExceptionLevel())
	if failures := fabricmodel.RunChecks(func(name string, err error) {
		if err != nil {
			fmt.Printf("FES_FABRIC_CHECK %s FAIL %v\n", name, err)
		} else {
			fmt.Printf("FES_FABRIC_CHECK %s PASS\n", name)
		}
	}); failures != 0 {
		panic("fabric model checks failed")
	}
	fmt.Println("FES_FABRIC_MODEL PASS hardware=none")
}
