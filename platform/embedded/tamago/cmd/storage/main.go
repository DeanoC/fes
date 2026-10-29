//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"crypto/sha256"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
)

func main() {
	fmt.Printf("FES_STORAGE_PROBE board=%s EL=%d counter=%dHz writes=disabled\n", board.Name, board.ExceptionLevel(), board.CounterFrequency())
	if _, err := board.InitDevices(); err != nil {
		panic(err)
	}
	disk, err := board.OpenStorage()
	if err != nil {
		panic(err)
	}
	volume, err := fat.Mount(disk)
	if err != nil {
		panic(err)
	}
	data, err := volume.ReadFile("FES/BOOT.JSON")
	if err != nil {
		panic(err)
	}
	if err = disk.Flush(); err != nil {
		panic(err)
	}
	fmt.Printf("FES_STORAGE_PROBE PASS sectors=%d boot_bytes=%d boot_sha256=%x\n", disk.SectorCount(), len(data), sha256.Sum256(data))
}
