//go:build tamago

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// The selected QEMU virt EL1 entry permits counter access. A hardware port
// must establish this independently with its firmware handoff.
TEXT ·CounterFrequency(SB),NOSPLIT|NOFRAME,$0-8
	MRS CNTFRQ_EL0, R0
	MOVD R0, ret+0(FP)
	RET

TEXT ·ExceptionLevel(SB),NOSPLIT|NOFRAME,$0-8
	MRS CurrentEL, R0
	LSR $2, R0, R0
	MOVD R0, ret+0(FP)
	RET

TEXT ·timerNanos(SB),NOSPLIT|NOFRAME,$0-8
	ISB $15
	MRS CNTPCT_EL0, R0
	MRS CNTFRQ_EL0, R2
	// quotient/remainder conversion avoids ticks * 1e9 overflowing early.
	UDIV R2, R0, R3
	MUL R2, R3, R4
	SUB R4, R0, R4
	MOVD $1000000000, R5
	MUL R5, R3, R3
	MUL R5, R4, R4
	UDIV R2, R4, R4
	ADD R4, R3, R0
	MOVD R0, ret+0(FP)
	RET

TEXT ·uartPut(SB),NOSPLIT|NOFRAME,$0-1
	MOVD $0x09000000, R1
	MOVBU c+0(FP), R0
uart_wait:
	MOVWU 0x18(R1), R2
	TBNZ $5, R2, uart_wait
	MOVW R0, (R1)
	RET

TEXT ·initializeUART(SB),NOSPLIT|NOFRAME,$0
	MOVD $0x09000000, R1
	MOVW ZR, 0x30(R1)
	MOVD $0x7ff, R0
	MOVW R0, 0x44(R1)
	// QEMU PL011: 24 MHz / 115200 baud, 8N1 with RX/TX FIFOs.
	MOVD $13, R0
	MOVW R0, 0x24(R1)
	MOVD $1, R0
	MOVW R0, 0x28(R1)
	MOVD $0x70, R0
	MOVW R0, 0x2c(R1)
	MOVD $0x301, R0
	MOVW R0, 0x30(R1)
	DSB $15
	RET
