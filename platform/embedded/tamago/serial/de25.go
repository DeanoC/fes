//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
package serial

// Read polls the UART selected by the explicit firmware handoff.
func Read() (byte, bool) { return read(profileConsoleUARTBase) }

func read(base uint64) (byte, bool)
