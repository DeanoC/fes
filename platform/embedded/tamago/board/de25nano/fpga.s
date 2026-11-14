//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// Name each array element so asmdecl can verify the ABI0 offsets.
TEXT ·smc(SB),NOSPLIT|NOFRAME,$0-88
 MOVD request_0+0(FP), R0
 MOVD request_1+8(FP), R1
 MOVD request_2+16(FP), R2
 MOVD request_3+24(FP), R3
 MOVD request_4+32(FP), R4
 MOVD request_5+40(FP), R5
 MOVD request_6+48(FP), R6
 DSB $15
 WORD $0xd4000003 // SMC #0
 DSB $15
 ISB $15
 MOVD R0, response_0+56(FP)
 MOVD R1, response_1+64(FP)
 MOVD R2, response_2+72(FP)
 MOVD R3, response_3+80(FP)
 RET
