//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

// Package de25nano supplies exploratory Agilex 5 EL1 boot hooks. Firmware
// must initialize DDR, clocks, pinmux, SDHC and the UART before entry.
package de25nano

import (
	"runtime/goos"
	_ "unsafe"

	"github.com/usbarmory/tamago/arm64"
)

// The boot command must provide exactly this RAM window and enter at EL1.
// The image links above the early page-table area used by TamaGo's ARM64 MMU.
//
//go:linkname ramStart runtime/goos.RamStart
var ramStart uint64 = profileRAMStart

//go:linkname ramSize runtime/goos.RamSize
var ramSize uint64 = profileRAMSize

//go:linkname ramStackOffset runtime/goos.RamStackOffset
var ramStackOffset uint64 = 0x100

var cpu arm64.CPU

//go:linkname hwinit1 runtime/goos.Hwinit1
func hwinit1() {
	if ExceptionLevel() != 1 || CounterFrequency() == 0 {
		halt()
	}
	cpu.Init()
	// Polling baseline: interrupts remain masked and no GIC is claimed.
	// The runtime's deadline loop can keep inspecting Nanotime without WFI.
	goos.Idle = func(int64) {}
}

// CounterFrequency reports the architectural frequency for the boot probe.
func CounterFrequency() uint64

// ExceptionLevel reports the current exception level for the boot probe.
func ExceptionLevel() uint64

//go:linkname nanotime runtime/goos.Nanotime
//go:nosplit
func nanotime() int64 { return timerNanos() }

//go:linkname printk runtime/goos.Printk
//go:nosplit
func printk(c byte) { uartPut(profileConsoleUARTBase, c) }

func timerNanos() int64
func uartPut(base uint64, c byte)
func halt()

// This deterministic source satisfies runtime hash seeding only; a DE25 platform
// must provide authenticated firmware/TRNG entropy before security use.
var diagnosticSeed uint64 = 0x6a09e667f3bcc909

//go:linkname initRNG runtime/goos.InitRNG
//go:nosplit
func initRNG() {}

//go:linkname getRandomData runtime/goos.GetRandomData
//go:nosplit
func getRandomData(buf []byte) {
	for i := range buf {
		diagnosticSeed ^= diagnosticSeed << 13
		diagnosticSeed ^= diagnosticSeed >> 7
		diagnosticSeed ^= diagnosticSeed << 17
		buf[i] = byte(diagnosticSeed)
	}
}
