//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package main

import (
	"fmt"
	"runtime"

	"github.com/DeanoC/fes/platform/embedded/tamago/board/arm64map"
	"github.com/DeanoC/fes/platform/embedded/tamago/board/qemuvirt"
	"github.com/usbarmory/tamago/dma"
)

func main() {
	if runtime.GOOS != "tamago" {
		panic("native map check requires bare metal")
	}
	if _, err := qemuvirt.InitDevices(); err != nil {
		panic(err)
	}
	address, data := dma.Reserve(513, 64)
	if len(data) != 513 || uint64(address) < qemuvirt.DMABase || uint64(address)+513 > qemuvirt.DMABase+qemuvirt.DMABytes {
		panic("DMA allocation outside owned QEMU region")
	}
	ranges := []arm64map.Range{{Base: 0x40000000, Bytes: 256 << 20, Kind: arm64map.WriteBack},
		{Base: 0x09000000, Bytes: 4, Kind: arm64map.Device},
		{Base: qemuvirt.DMABase, Bytes: 128 << 20, Kind: arm64map.Uncached}}
	before, err := arm64map.Verify(0x40000000, 256<<20, ranges)
	if err != nil {
		panic(err)
	}
	// Exercise ordinary unaligned Go copies only after the actual CPU reports
	// normal uncached read/write access. No disk/network controller is started.
	pattern := make([]byte, 512)
	for i := range pattern {
		pattern[i] = byte(i*37 + 11)
	}
	copy(data[1:], pattern)
	for i, value := range pattern {
		if data[i+1] != value {
			panic("uncached DMA publication changed")
		}
	}
	after, err := arm64map.Verify(0x40000000, 256<<20, ranges)
	if err != nil {
		panic(err)
	}
	if before != after {
		panic("CPU mapping changed across the DMA copy")
	}
	fmt.Printf("FES_NATIVE_CPU_MAP PASS el=%d sctlr=%x tcr=%x root=%x mair=%x mmfr0=%x mpidr=%x reads=%d translations=%d bytes=%d descriptor_sha256=%x mapping_only=1\n",
		after.CPU.EL>>2, after.CPU.SCTLR, after.CPU.TCR, after.CPU.TTBR0, after.CPU.MAIR, after.CPU.MMFR0, after.CPU.MPIDR,
		after.TableReads, after.Translations, after.Bytes, after.DescriptorSHA256)
}
