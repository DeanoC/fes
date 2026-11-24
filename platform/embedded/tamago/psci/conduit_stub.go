//go:build !tamago || !arm64

// SPDX-License-Identifier: GPL-3.0-or-later

package psci

import "sync/atomic"

var stubCounter atomic.Uint64

//go:nosplit
func callSMC(x0, x1, x2, x3 uint64) (uint64, uint64, uint64, uint64) {
	return ^uint64(0), 0, 0, 0
}

//go:nosplit
func callHVC(x0, x1, x2, x3 uint64) (uint64, uint64, uint64, uint64) {
	return ^uint64(0), 0, 0, 0
}

//go:nosplit
func hasRNDR() bool { return false }

//go:nosplit
func readRNDR() (uint64, bool) { return 0, false }

//go:nosplit
func readCounter() uint64 { return stubCounter.Add(0x9e3779b97f4a7c15) }
