//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"github.com/DeanoC/fes/platform/embedded/tamago/board"
	"github.com/DeanoC/fes/platform/embedded/tamago/fat"
	"github.com/DeanoC/fes/platform/embedded/tamago/sdcontrol"
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
	if board.Name == "de25-nano" {
		nano, ok := disk.(*sdcontrol.Session)
		if !ok {
			panic("Nano storage lacks firmware ownership")
		}
		first, err := volume.ReadFile("FES/RECOVERY.JSON")
		if err != nil {
			panic(err)
		}
		before, valid := nano.LastReceipt()
		if !valid || !before.Active || !before.Prepared {
			panic("invalid active SD receipt")
		}
		identity := nano.Identity()
		fmt.Printf("FES_NANO_SD READ bytes=%d sha256=%x cid=%x sectors=%d owner=%016x generation=%d\n",
			len(first), sha256.Sum256(first), identity.CID, identity.Sectors, before.Owner, before.Generation)
		if err = nano.Quiesce(); err != nil {
			panic(err)
		}
		contained, valid := nano.LastReceipt()
		if !valid || contained.Active || contained.Owner != before.Owner || contained.Generation != before.Generation+1 {
			panic("invalid contained SD receipt")
		}
		fmt.Printf("FES_NANO_SD CONTAINED owner=%016x generation=%d\n", contained.Owner, contained.Generation)
		if err = nano.Resume(); err != nil {
			panic(err)
		}
		second, err := volume.ReadFile("FES/RECOVERY.JSON")
		if err != nil {
			panic(err)
		}
		resumed, valid := nano.LastReceipt()
		if !valid || !resumed.Active || !resumed.Prepared || resumed.Owner != before.Owner ||
			resumed.Generation != contained.Generation+2 || nano.Identity() != identity || !bytes.Equal(first, second) {
			panic("SD resume changed media, bytes or ownership")
		}
		fmt.Printf("FES_NANO_SD RESUMED bytes=%d sha256=%x owner=%016x generation=%d\n",
			len(second), sha256.Sum256(second), resumed.Owner, resumed.Generation)
		if err = nano.Quiesce(); err != nil {
			panic(err)
		}
		final, valid := nano.LastReceipt()
		if !valid || final.Active || final.Owner != before.Owner || final.Generation != resumed.Generation+1 {
			panic("invalid final SD containment")
		}
		fmt.Printf("FES_NANO_SD PASS read-only owner=%016x generation=%d\n", final.Owner, final.Generation)
		return
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
