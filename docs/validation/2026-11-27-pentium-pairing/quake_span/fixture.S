/* Freestanding Linux i386 caller for id Software's D_DrawSpans8. */
.data
.align 4
span: .long 0, 0, SPAN_WIDTH, 0
.globl d_scantable, fp_8, fp_64k, float_point5, Float2ToThe31nd, FloatMinus2ToThe31nd
d_scantable: .long 0
fp_8: .float 8
fp_64k: .float 65536
float_point5: .float 0.5
Float2ToThe31nd: .float 2147483648
FloatMinus2ToThe31nd: .float -2147483648
texture:
.skip 128
row1:
.set texel, 1
.rept 128
.byte texel
.set texel, texel + 1
.endr

.bss
pixels: .skip 128

.text
.globl _start, span_done
_start:
    fninit
    movl $0x3f800000, d_ziorigin      /* 1/z = 1 */
    movl $0x3f800000, d_sdivzstepu    /* advance one texel in S per screen pixel */
    movl $0x3f800000, d_tdivzorigin  /* sample row 1, above the low clamp */
    movl $0x007fffff, bbextents
    movl $0x0001ffff, bbextentt
    movl $128, cachewidth
    movl $texture, cacheblock
    movl $pixels, d_viewbuffer
    pushl $span
    call D_DrawSpans8
span_done:
    xorl %ecx, %ecx
check_pixel:
    movb row1(%ecx), %al
    cmpb %al, pixels(%ecx)
    jne failed
    incl %ecx
    cmpl $SPAN_WIDTH, %ecx
    jb check_pixel
    cmpb $0, pixels(%ecx)            /* the first untouched pixel */
    jne failed
    xorl %ebx, %ebx
    jmp exit
failed:
    movl $1, %ebx
exit:
    movl $1, %eax
linux_exit:
    int $0x80

/* Unused entries of d_varsa.s's other renderers' jump tables. */
.irp name,DP_1x1,DP_2x2,DP_3x3,DP_4x4,Entry2_16,Entry3_16,Entry4_16,Entry5_16,Entry6_16,Entry7_16,Entry8_16,Entry9_16,Entry10_16,Entry11_16,Entry12_16,Entry13_16,Entry14_16,Entry15_16,Entry16_16,Spr8Entry2_8,Spr8Entry3_8,Spr8Entry4_8,Spr8Entry5_8,Spr8Entry6_8,Spr8Entry7_8,Spr8Entry8_8
.globl \name
\name:
.endr
    ud2
