//go:build tamago && arm64 && de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// Firmware must grant EL1 architectural physical-counter access.
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

// Firmware retains the UART divisor, pins and clock. Bounded TX polling
// drops output if the UART is unavailable; it cannot wedge runtime deadlines.
TEXT ·uartPut(SB),NOSPLIT|NOFRAME,$0-1
 MOVD $0x10c02000, R1
 MOVBU c+0(FP), R0
 MOVD $100000, R3
uart_wait:
 MOVWU 0x14(R1), R2
 TBNZ $5, R2, uart_send
 SUB $1, R3
 CBNZ R3, uart_wait
 RET
uart_send:
 MOVW R0, (R1)
 DSB $15
 RET

TEXT ·halt(SB),NOSPLIT|NOFRAME,$0
halt_loop:
 WFE
 B halt_loop
