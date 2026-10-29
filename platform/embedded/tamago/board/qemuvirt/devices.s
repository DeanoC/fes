//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

TEXT ·tableBase(SB),NOSPLIT|NOFRAME,$0-8
	MRS TTBR0_EL1, R0
	MOVD R0, ret+0(FP)
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
