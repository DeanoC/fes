//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package de25nano

// StartupCheckpoint writes bounded, allocation-free CPU observations even
// before Go initializes packages. It grants no device or bridge ownership.
//
//go:nosplit
func StartupCheckpoint(phase string) {
	ticks, frequency, state := startupRegisters()
	startupText("FES_BOOT_STEP phase=")
	startupText(phase)
	startupText(" ticks=")
	startupHex(ticks)
	startupText(" hz=")
	startupHex(frequency)
	startupText(" el=1 sctlr=")
	startupHex(state)
	startupText("\n")
}

//go:nosplit
func startupText(value string) {
	for i := 0; i < len(value); i++ {
		uartPut(profileConsoleUARTBase, value[i])
	}
}

//go:nosplit
func startupHex(value uint64) {
	for i := 0; i < 16; i++ {
		uartPut(profileConsoleUARTBase, "0123456789abcdef"[(value>>uint(60-4*i))&15])
	}
}

func startupRegisters() (ticks, frequency, state uint64)
