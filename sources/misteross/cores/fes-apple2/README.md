# FES Apple II

This directory is the described core `fes.apple2`: an Apple II+ class home
computer on the `fes.computer` 1.0 mailbox
([home-computer I/O](../../../mister-packages/docs/computer-io.md)). It is the
first FES home computer and the pathfinder for cards, late-bound firmware and
removable media. The cross-component contract is the
[Apple II pathfinder design](../../../../docs/superpowers/specs/2026-09-26-apple2-pathfinder-design.md).
The core lane is [docs/cores.md](../../docs/cores.md).

## Machine

- NMOS 6502 ([`cpu6502`](../fes-common/rtl/cpu6502/README.md), Arlet Ottens'
  core) at the original 14.31818 MHz × 65/912 ≈ 1.0205 MHz average rate, from
  a fractional enable of the 52.224 MHz system clock. `RDY` is the clock
  enable; the machine registers each bus cycle at the enable, applies side
  effects on the following clock and holds read data until the next enable.
  Undocumented opcodes are not implemented. The core's reset sequence would
  write three stack bytes; those writes are suppressed.
- 48 KiB RAM and a built-in 16 KiB language card (slot 0 switches
  `$C080-$C08F`, two-read write enable, reset to "read ROM, write bank 2").
- 16 KiB linked firmware window: `$D000-$FFFF` motherboard ROM, and the
  `$Cn00` page of a built-in card at offset `$0n00` (the Disk II boot PROM at
  `$0600`). The OSS package leaves the sixteen 1024×10 M10K lanes (column 5,
  rows 32–47) blank; FogCast links a selected 16,384-byte image at download
  time. No Apple ROM bytes are in this repository or the package. Those
  sixteen lanes, and the explicit text-font M10K, still use asynchronous
  reads. The seal allowlists only `machine.rom.lane0`–`lane15` and
  `video.font_rom`; any other async M10K fails the build.
- Keyboard latch `$C000`/`$C010`, speaker `$C030`, cassette output `$C020`,
  soft switches `$C050-$C057`, annunciators `$C058-$C05F`, push buttons and
  paddle timers `$C061-$C067`, paddle trigger `$C070`. Unclaimed I/O and slot
  space reads `$FF` (there is no floating-bus emulation).
- Ctrl-Reset (Control+F12 or Control+Pause) holds the 6502 and card RESET line
  throughout the key press, keeping RAM, video/annunciator switches and
  language-card mapping/write protection. The 6502 reads the reset vector from
  that retained ROM/RAM mapping; firmware restores the screen. Host Hold
  initializes the machine's switches and selects the motherboard ROM again.

