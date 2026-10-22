#include "textflag.h"

// ARMv7 NEON, gated by cpu.ARM.HasNEON in slot_arm.go. Go's ARM assembler
// does not expose these NEON mnemonics, so their encodings are WORDs. Only
// d0-d8 are used, which asynchronous preemption saves.
// Each iteration turns eight RGBA pixels into B, G, R, A words: one table
// lookup per two pixels picks B, G, R and A. Accesses are unaligned-safe.
TEXT ·writeRowNEON(SB), NOSPLIT, $0-28
	MOVW dst_base+0(FP), R0
	MOVW src_base+12(FP), R1
	MOVW src_len+16(FP), R3
	MOVW table+24(FP), R2
	CMP $32, R3
	BLT done
	WORD $0xf422870f // vld1.8 {d8}, [r2]
loop:
	WORD $0xf421020d // vld1.8 {d0-d3}, [r1]!
	WORD $0xf3b04808 // vtbl.8 d4, {d0}, d8
	WORD $0xf3b15808 // vtbl.8 d5, {d1}, d8
	WORD $0xf3b26808 // vtbl.8 d6, {d2}, d8
	WORD $0xf3b37808 // vtbl.8 d7, {d3}, d8
	WORD $0xf400420d // vst1.8 {d4-d7}, [r0]!
	SUB $32, R3
	CMP $32, R3
	BGE loop
done:
	RET
