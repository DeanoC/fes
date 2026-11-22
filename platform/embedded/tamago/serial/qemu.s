//go:build tamago && arm64 && !de25nano

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// PL011 registers at 0x09000000 (QEMU virt UART0):
//   0x00 UARTDR, 0x18 UARTFR (bit 4 = RXFE), 0x2c UARTLCR_H (bit 4 = FEN),
//   0x38 UARTIMSC (bit 4 = RXIM, bit 6 = RTIM), 0x44 UARTICR.

TEXT ·readRaw(SB),NOSPLIT|NOFRAME,$0-2
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

TEXT ·enableHWIRQ(SB),NOSPLIT|NOFRAME,$0
	MOVD $0x09000000, R0
	// Clear FEN (1-byte RX threshold) so single-byte input fires RXIM
	// immediately on QEMU PL011; software rxRing buffers bursts.
	MOVWU 0x2c(R0), R1
	BIC $0x10, R1, R1
	MOVW R1, 0x2c(R0)
	// Clear all stale interrupts and unmask only RXIM (bit 4) and RTIM (bit 6)
	// so non-RX sources (e.g. TXIM) cannot wedge the level-triggered SPI.
	MOVD $0x7ff, R2
	MOVW R2, 0x44(R0)
	MOVD $0x50, R2
	MOVW R2, 0x38(R0)
	DSB $15
	RET

TEXT ·disableHWIRQ(SB),NOSPLIT|NOFRAME,$0
	MOVD $0x09000000, R0
	MOVW ZR, 0x38(R0)
	MOVD $0x7ff, R2
	MOVW R2, 0x44(R0)
	// Restore FIFO mode (FEN=1) for polling callers.
	MOVWU 0x2c(R0), R1
	ORR $0x10, R1, R1
	MOVW R1, 0x2c(R0)
	DSB $15
	RET

TEXT ·clearHWIRQ(SB),NOSPLIT|NOFRAME,$0
	MOVD $0x09000000, R0
	MOVD $0x50, R1
	MOVW R1, 0x44(R0)
	DSB $15
	RET
