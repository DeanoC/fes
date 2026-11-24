//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// func hasGICv3() bool
TEXT ·hasGICv3(SB),NOSPLIT|NOFRAME,$0-1
	MRS	ID_AA64PFR0_EL1, R0
	LSR	$24, R0, R0
	AND	$0xf, R0, R0
	CMP	$0, R0
	CSET	NE, R0
	MOVB	R0, ret+0(FP)
	RET

// func readMPIDR() uint64
TEXT ·readMPIDR(SB),NOSPLIT|NOFRAME,$0-8
	ISB	$15
	MRS	MPIDR_EL1, R0
	MOVD	R0, ret+0(FP)
	RET

// func read32(addr uint64) uint32
TEXT ·read32(SB),NOSPLIT|NOFRAME,$0-12
	MOVD	addr+0(FP), R0
	MOVWU	(R0), R1
	DSB	$15
	MOVW	R1, ret+8(FP)
	RET

// func write32(addr uint64, val uint32)
TEXT ·write32(SB),NOSPLIT|NOFRAME,$0-12
	MOVD	addr+0(FP), R0
	MOVWU	val+8(FP), R1
	DSB	$15
	MOVW	R1, (R0)
	DSB	$15
	RET

// func read64(addr uint64) uint64
TEXT ·read64(SB),NOSPLIT|NOFRAME,$0-16
	MOVD	addr+0(FP), R0
	MOVD	(R0), R1
	DSB	$15
	MOVD	R1, ret+8(FP)
	RET

// func write64(addr uint64, val uint64)
TEXT ·write64(SB),NOSPLIT|NOFRAME,$0-16
	MOVD	addr+0(FP), R0
	MOVD	val+8(FP), R1
	DSB	$15
	MOVD	R1, (R0)
	DSB	$15
	RET

// func initCPUInterface(pmr uint8)
TEXT ·initCPUInterface(SB),NOSPLIT|NOFRAME,$0-1
	// Enable GICv3 system register interface at EL1.
	MOVD	$1, R0
	MSR	R0, ICC_SRE_EL1
	ISB	$15
	// Single-write EOI + deactivate (EOImode = 0).
	MRS	ICC_CTLR_EL1, R0
	BIC	$2, R0, R0
	MSR	R0, ICC_CTLR_EL1
	ISB	$15
	// Unmask priorities numerically below pmr; flat priority grouping.
	MOVBU	pmr+0(FP), R0
	MSR	R0, ICC_PMR_EL1
	MSR	ZR, ICC_BPR1_EL1
	ISB	$15
	// Enable Non-Secure Group 1 interrupts.
	MOVD	$1, R0
	MSR	R0, ICC_IGRPEN1_EL1
	ISB	$15
	RET

// func ackIRQ() uint32
TEXT ·ackIRQ(SB),NOSPLIT|NOFRAME,$0-4
	ISB	$15
	MRS	ICC_IAR1_EL1, R0
	DSB	$15
	MOVW	R0, ret+0(FP)
	RET

// func endIRQ(id uint32)
TEXT ·endIRQ(SB),NOSPLIT|NOFRAME,$0-4
	MOVWU	id+0(FP), R0
	MSR	R0, ICC_EOIR1_EL1
	ISB	$15
	RET

// func sendSGI(val uint64)
TEXT ·sendSGI(SB),NOSPLIT|NOFRAME,$0-8
	MOVD	val+0(FP), R0
	DSB	$15
	MSR	R0, ICC_SGI1R_EL1
	ISB	$15
	RET

// func EnableCPUIRQ()
TEXT ·EnableCPUIRQ(SB),NOSPLIT|NOFRAME,$0
	MSR	$0b0010, DAIFClr
	ISB	$15
	RET

// func DisableCPUIRQ()
TEXT ·DisableCPUIRQ(SB),NOSPLIT|NOFRAME,$0
	MSR	$0b0010, DAIFSet
	ISB	$15
	RET

// func ArmPhysicalTimer(ticks uint32)
TEXT ·ArmPhysicalTimer(SB),NOSPLIT|NOFRAME,$0-4
	MOVWU	ticks+0(FP), R0
	MSR	R0, CNTP_TVAL_EL0
	MOVD	$1, R1
	MSR	R1, CNTP_CTL_EL0
	ISB	$15
	RET

// func DisarmPhysicalTimer()
TEXT ·DisarmPhysicalTimer(SB),NOSPLIT|NOFRAME,$0
	MOVD	$2, R1
	MSR	R1, CNTP_CTL_EL0
	DSB	$15
	ISB	$15
	RET

// func WaitForInterrupt()
TEXT ·WaitForInterrupt(SB),NOSPLIT|NOFRAME,$0
	DSB	$15
	WFI
	ISB	$15
	RET

// func counterFreq() uint64
TEXT ·counterFreq(SB),NOSPLIT|NOFRAME,$0-8
	MRS	CNTFRQ_EL0, R0
	MOVD	R0, ret+0(FP)
	RET

// func irqEnabled() bool
TEXT ·irqEnabled(SB),NOSPLIT|NOFRAME,$0-1
	MRS	DAIF, R0
	TST	$0x80, R0
	CSET	EQ, R0
	MOVB	R0, ret+0(FP)
	RET

// func maskIRQ() bool
TEXT ·maskIRQ(SB),NOSPLIT|NOFRAME,$0-1
	MRS	DAIF, R0
	MSR	$0b0010, DAIFSet
	ISB	$15
	TST	$0x80, R0
	CSET	EQ, R0
	MOVB	R0, ret+0(FP)
	RET
