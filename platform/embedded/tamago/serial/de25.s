//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// Agilex 5 DesignWare APB UART (16550-compatible, 32-bit word-aligned):
//   0x00 RBR/THR, 0x04 IER (bit 0 = ERBFI), 0x08 IIR, 0x14 LSR, 0x7c USR.

TEXT ·read(SB),NOSPLIT|NOFRAME,$0-10
	MOVD base+0(FP), R0
	MOVWU 0x14(R0), R1
	TBZ $0, R1, empty
	// Discard bytes with overrun/parity/framing/break errors.
	AND $0x1e, R1, R2
	MOVWU (R0), R1
	CBNZ R2, empty
	MOVB R1, ret+8(FP)
	MOVD $1, R1
	MOVB R1, ret1+9(FP)
	RET
empty:
	MOVB ZR, ret+8(FP)
	MOVB ZR, ret1+9(FP)
	RET

TEXT ·dwEnableRXIRQ(SB),NOSPLIT|NOFRAME,$0-8
	MOVD base+0(FP), R0
	MOVWU 0x04(R0), R1
	ORR $1, R1, R1
	MOVW R1, 0x04(R0)
	DSB $15
	RET

TEXT ·dwDisableRXIRQ(SB),NOSPLIT|NOFRAME,$0-8
	MOVD base+0(FP), R0
	MOVWU 0x04(R0), R1
	BIC $1, R1, R1
	MOVW R1, 0x04(R0)
	DSB $15
	RET

TEXT ·dwClearRXIRQ(SB),NOSPLIT|NOFRAME,$0-8
	MOVD base+0(FP), R0
	MOVWU 0x08(R0), R1
	AND $0x0f, R1, R1
	CMP $7, R1
	BNE done
	// DW APB busy-detect interrupt is cleared by reading USR (0x7c).
	// Do not read LSR here so read() still sees per-byte RX error flags.
	MOVWU 0x7c(R0), R2
done:
	RET
