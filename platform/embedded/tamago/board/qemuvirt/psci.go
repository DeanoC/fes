//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later

package qemuvirt

import "github.com/DeanoC/fes/platform/embedded/tamago/psci"

// PSCI returns the ARM PSCI / SMCCC TRNG client for QEMU virt. When booted
// with virtualization=on through entry.S (which sets HCR_EL2.HCD=1 to disable
// HVC from EL1), it returns a disabled client whose calls yield psci.ErrUnsupported.
func PSCI() *psci.Client {
	if psci.HasEL2() {
		return psci.New(nil)
	}
	return psci.New(psci.HVC())
}

// PowerOff requests an orderly VM power-off via PSCI_SYSTEM_OFF.
func PowerOff() error { return PSCI().SystemOff() }

// Reset requests a VM reboot via PSCI_SYSTEM_RESET.
func Reset() error { return PSCI().SystemReset() }

// EntropySource reports the active entropy backend discovered at boot.
func EntropySource() psci.EntropySource { return psci.HardwareEntropySource() }
