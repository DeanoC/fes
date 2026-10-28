# 2026-10-25: Pentagon TR-DOS disks, seals and checks

Record of TR-DOS disk support on `fes.pentagon` (#1101): the unit-1
`fes.media.spectrum-disk` interface, the host's raw track conversion of
`.trd` and `.scl` images, the Beta 128 controller and TR-DOS paging, the host
simulations behind them, and routed, timing-closed bitstreams of all four
Spectrum models (every model's package changes, because the shared machine,
ROM, mailbox and DDR store sources changed). It is not acceptance of an image
or release.

## Design

`fes.media.spectrum-disk` 1.0 (capability bit 15) is a removable disk on
`fes.computer` media unit 1, beside the unit-0 cassette. Its medium is a raw
track image (`sources/FogCast/internal/spectrumdisks`): 8 KiB slots, a header
and one slot per cylinder and side holding the track's MFM bytes from the
index hole and a missing-clock bitmap. The optional
`fes.media.spectrum-disk-trdos` 1.0 names the Beta 128 controller; a +3
uPD765 can later read the same medium under its own sub-interface. The shared
mailbox gains an optional unit 1 (`ENABLE_SPECTRUM_DISK`): one transfer at a
time, `media_write_unit` naming the unit; cores that leave it off are
unchanged. The runtime maps the interface to unit 1 and admits it only beside
the Spectrum tape and HPS DDR. The host lists role `disk` (`.trd`, `.scl`) on
unit 1, converts launch and live disks, and the in-session picker admits them.

The Pentagon keeps the image at HPS DDR window offset 32 MiB, read through a
cached byte fetch port of `fes_media_ddr` on DDR port 2.
`rtl/spectrum_beta.sv` is an FD1793 and the Beta system register with Fuse
1.6.0's command, head-load/READY and status semantics (`wd_fdc.c` with
`WD_FLAG_BETA128`), reading a disk that turns: one byte per 112 T, a 10 ms
index pulse, Lost Data for a byte the CPU misses. The image is write
protected. TR-DOS pages in on an opcode fetch from `$3Dxx` and out on one from
`$4000` up, both with 48 BASIC selected, and the Beta ports are decoded only
while it is paged. The firmware is 48 KiB (128, 48 BASIC, TR-DOS); the
package is `fes.pentagon` 0.4.0. A reset with a disk inserted boots it
through TR-DOS from 48 BASIC, as Fuse's disk autoload does. The host inserts
a launch disk after Start with the image's boot flag set, and the Beta then
resets the machine into that boot; a launch disk without a `boot` program
gets Fuse's autoload loader. A disk changed while the machine runs is only a
new medium.

## Host checks

- The converter's tracks match the raw tracks Fuse 1.6.0's `disk.c` builds
  (a harness linking Fuse's `disk.o`): 905 of the 910 local `.trd`/`.scl`
  images byte for byte; 3 are refused by both, and 2 `.scl` images differ
  only in the descriptor's deleted-file count, which Fuse reads from the
  wrong buffer. With Fuse's autoload on, 895 of the 907 launch images match,
  the rest differing only in descriptor bytes Fuse clears when it rewrites
  the descriptor. 278 of the 910 images have no `boot` program.
- `go test` for `internal/spectrumdisks` (geometry and sector order of every
  disk type, `.scl` directories, the boot loader, Fuse-checked image hashes,
  layout and refusals), `protocol`, `fogcast`, `hostclient` and
  `internal/hostapi`; the runtime's unit tests (discovery of the two units,
  unit-addressed transfers, admission rules) and its full `run-tests`.
- `make sim-fes-spectrum` passes: the four machines (marker T states 14950,
  14960, 14955, 14946 unchanged), snapshot restore, the Beta 128 bench, the
  two-unit mailbox bench, video, board and tape.
- With the real 128 ROM and TR-DOS 5.03 (held locally, never committed), the
  Pentagon machine simulation boots the 128 menu matching Fuse instruction
  for instruction over 60 frames, and boots a `.trd` game (`1000`, A.I.
  Kuzma) with frames 100, 250, 499, 1000 and 1999 pixel-identical to Fuse's
  autoload of the same disk. A boot-flagged disk inserted at frame 30 resets
  the machine into TR-DOS.

## Artifacts

