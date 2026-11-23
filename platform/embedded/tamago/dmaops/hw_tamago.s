//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// func readCTR() uint64
TEXT ·readCTR(SB),NOSPLIT|NOFRAME,$0-8
	WORD	$0xd53b0020 // MRS X0, CTR_EL0
	MOVD	R0, ret+0(FP)
	RET

// func dmbSY()
TEXT ·dmbSY(SB),NOSPLIT|NOFRAME,$0
	DMB	$15
	RET

// func dsbSY()
TEXT ·dsbSY(SB),NOSPLIT|NOFRAME,$0
	DSB	$15
	RET

// func dcCleanRange(start, end, stride uint64)
// Cleans cache lines in [start, end) to PoC (DC CVAC) and waits with DSB SY.
// Callers must pass line-aligned start < end and non-zero stride.
TEXT ·dcCleanRange(SB),NOSPLIT|NOFRAME,$0-24
	MOVD	start+0(FP), R0
	MOVD	end+8(FP), R1
	MOVD	stride+16(FP), R2
	DSB	$15
loop:
	CMP	R1, R0
	BHS	done
	WORD	$0xd50b7a20 // DC CVAC, X0
	ADD	R2, R0, R0
	B	loop
done:
	DSB	$15
	RET

// func dcInvalidateRange(start, end, stride uint64)
// Invalidates cache lines in [start, end) to PoC (DC IVAC) and waits with DSB SY.
// Callers must pass line-aligned start < end and non-zero stride.
TEXT ·dcInvalidateRange(SB),NOSPLIT|NOFRAME,$0-24
	MOVD	start+0(FP), R0
	MOVD	end+8(FP), R1
	MOVD	stride+16(FP), R2
	DSB	$15
loop:
	CMP	R1, R0
	BHS	done
	WORD	$0xd5087620 // DC IVAC, X0
	ADD	R2, R0, R0
	B	loop
done:
	DSB	$15
	RET

// func dcCleanInvalidateRange(start, end, stride uint64)
// Cleans and invalidates cache lines in [start, end) to PoC (DC CIVAC) and waits with DSB SY.
// Callers must pass line-aligned start < end and non-zero stride.
TEXT ·dcCleanInvalidateRange(SB),NOSPLIT|NOFRAME,$0-24
	MOVD	start+0(FP), R0
	MOVD	end+8(FP), R1
	MOVD	stride+16(FP), R2
	DSB	$15
loop:
	CMP	R1, R0
	BHS	done
	WORD	$0xd50b7e20 // DC CIVAC, X0
	ADD	R2, R0, R0
	B	loop
done:
	DSB	$15
	RET
