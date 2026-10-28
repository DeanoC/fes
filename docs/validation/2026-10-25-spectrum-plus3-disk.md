# 2026-10-25: +2A/+3 disks, seals and checks

Record of +3 disk support on `fes.spectrum-plus3` (#1105): the host's raw
track conversion of `.dsk` images, the uPD765A controller on the unit-1
`fes.media.spectrum-disk` medium the Pentagon's TR-DOS drive introduced
(#1101), the simulations behind them, and routed, timing-closed bitstreams of
all four Spectrum models (every model's package changes, because the shared
machine and drive sources changed). It is not acceptance of an image or
release.

## Design

The medium is the raw track image of #1101, extended for the +3: the header
records the slot size (8 KiB, or 16 KiB when a track is longer than 6,912
bytes), a track flags byte marks FM tracks, the bitmap marks every address
mark's mark byte in either density, and up to eight weak ranges per track read
back differently each time. The unit's limit rises to 2,834,432 bytes.
`internal/spectrumdisks` reads standard and extended `.dsk` images as Fuse
1.6.0's `open_cpc` does for the +3: the uPD765 gap tables, the CPC layout
fixes, recorded CRC errors and deleted marks, sector bytes that fall in the
gap, weak sectors from their stored copies, and FM tracks. A dump's single
copy of a Speedlock weak sector (cylinder 0 sector 2, 512 bytes, data CRC
error) reads weak where Fuse's Speedlock hack corrupts it: bytes 29 and 58 of
an E5-filled sector, otherwise every 29th byte, the first eight.

`fes.media.spectrum-disk-plus3` 1.0 names the +3's controller beside the
TR-DOS one; a package declares exactly one. The host lists role `disk`
(`.dsk`) for it, converts a launch or live disk only for the drive it was
written for, and sends a live `.dsk` to the Spectrum disk when the active
package has one (otherwise it stays an Apple II disk).

`rtl/spectrum_fdd.sv` is now the drive the Beta 128 and the uPD765A share:
rotation, slots, marks in both densities and weak bytes (Fuse's
`data & rand | rand`). `rtl/spectrum_upd765.sv` follows Fuse's `upd_fdc.c`
(UPD765A, 4 MHz, non-DMA) on the turning disk: `$2FFD` main status, `$3FFD`
data, `$1FFD` bit 3 the motor, drive ready 400 ms after it starts, only US0
wired, drive B an empty write-protected drive as in Fuse. Read Data and Read
Deleted Data (multi-track, skip), Read Diagnostic, Read ID, Recalibrate,
Seek, Sense Interrupt Status, Specify, Sense Drive Status, Version and invalid
commands; without a terminal count a read ends with End of Cylinder, as on
the +3. Bytes pass at 32 us and a byte the CPU misses ends the read with
Overrun. The image is write protected. A launch disk resets the machine and,
once the menu has read every keyboard half-row in a frame, ENTER is held
through the third frame after, as Fuse's phantom typist presses it, so the
menu's Loader boots the disk. The package is `fes.spectrum-plus3` 0.4.0; its
M10K count is unchanged.

## Host checks

- The converter's tracks, mark and weak bitmaps match the raw tracks Fuse
  1.6.0's `disk.c` builds (a harness linking Fuse's `disk.o`) for all 710
  local `.dsk` images Fuse opens; the 14 it refuses are refused here too, and
  32 need 16 KiB slots. The Speedlock marking deliberately differs from
  `disk.c`, which leaves that to `upd_fdc.c`: 29 of the 724 images carry such
  a sector.
- `go test` for all of FogCast passes, including the `.dsk` cases, the
  system checks (a `.trd` refused for the +3, a `.dsk` for TR-DOS), the
  Speedlock sectors, the controller declarations, `.dsk` routing in the host
  client (and Apple II `.dsk` unchanged).
- `make sim-fes-spectrum` passes: the four machines (marker T states 14950,
  14960, 14955, 14946 unchanged), snapshot restore, the Beta 128 bench, the
  new uPD765 bench, the two-unit mailbox bench (now for the +2A/+3 too), video
  and board.
- With the real +3 ROMs (held locally, never committed), the +2A/+3 machine
  simulation boots launch disks through the menu's Loader. Its controller
  traffic matched Fuse's byte for byte through Batman's boot except Read ID's
  first sector: Fuse's disk moves only when a byte is read, ours turns in
  real time, so +3DOS reads some sectors in another order. Against Fuse's
  autoload of the same disks:
  - Bubble Bobble, Batman: The Caped Crusader (Speedlock, E5 type) and Beyond
    the Ice Palace (Speedlock, data type) reach the screens Fuse shows.
  - Of eleven further disks (Rainbow Islands, Chase H.Q. II, Target Renegade,
    Rick Dangerous 2, RoboCop 2, Afterburner, Shadow of the Beast, Night
    Raider, Edd the Duck!, Rolling Thunder, Batman: The Movie), ten show the
    same screen as Fuse at frame 1500, some at a different point of their
    attract animation. Batman: The
    Movie was still on its loading screen at frame 1520 and reached the
    same menu by frame 2000: Fuse hands the CPU each byte as soon as it reads
    the data register, while here the disk turns at its real speed, so
    loaders that read whole tracks take longer.

## Artifacts

Source FES `a5de9abb` (`sources/misteross`), producers
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`, unchanged), HIP
router, first-pass placement search in each recipe's seed order. Every core
is constrained to a 52.224 MHz system clock, the 74.25 MHz pixel clock and a
12.288 MHz audio clock.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.5.0 | 5 | 70.67 | 90.20 | 227.32 | 9,413 | 4,821 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.3.0 | 4 | 66.14 | 99.21 | 131.61 | 10,420 | 5,083 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.4.0 | 5 | 61.27 | 93.60 | 199.40 | 13,720 | 6,642 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.4.0 | 5 | 57.81 | 88.82 | 204.46 | 12,058 | 6,264 | 241 (32 TDP) | 3 |

- `fes.spectrum` 0.5.0: package `35b0041bcbb7bc2cb47aaa0f238eae548b44ad9e597444d07b540517d9e4ff23`, build `572d1e9c8ca325de5d2ae7342aecd5c1`, RBF sha256 `79adb1998542c11e2c0f3a20a2bf2a3527dd82586f0a6069a729a64722dc86c0` (2,657,345 bytes)
- `fes.spectrum128` 0.3.0: package `9aa51a5d000785273b8735c14d0686d31a5e7d285d09bd3d931a008bbc8ae69e`, build `bae7ae993fb2238fa979fd3d3f02dfcd`, RBF sha256 `5fb1a2838ed612141a9294229783b0c218fae0e9eb9f9ff8509679d90587d534` (2,996,125 bytes)
- `fes.spectrum-plus3` 0.4.0: package `d0318f1e5f0f6851c29d3109bdb357278803d4dcc8a722b88f532e4ff675813e`, build `621e33eb33218f87b833d99edc15fd42`, RBF sha256 `d7817d58c6315274bc3df3df28bb5441864609647029937e20992e1fc079637f` (3,160,308 bytes)
- `fes.pentagon` 0.4.0: package `34be15bb285b573624e3c6be260e19f9ec64d7c625604e124d4bf2b4f65bcd42`, build `0d8954d191bbcc15288b2659529a04e0`, RBF sha256 `7d20b5bd169a9077939d882ed244325f190b25aaebd2e6b567c6eabcfe16edd2` (3,092,540 bytes)

The +2A/+3 uses 257 M10K, as its previous seal did: the controller and
drive add none.

## Kit check

Kit B, run `p3B1` (2026-10-25 20:14-20:45 EET), on the running image
`4db74864` with the branch's kit software (bundle `c8b2a0c5`, built from
`a5de9abb`) installed and activated, and a private FogCast host built from
`a5de9abb`. The kit imported the four packages above by package ID. Bundle
install-then-restore: afterwards the kit software is back on `35d58489`
(generation 66), the image, boot state, idle and launcher files are
unchanged, and the kit was released idle and lease-free. Every automatic
check passed (`HIL_VERDICT PASS`).

- +3 disks: `fes.spectrum-plus3` 0.4.0 launched three `.dsk` disks, each
  booting through the menu's Loader: Bubble Bobble to its control menu,
  Batman: The Caped Crusader (Speedlock, E5 type) to its part select and
  Beyond the Ice Palace (Speedlock, data type) to "Press a key to continue",
  the screens Fuse shows. Chase H.Q. II inserted live while Bubble Bobble ran
  changed only the medium.
- TR-DOS: `fes.pentagon` 0.4.0 booted `tetris.trd` and refused a `.dsk`
  (`BAD_REQUEST`, the disk is for another drive); `fes.spectrum` refused a
  disk (`UNSUPPORTED_OPERATION`).
- Snapshots: every model restored real `.z80` and `.sna` games as in the
  TR-DOS run, and the 48K, 128K and Pentagon refused a class they do not run.
- Tapes: all four models loaded the turbo-loader `.tzx` from DDR to its
  eight stripes and `0 OK, 10:3`, passing the stripe check, with HDMI held
  (ADV7513 `0xBA` = `0x20`, no relocks).

## Not established

- Disk writes (the image is write protected), formatting, Scan commands and
  a second drive with a disk.
- Speedlock sectors whose Fuse pattern has more than eight weak bytes keep
  only the first eight.
- Copy protections beyond what a `.dsk` and the weak ranges can express, and
  game play beyond the screens the runs judge.
