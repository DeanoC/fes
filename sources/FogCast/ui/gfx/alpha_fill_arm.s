#include "textflag.h"

// ARMv7 NEON, gated by cpu.ARM.HasNEON in alpha_fill_arm.go. Go's ARM
// assembler does not expose these NEON mnemonics, so their encodings are WORDs.
// q0 holds repeated biased source contributions; d30 holds inverse alpha.
// q4-q7 hold widened channel sums. No FPSCR change.
//
// For 0 <= x <= 65025, y=x+1 gives floor(x/255) = (y + (y >> 8)) >> 8.
// All intermediates fit unsigned 16-bit lanes, including destination alpha.
// Each iteration handles eight RGBA pixels with unaligned-safe accesses.
TEXT ·fillAlphaNEON(SB), NOSPLIT, $0-20
	MOVW row_base+0(FP), R0
	MOVW row_len+4(FP), R1
	MOVW source+12(FP), R2
	MOVW inverse+16(FP), R3
	CMP $32, R1
	BLT done
	WORD $0xf4220a4f // vld1.16 {d0, d1}, [r2]
	WORD $0xeece3b90 // vdup.8 d30, r3
loop:
	WORD $0xf420420f // vld1.8 {d4, d5, d6, d7}, [r0]
	WORD $0xf3848c2e // vmull.u8 q4, d4, d30
	WORD $0xf385ac2e // vmull.u8 q5, d5, d30
	WORD $0xf386cc2e // vmull.u8 q6, d6, d30
	WORD $0xf387ec2e // vmull.u8 q7, d7, d30
	WORD $0xf2188840 // vadd.i16 q4, q4, q0
	WORD $0xf21aa840 // vadd.i16 q5, q5, q0
	WORD $0xf21cc840 // vadd.i16 q6, q6, q0
	WORD $0xf21ee840 // vadd.i16 q7, q7, q0
	WORD $0xf3988158 // vsra.u16 q4, q4, #8
	WORD $0xf398a15a // vsra.u16 q5, q5, #8
	WORD $0xf398c15c // vsra.u16 q6, q6, #8
	WORD $0xf398e15e // vsra.u16 q7, q7, #8
	WORD $0xf2884818 // vshrn.i16 d4, q4, #8
	WORD $0xf288581a // vshrn.i16 d5, q5, #8
	WORD $0xf288681c // vshrn.i16 d6, q6, #8
	WORD $0xf288781e // vshrn.i16 d7, q7, #8
	WORD $0xf400420d // vst1.8 {d4, d5, d6, d7}, [r0]!
	SUB $32, R1
	CMP $32, R1
	BGE loop
done:
	RET
