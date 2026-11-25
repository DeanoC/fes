//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package serial

import "errors"

// Agilex 5 DesignWare APB UART base addresses and GIC-600 SPI INTIDs.
const (
	de25UART0Base uint64 = 0x10c02000
	de25UART1Base uint64 = 0x10c02100
	de25UART0IRQ  uint32 = 140 // SPI 108
	de25UART1IRQ  uint32 = 141 // SPI 109
)

func consoleIRQ() (uint32, error) {
	switch uint64(profileConsoleUARTBase) {
	case de25UART0Base:
		return de25UART0IRQ, nil
	case de25UART1Base:
		return de25UART1IRQ, nil
	default:
		return 0, errors.New("serial: unsupported console UART base")
	}
}

func readRawStatus() (byte, bool, LineError) {
	data, lsr := readReg(profileConsoleUARTBase)
	return data, lsr&1 != 0, DecodeDWAPBLSR(uint32(lsr))
}
func enableHWIRQ()  { dwEnableRXIRQ(profileConsoleUARTBase) }
func disableHWIRQ() { dwDisableRXIRQ(profileConsoleUARTBase) }
func clearHWIRQ()   { dwClearRXIRQ(profileConsoleUARTBase) }

func readReg(base uint64) (byte, byte)
func dwEnableRXIRQ(base uint64)
func dwDisableRXIRQ(base uint64)
func dwClearRXIRQ(base uint64)
