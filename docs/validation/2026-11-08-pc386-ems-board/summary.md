# pc386 EMS board, 8 November 2026

This record covers [#1554](https://github.com/DeanoC/fes/issues/1554): an
expanded-memory board in `fes.pc386`, with its own LIM 4.0 driver. The
[compatibility census](../2026-11-07-pc386-compat-census/summary.md) found four
titles that stop for want of EMS. EMM386 needs V86 mode and paging (CPU Phase
5); a board needs neither. The design is in
[`pc386-machine-design.md`, "EMS board"](../../../sources/misteross/docs/pc386-machine-design.md#ems-board).

**Headline.** Three of the four titles now play on the kits:

| Title | Before (census) | Now on a kit | Notes |
| --- | --- | --- | --- |
| Ultima Underworld | "Out of EMS Memory C001" | **plays**: intro, character creation, then walking in the Abyss | INT 67h 50h map-multiple 110,242 times in its first 20 s on the oracle |
| Frontier: Elite 2 | needed 594,000 bytes, then 768K of EMS | **plays**: intro, Ross 154 start, cockpit and galactic map | keys `2` (AdLib/Sound Blaster) at its sound menu, then `1` for the start position |
| Master of Orion | "You must have at least 1024K of expanded memory" | **plays** after its own `INSTALL` selects "SoundBlaster (orig.)", IRQ 7: new game, then the Sol production screen | eXoDOS's config selects "SoundBlaster Pro (later)", which the installer itself greys out on a Sound Blaster 2.0; with it ORION.EXE stops with "You selected an invalid sound driver" |
| Darklands | "Unable to find EMS memory" | **does not run**: finds EMS, allocates and frees two handles, then prints garbled text and exits | the same text comes from its Art Gallery on boot entry 1 without the driver, so the stop is not EMS: [#1553](https://github.com/DeanoC/fes/issues/1553) |

The five acceptance games still start on the default boot entry: Ultima VII
(intro), Serpent Isle (intro), Wolfenstein 3D (title), Ultima VI (intro) and
DOOM (menu over the demo).

**One brief item could not be met as written.** The driver cannot be on the
default boot entry. Ultima VII's MAINMENU.EXE looks for the `EMMXXXX0` device
and stops with "Please remove your expanded memory manager before running
Ultima VII" whatever the manager is (oracle bench, below). The games image
keeps entry 1 unchanged for Ultima VII and adds entry 3, entry 1 plus the
driver, for the EMS titles. The per-game board setting
([#1555](https://github.com/DeanoC/fes/issues/1555)) is what later lets the
default entry carry the driver, gated by CMOS.

## What was built

- **Board** ([`pc386_ems.sv`](../../../sources/misteross/cores/fes-pc386/rtl/pc386_ems.sv)):
  the Lo-tech 2MB EMS Board's register interface, jumpers fixed at E000h and
  260h, with the hkzlab 4 MB board's 8-bit page numbers. Four write-only page
  registers (FFh at machine reset: unmapped), the frame as uncached MMIO
  beside the VGA's, and 2 MiB of guest-invisible DDR at window offset
  `0x01400000` (4 MiB reserved), single beats on port 2 between capture and
  the shell's aux master. The 8237's memory cycles in the frame go to the
  board too, as on an AT. The machine mirrors it into CMOS 63h-65h.
- **Driver** ([`fesems.asm`](../../../sources/misteross/cores/fes-pc386/sim/fesems.asm)),
  `FESEMS.SYS`, MIT, NASM, about 4.1 KiB resident: LIM 4.0 40h-48h, 4Bh-54h,
  57h-5Dh (5Bh with register set 0 only); 49h, 4Ah, 55h and 56h answer 84h
  and name themselves on port E9h.
  No open-licensed driver for this interface exists: Lo-tech's LTEMM is
  Tsourikov's BSD driver patched under the Standard Lo-tech Licence, which is
  not open source, and karcherm's TOPEMM inherits those terms.
- **Games image**: UMBM takes `/I=C800-DFFF`; boot menu entry 3 is entry 1
  plus `DEVICEHIGH=C:\FDOS\BIN\FESEMS.SYS`.
- **Benches**: `ems_tb.cpp` (the board alone), the AT-core bench's port and
  CMOS checks, `machine_tb` routing E0000h-EFFFFh to the RTL board for the
  oracle and the RTL CPU, `EMSTEST.COM` and the `ems` suite, and `--boot-entry`, `--game-keys` and
  `--oracle-profile` for the game suite.

## Checks

### Board and devices

| Check | Result |
| --- | --- |
| `sim_fes_pc386.py --suite benches`: `ems_tb` (directed: reset unmapped, byte enables in both halves of a beat, aliasing, last page, past the end, a register write after a posted store, reset; DMA bytes written and read on a mapped page and the CPU seeing them, DMA into an unmapped page; then 40,000 random accesses each on 128- and 255-page boards against a reference) | 14 of 14 pass; board memory matches the reference byte for byte and nothing outside the 4 MiB changes. The presenter, capture and plane benches still pass with the fifth port 2 client tied off |
| `--suite devices`: 260h-263h take bytes (a word as two; 25Fh and 264h are not the board's), read FFh at `isa8`; CMOS 63h-65h = 128, E0h, 26h | 137 of 137 pass |
| `--suite sound`: SOUND.COM's Sound Blaster DMA from conventional memory, which must still reach the CPU's DMA port | pass |
| `python3 -m unittest tests.test_pc386_machine tests.test_pc386_game_drive tests.test_build_fes_pc386_quartus` | pass, with new tests for entry 3, the UMB range, the `ems` suite and the board bench |

### Oracle bench

`machine_tb` with the Bochs oracle as CPU and the RTL machine.

- **Upper memory budget.** With UMBM at C800-DFFF the games image's entry 1
  keeps **594,256 bytes** free (the figure before), 12K of the 96K of upper
  memory used. With the driver (now entry 3) it is still 594,256 bytes, 16K
  used, and MEM reports 2,048K of EMS, 2,048K free, 64 handles. The VGA trace
  replay matched Bochs's VGA (279,210 events, no mismatch). The driver's load
  clear made 524,800 frame writes and 257 reads.
- **EMSTEST.COM** (`--suite ems`, entry 3): detect, frame, counts, allocate,
  a full 1 MiB pattern through all four frame pages, alias, unmap, save and
  restore (47h/48h and 4Eh), map multiple by segment, reallocate, names,
  57h moves across a page boundary both ways, an overlapping expanded move
  (92h, data intact), an exchange and 94h for a conventional side in the
  frame, error codes 83h, 8Ah, 87h, 84h and A3h, 51h's unchanged count when
  it refuses, the OS/E functions (5Dh's key and disable, 5B01h's save area
  returned by 5B00h and not replaced by a bad one, 5B06h's 9Eh), and free:
  **all 16 pass** on the final RTL and driver. Its first two runs found three
  driver bugs, fixed before the driver went on the kit: `set_register` wrote its map shadow through DS
  (4E01h and 5B01h call it with DS = SS), map errors returned the function
  number instead of 8Ah, and 57h's overlap direction read EAX after AH was
  overwritten (correct only by luck). A third failure was the test's own
  destination offset. Codex's reviews then found LIM 4.0 gaps on paths no
  title uses, all fixed: 5B00h/5B01h's save area, 5Dh's disable and its
  predictable key, 5B06h, 57h with a conventional side in the frame, 51h's
  BX on refusal and 4F00h's count error. The four titles call none of 4Fh,
  57h, 5Bh or 5Dh and no refused 51h, so the kit runs below, some made with
  the driver before those fixes, are unaffected.
- **Titles** (`--suite game --oracle-profile`, 20 simulated seconds, the
  driver on the default entry before entry 3 existed, census keys):

  | Title | INT 67h functions (count) | Reached |
  | --- | --- | --- |
  | Ultima Underworld | 40 (2), 41 (2), 42 (2), 43 (2), 44 (84), 46, 47 (51), 48 (51), 50 (110,242), 53 | intro text and pictures |
  | Master of Orion | 41, 42, 43 (2), 45 (2), 46, 4B, 53 (50) | past the EMS check; then "You selected an invalid sound driver" (eXoDOS config) |
  | Darklands | 41, 42, 43 (2), 44 (2), 45 (2), 46, 47 (2), 48 (2) | past the EMS check; then the garbled exit of #1553 |
  | Frontier: Elite 2 | 40, 41, 42 | past the memory and EMS checks to its sound menu (the census keys press no digit) |
  | Ultima VII | none | "Please remove your expanded memory manager before running Ultima VII" |

  Every function the titles called is implemented; no `FESEMS: function`
  line appeared. These runs predate the DMA change; the bench of the time
  counted DMA cycles into the frame and saw none from any title.

### Seals

Quartus 17.0.2, seed 1, each from a clean checkout; **pass** on every
analysis at both corners. System 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.

| Commit | RTL | Setup | Hold | Recovery | Removal | ALMs | Package | Used for |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `a1528a999` | final: DMA into the frame | +0.879 ns | +0.105 ns | +6.635 ns | +0.274 ns | 32,928 | `29c621bd6df4ea29…` ([seal-summary.json](seal-summary.json), build `01f2c9f1…`, RBF `88efe6fe…`) | Kit B e3 |
| `c0b7da087` | the board without the DMA path | +0.430 ns | +0.038 ns | +7.476 ns | +0.526 ns | 32,867 | `cc6b0c6d0e3fbfb2…` (build `646bb667…`, RBF `7d3981ed…`) | Kit A e1, e2 |

Both use 265 of 553 M10K blocks and 38 DSP blocks. A first attempt in the
working tree met timing (+0.933 ns setup, +0.108 ns hold) but refused to
export because the tree changed during the build.

### Kits

Core-only, on each kit's baseline software (Kit A `4efd34bf` on image
`31b39063`, Kit B `a8cd30c4` on image `f904961e`): no install, no image
change, no reboot, the shared host untouched. The sealed
package was launched as fresh library games through the agent under a kit
lease, with game drives from a private store (`~/fes-drives/pc386-ems`, the
games image of this branch plus Deano's eXoDOS titles). Runners and frames are
in `~/fes-scratch/ems/kit/runs/` on Powerboat.

- **e1** (02:05-02:41): the board in hardware: `FESEMS:  2048K of EMS at E000h,
  registers at 0260h`, and MEM identical to the bench (594,256 bytes, 2,048K
  EMS, 16K upper used). Ultima Underworld, Frontier and Master of Orion played;
  Darklands stopped as above, and its Art Gallery printed the same text on
  entry 1. The five acceptance games started on entry 1, from this branch's
  `xms`, `fpu`, `real` and `pmode` drives. The first three titles ran from a
  drive built before the driver fixes.
- **e2** (02:52-03:01): Ultima Underworld (into the Abyss and walking),
  Frontier (the cockpit and galactic map) and Master of Orion (its `INSTALL`,
  then a new game to the Sol screen) again from drive `5ebf9693`, which
  carries the driver with the EMSTEST fixes.
- **e3** (Kit B, 08:18-08:26), the final seal `a1528a999` and drive
  `af0f7fb0`: MEM identical (594,256 bytes, 2,048K EMS), Frontier in its
  cockpit, Ultima Underworld's intro and menu, and Ultima VII and
  Wolfenstein 3D starting on entry 1 (Wolfenstein's Sound Blaster DMA from
  conventional memory still reaches the CPU).

Every run was logged in its kit's status file. Each released the kit idle with
a free lease and the same image, previous, pending, idle, launcher, images,
quirk and boot ID keys as before, then removed the FogCast data trees it
created (26, 7 and 10: staged drives, game records and disk deltas).

## RTL runs

`sim_fes_pc386.py --suite ems --cpu rtl`: the oracle boots the games image on
entry 3 and `fes_i386_cpu` takes the prompt (`--rtl-at-prompt`), so
EMSTEST.COM and the driver's INT 67h handler run on the RTL CPU against the
RTL board. **All 16 checks pass**; the RTL retired 51,530,712 instructions
with no oracle steps, no protected-mode steps and no stops, and the board saw
the same 265,430 reads and 788,962 writes as on the oracle. A first attempt
with the RTL booting FreeDOS itself was still booting after 90 minutes at
this host's load and was stopped.

`--suite game --game uw1 --cpu rtl --rtl-at-prompt --boot-entry 3`: Ultima
Underworld, a title that uses EMS heavily, from the prompt on the RTL CPU to
18 simulated seconds: its EMS checks, then its intro text and pictures, as on
the oracle and the kit. The RTL retired 656,322,895 instructions with no
oracle steps, no protected-mode steps and no stops; the suite passes. Both
RTL runs used the RTL before the DMA change, which touches only the 8237's
cycles into the frame; the oracle `ems` and `sound` suites ran on the final
RTL.

## Follow-ups

- [#1555](https://github.com/DeanoC/fes/issues/1555): the per-game board
  setting (word 7), so the default entry can carry the driver and Ultima VII
  entries turn the board off.
- [#1553](https://github.com/DeanoC/fes/issues/1553): Darklands' garbled exit,
  not EMS.
- Master of Orion's drive needs a sidecar `CONFIG.MOO` from its own `INSTALL`
  ("SoundBlaster (orig.)", 220h, IRQ 7, DRQ 1), as Rise of the Triad's and
  Serpent Isle's have. That is a `game-drives.toml` change for the census
  drives.
- The census titles need boot entry 3 for EMS; the drive manifest has no
  per-title boot entry yet, so runners pass it (`--boot-entry 3`, or typing
  `3` at the menu).
