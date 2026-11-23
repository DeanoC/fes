//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later

package de25nano

import "github.com/DeanoC/fes/platform/embedded/tamago/psci"

var psciClient = psci.New(psci.ConduitSMC())

// PSCI returns the ARM PSCI / SMCCC TRNG client backed by TF-A BL31 SMC #0.
func PSCI() *psci.Client { return psciClient }

// PowerOff requests an orderly SoC power-off via PSCI_SYSTEM_OFF.
func PowerOff() error { return psciClient.SystemOff() }

// Reset requests a cold SoC reboot via PSCI_SYSTEM_RESET.
func Reset() error { return psciClient.SystemReset() }

// EntropySource reports the active entropy backend discovered at boot.
func EntropySource() psci.EntropySource { return psci.HardwareEntropySource() }
