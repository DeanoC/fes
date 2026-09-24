# Coleco TMS9918 Text and Multicolor modes

**Status:** Approved
**Base:** FES `663ce6d582dfe8c5260985ca1f858fcee44f2bae` (`origin/main`)
**Owner:** `sources/misteross`
**First-slice scope:** Add the two documented TMS9918 display modes missing from the shared VDP renderer. NTSC clock and raster timing are a separate follow-up.

## Outcome

The shared TMS9918 renderer will display all four documented modes: Graphics I,
Graphics II, Text and Multicolor. Existing Graphics I/II output, VRAM access,
color indices, sprite behavior in graphics modes, status reads, and the current
logical raster interface remain intact.

The first slice adds Text and Multicolor rendering in
`sources/misteross/cores/fes-common/rtl/coleco_vdp.sv`. The module is used by
ColecoVision, SG-1000 and the SMS legacy TMS mode, so all three paths receive
the new mode behavior and must retain their existing simulations.

## Mode selection

Decode the three documented mode bits as M1 = R1[4], M2 = R1[3] and M3 =
R0[1]. The defined combinations are:

| M1 M2 M3 | Mode |
| --- | --- |
| `000` | Graphics I |
| `001` | Graphics II |
| `010` | Multicolor |
| `100` | Text |

The other four combinations are not documented display modes and are outside
this change's compatibility claim. The implementation should give them a
deterministic backdrop-only result rather than accidentally treating them as a
valid new mode.

## Text mode

- Render a 40-column by 24-row name table. Each name selects an 8-byte glyph in
  the pattern generator table, using the existing R2 and R4 bases.
- Each glyph is 6 pixels wide by 8 pixels high. The high six bits of each
  pattern row select foreground or background; the low two bits do not appear.
- Center the 240-pixel text area in the existing 256-pixel logical row, leaving
  eight backdrop pixels on each side.
- Use R7's upper nibble for foreground and lower nibble for background. Preserve
  the existing four-bit color path and color-zero/backdrop behavior.
- Suppress sprite pixels in Text mode. Do not clear already latched status
  flags on a mode change; status reads retain their existing acknowledge
  behavior. The manual does not specify collision/overflow behavior while Text
  mode is active, so that detail is not part of this slice's contract.

## Multicolor mode

- Keep the existing 32-by-24 name table and use each name to select an
  eight-byte pattern-generator segment through R2 and R4.
- Each 8-by-8 pattern cell displays four 4-by-4 color blocks. The two bytes
  selected for a name-table row provide upper-left, upper-right, lower-left and
  lower-right colors, with each nibble selecting one TMS color.
- Select the byte pair from the screen tile row: successive tile rows use byte
  pairs 0/1, 2/3, 4/5 and 6/7, then repeat that sequence. Within the tile, Y
  selects the upper or lower 4-pixel block and X selects the left or right
  nibble.
- Preserve backdrop resolution for color zero and keep the sprite plane active,
  matching the documented Multicolor behavior.

## Interfaces and boundaries

No module ports, FES mailbox/package interfaces, VRAM size, CPU port behavior,
or 256-by-192 logical raster coordinates change. The existing 720p HDMI shell
and framebuffer remain in place. BIOS and ROM linking remain unchanged.

This slice does not change CPU frequency, `raster_ce`, horizontal or vertical
totals, VBlank cadence, the fixed 720p output timing, or sprite evaluation's
system-clock schedule. In particular, it does not claim cycle-accurate NTSC
timing.

The timing follow-up is a separate design. It will compare the current shared
TMS paths—including the SMS Mode 4 path and PSG cadence—before changing their
clock enables. It must budget the serial sprite renderer against the shorter
line interval and verify the target TMS9918 NTSC cadence rather than changing
the system PLL or the display shell by assumption.

## Verification

The focused VDP fixtures will use explicit VRAM bytes and literal pixel
expectations for both new modes. They will cover Text name/pattern addressing,
six visible glyph bits, edge margins, R7 colors and sprite suppression; and
Multicolor row-pair selection, all four nibbles, color-zero backdrop resolution
and sprite overlay. Existing Graphics I/II, buffered data-port and status
regressions remain required.

Run the default and registered-memory Coleco graphics simulations, then the
affected Coleco, SG-1000 and SMS legacy-VDP simulation cases. Rebuild the
Coleco OSS package and require its normal timing/resource checks before treating
the RTL change as integration-ready. Hardware acceptance is separate and is
not implied by simulation or routing.

## References

- Texas Instruments, [TMS9918A/TMS9928A/TMS9929A Video Display Processors Data
  Manual](https://computers.baffa.tec.br/pages/datasheet/TMS9918A_TMS9928A_TMS9929A_Video_Display_Processors_Data_Manual_Nov82.pdf):
  mode selection, Text and Multicolor table layouts, sprite availability, and
  NTSC clock/raster reference for the separate timing slice.
- Coleco, [ColecoVision Technical
  Manual](https://www.colecovisionzone.com/downloads/cv_Technical_Manual.pdf):
  console hardware and VDP clock context.
