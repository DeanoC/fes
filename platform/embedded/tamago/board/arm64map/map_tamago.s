//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

TEXT ·cpuState(SB),NOSPLIT|NOFRAME,$0-56
	DSB $15
	ISB $15
	MRS CurrentEL, R0
	MOVD R0, values_0+0(FP)
	MRS SCTLR_EL1, R0
	MOVD R0, values_1+8(FP)
	MRS TCR_EL1, R0
	MOVD R0, values_2+16(FP)
	MRS TTBR0_EL1, R0
	MOVD R0, values_3+24(FP)
	MRS MAIR_EL1, R0
	MOVD R0, values_4+32(FP)
	MRS ID_AA64MMFR0_EL1, R0
	MOVD R0, values_5+40(FP)
	MRS MPIDR_EL1, R0
	MOVD R0, values_6+48(FP)
	RET

// AT probes permissions/attributes without accessing the target memory.
// Restore the caller's PAR_EL1; no result from another query is retained.
TEXT ·translateRead(SB),NOSPLIT|NOFRAME,$0-16
	MOVD address+0(FP), R0
	MRS PAR_EL1, R1
	DSB $15
	WORD $0xd5087800 // AT S1E1R, X0
	ISB $15
	MRS PAR_EL1, R0
	MOVD R0, par+8(FP)
	MSR R1, PAR_EL1
	ISB $15
	RET

TEXT ·translateWrite(SB),NOSPLIT|NOFRAME,$0-16
	MOVD address+0(FP), R0
	MRS PAR_EL1, R1
	DSB $15
	WORD $0xd5087820 // AT S1E1W, X0
	ISB $15
	MRS PAR_EL1, R0
	MOVD R0, par+8(FP)
	MSR R1, PAR_EL1
	ISB $15
	RET

TEXT ·setUncachedAttribute(SB),NOSPLIT|NOFRAME,$0
	MRS MAIR_EL1, R0
	MOVD $0xff0000, R1
	BIC R1, R0, R0
	MOVD $0x440000, R1
	ORR R1, R0, R0
	MSR R0, MAIR_EL1
	DSB $15
	ISB $15
	RET

TEXT ·publishTableEntry(SB),NOSPLIT|NOFRAME,$0-8
	MOVD address+0(FP), R0
	WORD $0xd50b7a20 // DC CVAC, X0
	DSB $15
	TLBI VMALLE1
	DSB $15
	ISB $15
	RET
