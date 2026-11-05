# pc386 Ultima VII view roll and the VGA BIOS scroll, 5 November 2026

Issue [#1493](https://github.com/DeanoC/fes/issues/1493). Kit B run `w1`, with
#1483's CPU status ring and guest memory read live through the HPS (busybox
`devmem` on the DDR window, read-only), the ring log of #1485's run `u1`, and
the machine bench.

**Result.** The fault is in the VGA BIOS. The game makes a valid BIOS call,
and the BIOS never returns from it.

- **Cause.** After Iolo's "I shall accompany thee.", Ultima VII: The Black Gate
  calls INT 10h with AX=0701h, BH=0, CX=0000h and DX=1928h in mode 13h. That
  is a scroll down by one row of the whole screen, from row 0. The upstream
  LGPL VGABIOS (`biosfn_scroll`, v0_9d and current master) runs its graphics
  scroll-down loops as `for(i=rlr;i>=rul;i--)` with an unsigned 16-bit `i`.
  With `rul` = 0 that never ends: the row wraps to FFFFh, and the copy keeps
  moving video memory.
- **What that does on the kit.** The copy loop is the rolling picture. It
  keeps shifting mode 13h VRAM by character rows, with the blank fill rows as
  black bands, so frames keep changing. The game never gets control back.
  Timer and mouse interrupts still run, and clicks do nothing. The issue's
  reading that "the game is alive" was wrong: the BIOS loop is what changes
  the frames.
- **Fix.** Each graphics scroll-down loop (planar, CGA, linear) stops once its
  row has passed row 0, before using that row. The text loop already had a
  check like this (`if (i>rlr) break;`), placed at the end of its body. The
  local change is recorded in `vgabios/source.json` and marked in
  `vgabios.c`. The VGA BIOS image is `e0fae3ec…` (was `69401dc8…`).

## Evidence

| What | Where | Value |
| --- | --- | --- |
| CPU from the roll on, run `u1` ring | 10:13:40–10:14:34, 561 of 561 samples in the last minute | C000:4293–429A, the `rep movsw` of `_vgamem_copy_lin` (VGA BIOS image `69401dc8…`, offset 4297h) |
| Same, run `w1` | from 19:55:30 to the end, 20:54 (every sample from 19:56) | C000:4297, last vector 08h |
| Retired count while there | `u1` ring | about 15,000 a sample (70 ms), so the CPU keeps running inside the BIOS |
| Video mode | `w1`, BDA 449h | 13h, 40 columns, rows 24, character height 8 |
| The call | `w1`, saved registers at linear 60318h | AX 0701h, BX 004Ah, CX 0000h, DX 1928h, DS 5043h |
| The caller's segment | `w1`, IVT | INT 23h = 5043:05E8, U7's data segment |
| INT 10h chain | `w1`, IVT | C897:0434 (CuteMouse), then the VGA BIOS |

## Bench

`scripts/sim_fes_pc386.py --suite scroll` runs `SCROLLT.COM`
([source](../../../sources/misteross/cores/fes-pc386/sim/scroll_test.asm)) at
the FreeDOS prompt. It runs INT 10h AH=07h from row 0 in modes 13h, 12h and
04h. Each mode must move a marker one character row down and leave the top
row blank.

| BIOS | CPU | Result |
| --- | --- | --- |
| `69401dc8…` (upstream) | Bochs | never returns; no check printed; FAIL at the limit |
| `e0fae3ec…` (this PR) | Bochs | mode13 2/2, mode12 2/2, mode04 2/2, DONE ok |
| `e0fae3ec…` (this PR) | RTL (`fes_i386_cpu`) | mode13 2/2, mode12 2/2, mode04 2/2, DONE ok; 0 oracle steps, 0 stops |

Bochs's own VGA is not a reference here, because the bench runs the same
VGABIOS. The reference is the IBM BIOS behaviour: the call scrolls and
returns.

## Seal

Quartus 17.0.2, seed 1, from a clean checkout of `f5974726`: package
`9c1f36b7df62472ee1f7d3410a3ae07b87f766b8e5fd02c273feecca56626ee8`. Every
timing analysis passed. The worst slacks over the four corners: setup
1.254 ns, hold 0.075 ns, recovery 7.334 ns, removal 0.383 ns, minimum pulse
width 1.122 ns.

## Inputs

- Kit B, core-only on baseline software `a8cd30c4`: no install, image change
  or reboot. The claim and release are in Kit B's status file.
- Package `01e1eebb…` (#1483's status-ring seal of main `ee7ea9bb4`). Disk
  from #1485's `v1` (`v-u7bg.img`), The Black Gate from Deano's eXoDOS 6.04
  zips. Run `w1` replayed `v1`'s commands with `v1`'s own timing, after a
  first replay without operator pauses fell out of sync in the intro.

Nothing from the game is committed: no frames, no bytes. Frames and memory
dumps stay on Powerboat in `~/fes-scratch/u7roll/runs/w1`.
