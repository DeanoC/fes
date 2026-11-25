// SPDX-License-Identifier: MIT
// Native 6569 PAL, 6567R8 NTSC and 6567R56A early NTSC timing. VIC bus cycles are numbered from 1.
`ifndef C64_TIMING_VH
`define C64_TIMING_VH

`define C64_PAL_CPU_HZ 985_248
`define C64_PAL_TOD_HZ 50
`define C64_NTSC_CPU_HZ 1_022_730
`define C64_NTSC_TOD_HZ 60
// Timing selector: 0 = PAL, 1 = NTSC R8, 2 = early NTSC R56A.
`define C64_CYCLES_PER_LINE(ntsc) ((ntsc) == 2 ? 64 : ((ntsc) ? 65 : 63))
`define C64_LINES_PER_FRAME(ntsc) ((ntsc) == 2 ? 262 : ((ntsc) ? 263 : 312))
`define C64_CYCLE_BITS(ntsc) $clog2(`C64_CYCLES_PER_LINE(ntsc) + 1)

// Cycle 14 starts at beam X=0. The capture includes both borders.
`define C64_X_ZERO_CYCLE 14
`define C64_CAPTURE_FIRST_CYCLE 13
`define C64_CAPTURE_LAST_CYCLE 60
`define C64_PAL_CAPTURE_FIRST_LINE 16
`define C64_PAL_CAPTURE_LAST_LINE 287
`define C64_BORDER_TOP_25 51
`define C64_BORDER_BOTTOM_25 251
`define C64_BORDER_TOP_24 55
`define C64_BORDER_BOTTOM_24 247
`define C64_BORDER_LEFT_CYCLE 17
`define C64_BORDER_RIGHT_40 57
`define C64_BORDER_RIGHT_38 55

// First half reads graphics, second half reads the matrix on badlines.
`define C64_BADLINE_FIRST 48
`define C64_BADLINE_LAST 247
`define C64_MATRIX_FIRST 15
`define C64_MATRIX_LAST 54
`define C64_GLYPH_FIRST 16
`define C64_GLYPH_LAST 55
`define C64_ROW_UPDATE 58

// See VICE x64sc, src/viciisc/vicii-chip-model.c, cycle_tab_pal and
// cycle_tab_ntsc. Each sprite occupies a pointer cycle and the following
// cycle. On the 6567R8 sprite 3's pointer and row straddle the line end.
`define C64_SPRITE_POINTER_FIRST(ntsc) ((ntsc) ? 59 : 58)
`define C64_SPRITE_POINTER(ntsc,n) (1 + ((`C64_SPRITE_POINTER_FIRST(ntsc)-1 + 2*(n)) % `C64_CYCLES_PER_LINE(ntsc)))
`define C64_SPRITE_ROW(ntsc,n) (1 + (`C64_SPRITE_POINTER(ntsc,n) % `C64_CYCLES_PER_LINE(ntsc)))
`define C64_SPRITE_DMA_FIRST(ntsc) ((ntsc) ? 56 : 55)
`define C64_SPRITE_EXPAND_CYCLE 56

`endif