Source FES `f931e08c` (`sources/misteross`), producers
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`, unchanged), HIP
router, first-pass placement search in each recipe's seed order. Every core
is constrained to a 52.224 MHz system clock, the 74.25 MHz pixel clock and a
12.288 MHz audio clock.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.5.0 | 2 | 53.64 | 81.04 | 183.12 | 9,442 | 4,821 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.3.0 | 5 | 61.35 | 88.59 | 165.48 | 10,459 | 5,083 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.3.0 | 4 | 64.50 | 86.15 | 225.28 | 10,555 | 5,086 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.4.0 | 4 | 58.19 | 97.31 | 185.01 | 11,807 | 5,997 | 241 (32 TDP) | 3 |

- `fes.spectrum` 0.5.0: package `baa042cca0eef995d7318d17ee7e1c719b14be73639b6b6dc7330448985b5792`, build `f9dabced0f82427cea98d9427b06fbc5`, RBF sha256 `e766132ce5530369483fd4a1946979dcaf8ddba66bf09f4b44c9be0c0474d7e3` (2,660,448 bytes)
- `fes.spectrum128` 0.3.0: package `f7b2bdade3a66730e7fe61be9485fdf72442bf410eb5956262d4613b96912241`, build `c231ed14cf94c4709b95b2de841b28ab`, RBF sha256 `7f531410a3fd42bb94590725c68afa6f5001027934e6541086535b4c52a42aa6` (3,022,606 bytes)
- `fes.spectrum-plus3` 0.3.0: package `b82212e9192c22e8a7c9c6e8ae08f054ad402a45861e4bd0f1c7e646f9f8db3f`, build `33e0fd3122370cacdeec19022ae4c334`, RBF sha256 `157a0969ff375bec00ca0f6087c7c88598f40c8720e5b5d0a5b08feffe7c48f7` (3,069,349 bytes)
- `fes.pentagon` 0.4.0: package `1e9b0a6d472cd8a8b1e1b84d94634a8f254b46b0c61d91ee8fdc6ae5d68af652`, build `956b3f0b3a596e32c66bee01d59a1886`, RBF sha256 `460edf54d6a44ed94d58b7b75d361294660821848cafa6b0d9683d9848ba83fa` (3,057,058 bytes)

The 48K closed on seed 2 after seeds 5 (51.86 MHz) and 4 did not; the
Pentagon on seed 4 after seed 5 (47.75 MHz). The Pentagon's 241 M10K are the
128K's 225 and its 16 TR-DOS firmware lanes; the disk store adds none.

## Kit check

Kit B, run `trdB2` (2026-10-25 19:26-19:55 EET), on the running image
`4db74864` with the branch's kit software (bundle `12e3c8c5`, built from
`0f39491f`: the runtime's unit-1 Spectrum disk) installed and activated, and a
private FogCast host built from `f931e08c`. The kit imported the four
packages above by package ID. Bundle install-then-restore: afterwards the kit
software is back on `35d58489` (generation 64), the image, boot state, idle
and launcher files are unchanged, and the kit was released idle and
lease-free.

- TR-DOS: `fes.pentagon` 0.4.0 launched three disks, each booting through
  TR-DOS from its launch image: `tetris.trd` to its menu, `b1942.scl` (1942)
  to its options screen and `bombjack2.scl` (Bomb Jack 2) to its first
  prompt. A disk inserted live while Tetris ran changed only the medium; the
  running program carried on. `fes.spectrum` refused a disk
  (`UNSUPPORTED_OPERATION`), having no disk unit.
- Snapshots: the 48K restored `.z80` and `.sna` games (Exolon, Fantasy World
  Dizzy, Acro Jet), the 128K Bubble Dizzy and Xonix, the +2A/+3 Nanako
  Descends to Hell and Exolon, and the Pentagon Bubble Dizzy and Exolon. The
  48K, 128K and Pentagon each refused a snapshot class they do not run (the
  +2A/+3 runs all three).
- Tapes: all four models loaded the turbo-loader `.tzx` from DDR to its
  eight paper stripes and `0 OK, 10:3`, with HDMI held (ADV7513 `0xBA` =
  `0x20`, no relocks). The 128K, +2A/+3 and Pentagon passed the automatic
  stripe check. On the 48K it failed: the still grabbed after the recording
  was black, as were three of that run's 600 recorded frames, while the
  recording's last frames show the stripes and `0 OK, 10:3` exactly as on the
  128K. The check's own verdict was FAIL; the load is judged from the
  recording.

## Not established

- Disk writes (the image is write protected), Beta drives B-D, the
  Pentagon's magic NMI button, and the +3 disk.
- Ejecting the TR-DOS disk through the host's clear request or the CLI's
  disk commands; a live insert replaces it.
- Copy-protected disks beyond what a `.trd`/`.scl` can express, and game
  play beyond the screens the runs judge.
