//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package serial

const qemuUART0IRQ uint32 = 33 // QEMU virt PL011 UART0 (SPI 1)

func consoleIRQ() (uint32, error) { return qemuUART0IRQ, nil }
func readRawStatus() (byte, bool, LineError) {
	dr, ok := readDR()
	if !ok {
		return 0, false, 0
	}
	return byte(dr), true, DecodePL011DR(dr)
}
func readDR() (uint32, bool)
func enableHWIRQ()
func disableHWIRQ()
func clearHWIRQ()
