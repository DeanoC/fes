//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

TEXT ·smc(SB),NOSPLIT|NOFRAME,$0-88
 MOVD request+0(FP), R0
 MOVD request+8(FP), R1
 MOVD request+16(FP), R2
 MOVD request+24(FP), R3
 MOVD request+32(FP), R4
 MOVD request+40(FP), R5
 MOVD request+48(FP), R6
 DSB $15
 WORD $0xd4000003 // SMC #0
 DSB $15
 ISB $15
 MOVD R0, response+56(FP)
 MOVD R1, response+64(FP)
 MOVD R2, response+72(FP)
 MOVD R3, response+80(FP)
 RET
