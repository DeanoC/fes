// SPDX-License-Identifier: MIT
// Native PAL machine timing. VIC bus cycles are numbered from 1.
`ifndef C64_PAL_TIMING_VH
`define C64_PAL_TIMING_VH

`define C64_PAL_CPU_HZ 985_248
`define C64_PAL_TOD_HZ 50
`define C64_PAL_CYCLES_PER_LINE 63
`define C64_PAL_LINES_PER_FRAME 312
`define C64_PAL_CYCLE_BITS $clog2(`C64_PAL_CYCLES_PER_LINE + 1)

// Cycle 14 starts at beam X=0. The capture includes both borders.
`define C64_PAL_X_ZERO_CYCLE 14
`define C64_PAL_CAPTURE_FIRST_CYCLE 13
`define C64_PAL_CAPTURE_LAST_CYCLE 60
`define C64_PAL_CAPTURE_FIRST_LINE 16
`define C64_PAL_CAPTURE_LAST_LINE 287
`define C64_PAL_BORDER_TOP_25 51
`define C64_PAL_BORDER_BOTTOM_25 251
`define C64_PAL_BORDER_TOP_24 55
`define C64_PAL_BORDER_BOTTOM_24 247
`define C64_PAL_BORDER_LEFT_CYCLE 17
`define C64_PAL_BORDER_RIGHT_40 57
`define C64_PAL_BORDER_RIGHT_38 55

// First half reads graphics, second half reads the matrix on badlines.
`define C64_PAL_BADLINE_FIRST 48
`define C64_PAL_BADLINE_LAST 247
`define C64_PAL_MATRIX_FIRST 15
`define C64_PAL_MATRIX_LAST 54
`define C64_PAL_GLYPH_FIRST 16
`define C64_PAL_GLYPH_LAST 55
`define C64_PAL_ROW_UPDATE 58

// Two cycles per sprite. Slots 0..2 finish the line, 3..7 start the next.
`define C64_PAL_SPRITE_POINTER(n) (((n) < 3) ? (58 + 2*(n)) : (1 + 2*((n)-3)))
`define C64_PAL_SPRITE_DMA_FIRST 55
`define C64_PAL_SPRITE_DMA_LAST 56

`endif
