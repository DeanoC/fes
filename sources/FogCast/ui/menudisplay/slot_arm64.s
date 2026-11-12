#include "textflag.h"

// Each iteration converts eight RGBA pixels, preserving alpha. Loads and
// stores accept arbitrary byte alignment and stay within complete blocks.
// This is a Go ABI leaf so a revoked mapping can panic back to window.guard.
TEXT ·writeRowNEON(SB), NOSPLIT, $0-56
	MOVD dst_base+0(FP), R0
	MOVD src_base+24(FP), R1
	MOVD src_len+32(FP), R3
	MOVD table+48(FP), R2
	CMP $32, R3
	BLT done
	VLD1 (R2), [V2.B16]
loop:
	VLD1.P 32(R1), [V0.B16, V1.B16]
	VTBL V2.B16, [V0.B16], V0.B16
	VTBL V2.B16, [V1.B16], V1.B16
	VST1.P [V0.B16, V1.B16], 32(R0)
	SUB $32, R3
	CMP $32, R3
	BGE loop
done:
	RET
