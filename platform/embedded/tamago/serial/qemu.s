//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

TEXT ·Read(SB),NOSPLIT|NOFRAME,$0-2
	MOVD $0x09000000, R0
	MOVWU 0x18(R0), R1
	TBNZ $4, R1, empty
	MOVWU (R0), R1
	MOVB R1, ret+0(FP)
	MOVD $1, R1
	MOVB R1, ret1+1(FP)
	RET
empty:
	MOVB ZR, ret+0(FP)
	MOVB ZR, ret1+1(FP)
	RET
