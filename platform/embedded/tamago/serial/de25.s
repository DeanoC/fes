//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"
TEXT ·Read(SB),NOSPLIT|NOFRAME,$0-2
 MOVD $0x10c02000, R0
 MOVWU 0x14(R0), R1
 TBZ $0, R1, empty
 // Discard bytes with overrun/parity/framing/break errors.
 AND $0x1e, R1, R2
 MOVWU (R0), R1
 CBNZ R2, empty
 MOVB R1, ret+0(FP)
 MOVD $1, R1
 MOVB R1, ret1+1(FP)
 RET
empty:
 MOVB ZR, ret+0(FP)
 MOVB ZR, ret1+1(FP)
 RET
