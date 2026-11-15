//go:build tamago && arm64 && !de25nano

#include "textflag.h"

TEXT ·read32(SB),NOSPLIT,$0-12
 MOVD address+0(FP), R0
 MOVWU (R0), R1
 MOVW R1, ret+8(FP)
 RET

TEXT ·write32(SB),NOSPLIT,$0-12
 MOVD address+0(FP), R0
 MOVWU value+8(FP), R1
 MOVW R1, (R0)
 RET

TEXT ·barrier(SB),NOSPLIT,$0-0
 DSB $15
 RET