The II/II+ reset behavior follows the motherboard's address-controlled F14
latch in Apple's [Reference Manual, Figure S-10](https://apple2history.org/dl/Apple_II_Redbook.pdf)
and the power-on clear in the original
[Language Card schematic 050-0019-01](https://mirrors.apple2.org.za/ftp.apple.asimov.net/documentation/hardware/schematics/language_card_050-0019-01_schematic.pdf).
The IIe's reset of its integrated memory switches is different.

## Video

`apple2_video.v` scans text, lo-res and hi-res pages directly from RAM port B
in the 74.25 MHz HDMI domain; there is no frame buffer. The 280×192 picture is
scaled 4× horizontally and 3× vertically to 1120×576 and centred in fixed
1280×720p60. Text uses an original open 5×7 character set
(`diagnostic/font.py`, generated as the logic function `rtl/apple2_font.vh`), white on black, with inverse
and ~1.9 Hz flashing. Lo-res draws the sixteen colours directly. Hi-res
follows the NTSC artifact rule: each 14 MHz half dot updates one bit of a
four-bit window indexed by its phase, and the window is the lo-res colour
index, giving violet/green/blue/orange/white. Soft switches are sampled once
per line, so mid-line raster effects are not reproduced.

## Slot bus and cards

Slots 1–7 share one registered request word (`rtl/apple2_bus.vh`,
`fes.apple2-bus.slot/1`): address, write data, direction, a STROBE pulse at
the start of each 6502 cycle, a 2 MHz Q3 pulse, reset and per-slot active-high
DEVSEL/IOSEL plus the shared IOSTROBE. The 28-bit response carries read data,
DRIVE, IRQ, NMI, INH and a signed 16-bit PCM contribution. A vacant response
is all zero. Cards own their `$C800` expansion-ROM enable, as on the original
backplane.

Slots 2, 4, 5 and 7 are physical sockets (`rtl/apple2_slot_sockets.v`): each
pins its 32 request and 28 response flip-flops in the first three LABs of
column 24 of its placement rectangle (rows 1–18, 21–38, 41–58, 61–78) plus
one clock-coverage flip-flop per socket row in columns 24 and 28. A frozen
shell routes a horizontal clock segment (HCLKB.16 for column 24, HCLKB.25 for
columns 25–28) only to rows where it has a cell, and a card cannot add one
because those pips lie outside its CRAM fence; the anchors make both segments
reach every socket row. The Go linker layout `fes.apple2-bus.slots/1`
admits any combination of independently built cards into those sockets.
Slot 6 is the built-in Disk II controller; slots 1 and 3 are vacant.

### Cards

`expansions/probe.v` is the open probe card (module `cart`, the socket plug
ports nextpnr merges onto a frozen shell): a scratch/ID/counter/tone register
file at `$C0n0`, a position-independent 256-byte `$Cn00` page
(`diagnostic/probe_card.py`, signature `FESPROBE` at `$CnF8`) that tests the
card and prints through the diagnostic, 1 KiB of `$C800` RAM claimed by a
`$Cn00` access and released by `$CFFF`, and a square-wave tone on the slot
audio. The diagnostic's `S` command scans slots and calls every probe card;
`D` prints, per slot, the eight bytes the CPU reads at `$CnF8`, the card
registers and a `$C800` RAM write/read-back, so the late-bound firmware alone
can inspect a linked card on hardware. The machine simulation links the card
into sockets 4 and 7 and checks both commands.

The card's ROM is an explicit M10K and its RAM a plain inferred memory, which
synthesis maps to a dual-clock M10K. The toolchain's cart merge drives every
cart clock pin from the socket clock and rejects undriven cart inputs (FES
#250; nextpnr before that left the RAM read clock floating and reads returned
zero on hardware).

```sh
python3 scripts/build_apple2_slot_card.py --shell build/fes-apple2-oss \
  --package build/packages/SHELL_PACKAGE_ID --slot 4 --card probe [--cache-root DIR]
```

The card producer copies the shell's frozen routed netlist, renames only the
chosen slot's boundary flip-flops to the canonical plug cells, places the card
in that slot's named region (`--fes-cart-region slotN`), fences routing to its
CRAM rectangle, requires the three shell clocks, requires every card clock pin
on the shell system clock, rejects any CRAM change outside the socket, and
publishes `build/apple2-cards/RECIPE/EXPANSION_ID.tar`
(`manifest.json` with `slot_index`, and `cart.rbf`). The Go
`expansion/cmd/fes-slot-link` composes any set of those archives and an
optional firmware image onto the sealed shell, as a library launch does.

## Disk II

The slot 6 controller (`apple2_disk2_card.v`) owns the `$C0E0-$C0EF`
switches (phases, motor with ~1 s off delay, drive select, Q6/Q7) and the read
latch. Bits shift into an assembly register; leading zeros do not shift
(self-sync); a completed nibble is held for eight CPU cycles. The drive
(`apple2_disk2_drive.v`) keeps the head in quarter tracks, emits one bit per
four CPU cycles while spinning and synthesises the 16-sector track on the fly
from the 143,360-byte DOS-order image: 40/20 sync bytes, address field with
volume 254, 6-and-2 data field, physical-to-DOS interleave. Media unit 0
(`fes.media.apple2-floppy`) holds the image in `apple2_disk_store.v`; the drive
presents it only while the unit is ready, and emits noise when empty. The disk
is read only: write protect is always reported. Drive 2 is empty.

## Keyboard, controllers, audio

`apple2_keyboard.v` turns USB HID key state into Apple II codes: upper-case
letters, US-layout shifted symbols folded onto the II+ character set,
Control codes, Return, Escape, Tab, Backspace/arrows, Delete, 15 Hz repeat
after 0.5 s, and the Alt keys as push buttons 0/1. The rows must be quiet for
1 ms before a change is interpreted, so a key and its modifiers are seen
together. Controller port 0 A/B are push buttons 0/1 and its D-pad sets
paddles 0/1 to their end stops; port 1 drives paddles 2/3 and button 2.
`apple2_audio.v` DC-blocks the speaker (4 ms) and mixes slot PCM into the
shared 48 kHz I2S path.

## Open diagnostic

`diagnostic/asm6502.py` is a small two-pass 6502 assembler. `firmware.py`
assembles an original 16 KiB firmware image: RAM and language-card self tests,
a text/character-set screen, lo-res, hi-res and mixed test screens, keyboard
echo, and a clean-room Disk II boot and 6-and-2 sector reader behind an open
slot 6 boot page. It also builds a synthetic disk whose two boot stages load
and verify sectors on tracks 0, 17 and 34 (every other sector holds all 256
byte values in an arithmetic progression). `render.py` independently renders
the expected HDMI frames. Nothing is derived from Apple's ROMs.

The diagnostic's `R` command installs a reset vector in language-card RAM,
selects and write-protects bank 1, and changes all four video switches and
the annunciators. Ctrl-Reset then checks retained RAM, stack bytes, bank
selection and write protection through the running 6502. Host Hold returns
to the normal diagnostic.

## Simulation

```sh
make sim-fes-apple2            # all three below
make sim-fes-apple2-mailbox    # fes.computer golden exchanges on fes_computer_mailbox
make sim-fes-apple2-machine    # diagnostic boot, screens, held RESET/LC vector and disk
make sim-fes-apple2-board      # top.v through the mailbox: HID keys, live disk, eject, reset
```

The board simulation releases execution with an empty drive, checks nine key
translations and speaker audio, inserts the synthetic disk into unit 0 while
the machine runs (71,680 data transactions), boots it, ejects it without
stopping the machine and warm-resets with Control+F12. These are host
simulations, not an RBF, timing or kit result.

## Not implemented

Disk writes and write-back to the host, a second drive, cassette input,
floating-bus reads, mid-line video effects, 80-column and lower-case display,
undocumented 6502 opcodes and cycle-exact Disk II LSS timing.
The planned next iterations and their open decisions are in
[the Apple II next-iterations plan](../../../../docs/superpowers/plans/2026-09-27-apple2-next-iterations.md).
