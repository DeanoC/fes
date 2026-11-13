//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"
TEXT ·load32(SB),NOSPLIT|NOFRAME,$0-12
 MOVD addr+0(FP), R0
 DMB $15
 MOVWU (R0), R1
 DMB $15
 MOVW R1, ret+8(FP)
 RET
TEXT ·store32(SB),NOSPLIT|NOFRAME,$0-12
 MOVD addr+0(FP), R0
 MOVWU value+8(FP), R1
 DMB $15
 MOVW R1, (R0)
 DSB $15
 RET
