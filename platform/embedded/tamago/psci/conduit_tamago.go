//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package psci

type smcConduit struct{}
type hvcConduit struct{}

func (smcConduit) Call(x0, x1, x2, x3 uint64) (uint64, uint64, uint64, uint64) {
	return callSMC(x0, x1, x2, x3)
}

func (hvcConduit) Call(x0, x1, x2, x3 uint64) (uint64, uint64, uint64, uint64) {
	return callHVC(x0, x1, x2, x3)
}

// ConduitSMC returns an ARM64 SMC #0 conduit (used under EL3 TF-A BL31 on DE25-Nano).
func ConduitSMC() Conduit { return smcConduit{} }

// ConduitHVC returns an ARM64 HVC #0 conduit (used under QEMU virt -kernel EL1 PSCI).
func ConduitHVC() Conduit { return hvcConduit{} }

// HasEL2 reports whether ID_AA64PFR0_EL1 advertises EL2 support. On QEMU virt
// booted with virtualization=on via entry.S, HCR_EL2.HCD disables HVC from EL1.
//
//go:nosplit
func HasEL2() bool { return hasEL2() }

//go:nosplit
func callSMC(x0, x1, x2, x3 uint64) (r0, r1, r2, r3 uint64)

//go:nosplit
func callHVC(x0, x1, x2, x3 uint64) (r0, r1, r2, r3 uint64)

//go:nosplit
func hasEL2() bool

//go:nosplit
func hasRNDR() bool

//go:nosplit
func readRNDR() (val uint64, ok bool)

//go:nosplit
func readCounter() uint64
