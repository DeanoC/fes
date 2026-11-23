//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// func read32(addr uint64) uint32
TEXT ·read32(SB),NOSPLIT|NOFRAME,$0-12
	MOVD	addr+0(FP), R0
	MOVWU	(R0), R1
	DSB	$15
	MOVW	R1, ret+8(FP)
	RET

// func write32(addr uint64, val uint32)
TEXT ·write32(SB),NOSPLIT|NOFRAME,$0-12
	MOVD	addr+0(FP), R0
	MOVWU	val+8(FP), R1
	DSB	$15
	MOVW	R1, (R0)
	DSB	$15
	RET
