//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

TEXT ·read8(SB),NOSPLIT|NOFRAME,$0-9
	MOVD address+0(FP), R0
	MOVBU (R0), R1
	MOVB R1, ret+8(FP)
	RET

TEXT ·write16(SB),NOSPLIT|NOFRAME,$0-10
	MOVD address+0(FP), R0
	MOVHU value+8(FP), R1
	MOVH R1, (R0)
	RET

TEXT ·write64(SB),NOSPLIT|NOFRAME,$0-16
	MOVD address+0(FP), R0
	MOVD value+8(FP), R1
	MOVD R1, (R0)
	RET

TEXT ·barrier(SB),NOSPLIT|NOFRAME,$0
	DSB $15
	RET
