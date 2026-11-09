# PC/XT CGA font: Rogue's map (fes#1537)

Run on 8 November 2026 on Kit B.

## Cause

On `fes.pcxt-dos`, Rogue drew only its text, status line, player and monsters.
The package's CGA character ROM came from GLaBIOS's INT 10h graphics table,
which holds only characters 00h–7Fh. Codes 80h–FFh were blank. MAME's text
memory at the same point
([`mame-rogue-cells.json`](mame-rogue-cells.json)) shows that Rogue draws the
whole map from that upper half:

| Code | Attribute | Glyph | Use |
| --- | --- | --- | --- |
| BAh BBh BCh C8h C9h CDh | 06h (brown) | ║ ╗ ╝ ╚ ╔ ═ | walls |
| CEh | 06h | ╬ | doors |
| B1h | 07h | ▒ | passages |
| FAh | 0Ah (light green) | · | floor |

The status line and the player (01h ☺) are below 80h, so they drew.

The fix gives the character ROM all 256 CP437 characters. The source is the
public-domain `vgafont8` table, already vendored for pc386 in VGABios 0.9d, now
`cores/fes-pcxt/firmware/cga-font-8x8.bin`. Characters 00h–7Fh are
byte-identical to the GLaBIOS table, so BIOS and DOS text is unchanged.
`tests/test_pcxt_cga_font.py` fails on the old font and passes on the new one.

## Package

- Source commit `9036d360dfd2fa9297d4b6ac8e70c641e1362cee`.
- Quartus 17.0.2 Lite, seeds 1–3, build `dfee2badd74422aff702e2bc95399f88`.
- Package `4dd6d431b1abb44274bcabdb7734fe7c1686394af440eeb146d4dd6987655db6`.
- Archive `4ef787f74e2a247572d980815503d07746448ce11f168be2132dcd99175deccf`.
- RBF `4d89961f1db5b3e4522cabfd19473da37ece0566474b5136a88de8b76ad1eba0`. The
  independent repeat build produced the same RBF.
- Internal timing passed on all three seeds. These are the worst slow-corner
  Fmax figures across the seeds, each against its required clock:

  | Clock | Worst Fmax | Required |
  | --- | --- | --- |
  | Memory | 107.33 MHz | 100 MHz |
  | Video | 87.65 MHz | 74.25 MHz |
  | Audio | 228.31 MHz | 12.288 MHz |

## Kit B run f1

The run was core-only on baseline software `a8cd30c4`, image `f904961e`, with
no install, no image change and no reboot. It used the same `rogue.img` and
keystrokes as the #1529 qualification run s3, under new library game IDs.

- **Rogue.** After `ROGUE` and the name, the start room, its ╬ door and
  floor are drawn. Walls are brown double lines, the floor is green dots and
  the player is ☺. They match the code and attribute classes in the table
  above. Ctrl+R redraws the room
  ([`kit-b-rogue-redraw.png`](kit-b-rogue-redraw.png); MAME:
  [`mame-rogue-redraw.png`](mame-rogue-redraw.png)). Rogue seeds its dungeon
  from the clock, so the layouts differ.
- **CGASTAT probe.** The run used `CGASTAT.COM` from the #1537 issue, twice
  ([`kit-b-cgastat.png`](kit-b-cgastat.png)):

  | | bit0 transitions | bit3 transitions | bit0-set reads | bit3-set reads | ticks |
  | --- | --- | --- | --- | --- | --- |
  | f1 (this package) | 6A5A / 778D | 013A / 013A | 7081 / 771F | 0DCC / 0DB9 | 0030 / 0030 |
  | s3 (package 87fd8149) | 6D16 / 6964 | 013A / 013A | 71F0 / 7003 | 0DCE / 0DB7 | 0030 / 002F |

  The status port did not cause this defect, and the fix does not change it.
  Port 3DAh behaves as a real CGA does:
  - **Bit 0** is "display enable inactive". GLaBIOS programs the 80×25
    6845 to 114 characters per line, 80 of them displayed, and 262 lines, 200
    of them displayed. So 1 − (80/114)(200/262) = 46.4% of the time is blank.
    The kit reads bit 0 set 43.9–46.5% of the time. MAME's 6.8% comes from
    its coarse bit-0 model, as the issue notes.
  - **Bit 3** is the 6845's 16-line vertical sync: 16/262 = 6.1% of the time.
    The kit reads 5.4%, MAME 6.2%. 013Ah transitions over 30h ticks
    (2.64 s) is 157 frames, or 59.5 Hz.
  - **Loop speed.** The IN loop runs about 1.7× slower on the kit than in
    MAME, which is ISA I/O wait time. That is outside this fix.

The run removed the floppy data it saved. Kit B was restored to its baseline
and released, idle with its lease free. The runner and frames are on Powerboat in
`~/fes-scratch/xt1537`.
