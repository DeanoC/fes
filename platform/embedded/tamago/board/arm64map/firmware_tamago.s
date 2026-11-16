//go:build tamago && arm64

// SPDX-License-Identifier: GPL-3.0-or-later
#include "textflag.h"

// Early primary-CPU operation, before devices or Go package initialization.
// No target OCRAM loads/stores, stack frame, calls, allocation or page tables.
// R3..R7 retain CPU state, R12 RAM, R17 the caller's PAR. The only stores are
// the attempt latch, exactly 16 owned PTEs, and the ABI return slot.
TEXT ·ExcludeFirmwareRAM(SB),NOSPLIT|NOFRAME,$0-9
	MRS CurrentEL, R0
	CMP $4, R0
	BNE wrong_el
	MRS PAR_EL1, R17
	MOVD $·firmwareExclusionAttempted(SB), R16
	MOVWU (R16), R0
	CBNZ R0, reject
	MOVD $1, R0
	MOVW R0, (R16)
	MRS SCTLR_EL1, R3
	MOVD $0x1005, R1
	AND R3, R1, R0
	CMP R1, R0
	BNE reject
	MOVD $0x3080002, R1 // EE/E0E/WXN/A must match the existing mapper
	TST R1, R3
	BNE reject
	MRS TCR_EL1, R4
	MOVD $0x200003519, R1
	CMP R1, R4
	BNE reject
	MRS MAIR_EL1, R6
	MOVD $0xff00, R1
	CMP R1, R6
	BNE reject
	MRS DAIF, R0
	CMP $0x3c0, R0
	BNE reject
	MRS MPIDR_EL1, R7
	MOVD $0xff00ffffff, R1
	AND R7, R1, R0
	CBZ R0, primary
	CMP $0x200, R0
	BNE reject
primary:
	MOVD ramBase+0(FP), R12
	MOVD $0x40000000, R1
	CMP R1, R12
	BLO reject
	MOVD $0xc0000000, R1
	CMP R1, R12
	BHS reject
	MOVD $0x1fffff, R1
	TST R1, R12
	BNE reject
	MRS TTBR0_EL1, R5
	ADD $0x4000, R12, R1
	CMP R1, R5
	BNE reject

	// Prove read/write identity WB translations for all reserved table pages
	// before dereferencing their contents. AT never accesses the target page.
	ADD $0x4000, R12, R8
	ADD $0x8000, R12, R9
tables:
	MOVD R8, R0
	DSB $15
	WORD $0xd5087800 // AT S1E1R, X0
	ISB $15
	MRS PAR_EL1, R1
	TST $1, R1
	BNE reject
	LSR $56, R1, R2
	CMP $255, R2
	BNE reject
	MOVD $0x000000fffffff000, R2
	AND R1, R2, R2
	CMP R8, R2
	BNE reject
	LSR $7, R1, R2
	AND $3, R2
	CMP $3, R2
	BNE reject
	MOVD R8, R0
	WORD $0xd5087820 // AT S1E1W, X0
	ISB $15
	MRS PAR_EL1, R1
	TST $1, R1
	BNE reject
	LSR $56, R1, R2
	CMP $255, R2
	BNE reject
	MOVD $0x000000fffffff000, R2
	AND R1, R2, R2
	CMP R8, R2
	BNE reject
	LSR $7, R1, R2
	AND $3, R2
	CMP $3, R2
	BNE reject
	ADD $4096, R8
	CMP R9, R8
	BLO tables
	MOVD (R5), R0
	ADD $0x5000, R12, R1
	ORR $3, R1
	CMP R1, R0
	BNE reject
	ADD $0x5000, R12, R8
	MOVD (R8), R0
	ADD $0x7000, R12, R10
	ORR $3, R10, R1
	CMP R1, R0
	BNE reject

	ADD $0x280, R10, R8 // PTEs for [0x50000,0x60000), fixed 16 pages
	ADD $0x80, R8, R9
	MOVD $0x50000, R13
	MOVD $0x60000000000603, R11
preflight:
	MOVD (R8), R0
	ORR R13, R11, R1
	CMP R1, R0
	BNE reject
	ADD $8, R8
	ADD $4096, R13
	CMP R9, R8
	BLO preflight
	ADD $0x280, R10, R8
remove:
	MOVD ZR, (R8)
	MOVD R8, R0
	WORD $0xd50b7a20 // DC CVAC, X0
	ADD $8, R8
	CMP R9, R8
	BLO remove
	DSB $15
	TLBI VMALLE1
	DSB $15
	ISB $15

	ADD $0x280, R10, R8
	MOVD $0x50000, R13
verify:
	MOVD (R8), R0
	CBNZ R0, reject
	MOVD R13, R14
edge:
	MOVD R14, R0
	WORD $0xd5087800 // AT S1E1R, X0
	ISB $15
	MRS PAR_EL1, R1
	MOVD $0x37f, R2 // reject stage-2/table-walk faults as well
	AND R2, R1
	CMP $15, R1 // F=1, FST=0b000111: level-3 translation fault
	BNE reject
	MOVD R14, R0
	WORD $0xd5087820 // AT S1E1W, X0
	ISB $15
	MRS PAR_EL1, R1
	MOVD $0x37f, R2
	AND R2, R1
	CMP $15, R1
	BNE reject
	CMP R13, R14
	BNE next
	ADD $4095, R14
	B edge
next:
	ADD $8, R8
	ADD $4096, R13
	CMP R9, R8
	BLO verify
	MOVD (R5), R0
	ADD $0x5000, R12, R1
	ORR $3, R1
	CMP R1, R0
	BNE reject
	ADD $0x5000, R12, R8
	MOVD (R8), R0
	ORR $3, R10, R1
	CMP R1, R0
	BNE reject
	MRS SCTLR_EL1, R0
	CMP R3, R0
	BNE reject
	MRS TCR_EL1, R0
	CMP R4, R0
	BNE reject
	MRS TTBR0_EL1, R0
	CMP R5, R0
	BNE reject
	MRS MAIR_EL1, R0
	CMP R6, R0
	BNE reject
	MRS MPIDR_EL1, R0
	CMP R7, R0
	BNE reject
	MRS DAIF, R0
	CMP $0x3c0, R0
	BNE reject
	MOVD $1, R0
	B done
reject:
	MOVD $0, R0
done:
	MSR R17, PAR_EL1
	ISB $15
	MOVB R0, ret+8(FP)
	RET
wrong_el:
	MOVB ZR, ret+8(FP)
	RET
