# 2026-10-25: Spectrum snapshot loading, seals and kit A checks

Record of `.z80` and `.sna` snapshot loading on the four Spectrum models
(#1084): the host conversion, the core's restore unit, the host simulations
behind them, routed and timing-closed bitstreams, and kit A runs of the sealed
archives with real game snapshots and the turbo tape. It is not acceptance of
an image or release.

## Design

FogCast converts a snapshot into a restore stream
(`sources/FogCast/internal/spectrumsnapshots`): an 8-byte header (`ZXRS`,
version 1, the snapshot's class: 0 48K, 1 128K, 2 +2A/+3) followed by Z80
code. It is delivered through the cassette unit and the HPS DDR media store
that tapes use. The core's new `spectrum_snapshot` unit reads the first 8
bytes of every image. A tape goes to the deck after the stream rewinds. A
restore stream of a class the model runs resets the machine, and then every
CPU memory read, at any address, takes the next stream byte, with WAIT while
the media ring is empty. The code fills RAM with `LD (HL),n` / `INC HL`, sets
the border, AY registers and paging with `OUT`, loads every register (R
compensated for the fetches after `LD R,A`; AF popped from the stream) and
jumps to PC. The first fetch after the last byte comes from real memory. The
Spectrum README ("Snapshots") describes the code in full.

One stream runs on every model its class allows. A 48K snapshot fills
`$4000–$FFFF`, which every model maps to banks 5, 2 and 0 after reset; the
stream writes `$1FFD` and then `$7FFD`, so on the 128K and Pentagon (where
`$1FFD` decodes as `$7FFD`) the second write replaces the first. A 48K snapshot
on a paged model selects 48 BASIC and locks paging.

Packages declare optional interfaces, as the edge bus is declared:
`fes.media.spectrum-snapshot` 1.0 on every model, plus
`fes.media.spectrum-snapshot-128` 1.0 on the 128K-class models and
`fes.media.spectrum-snapshot-plus3` 1.0 on the +2A/+3. They add no unit and no
capability bit, and the runtime ignores optional interfaces it does not know,
so the kit runtime, the shared mailbox and the shared NMOS Z80 do not change.
The host lists `.z80` and `.sna` for the cassette of a package that declares
them, converts media that is not a tape image but reads as a snapshot, and
refuses a snapshot whose machine the package does not declare. The core also
checks the class and leaves a stream it cannot run to the deck.

The FES Spectrum cores are now `fes.spectrum` 0.5.0 and `fes.spectrum128`,
`fes.spectrum-plus3` and `fes.pentagon` 0.3.0.

## Host checks

- `go test` for `internal/spectrumsnapshots` (parsers, stream writer and a
  model of the core's restore mode that runs each stream on each model's port
  decoding and compares registers, R at the jump, paging, AY and every RAM
  byte), `protocol`, `fogcast`, `hostclient`, `internal/fogcastcli` and
  `internal/spectrumtapes`.
- `make sim-fes-spectrum` passes. The new snapshot simulation writes a
  synthetic `.z80` for each model (48K on the 48K and Pentagon, 128K with
  interrupts in IM 2, +2A/+3 in the all-RAM `{4,5,6,7}` mode), converts it
  with `cmd/fes-spectrum-restore`, streams it with random gaps and checks
  every register, `$7FFD`/`$1FFD`, the border, the AY registers and all RAM at
  the first fetch from real memory, then that the restored program runs; the
  48K, 128K and Pentagon each leave a stream of a class they cannot run to the
  deck. A 48 KiB restore takes 12.3M clocks (0.23 s) and a 128 KiB one 33.4M
  (0.64 s). The machine T-state markers are unchanged (14950, 14960, 14955,
  14946), and the board, video and tape deck checks pass.
- The converter read 15,920 local `.z80`/`.sna` files: 15,754 converted; 155
  were refused as hardware no FES core emulates (84 Scorpion, 49 MGT, 22 16K,
  3 SamRam), 6 `.sna` files hold their stack in ROM, and 2 have an interrupt
  mode above 2.

## Artifacts

Source FES `a520345a` (`sources/misteross`), producers
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`, unchanged), HIP
router, first-pass placement search in each recipe's seed order. Every core
is constrained to a 52.224 MHz system clock, the 74.25 MHz pixel clock and a
12.288 MHz audio clock.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.5.0 | 2 | 64.95 | 82.65 | 209.21 | 9,432 | 4,821 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.3.0 | 5 | 62.61 | 92.28 | 179.28 | 10,481 | 5,083 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.3.0 | 5 | 61.23 | 77.03 | 235.18 | 10,479 | 5,086 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.3.0 | 5 | 61.64 | 91.04 | 185.94 | 10,371 | 5,069 | 225 (32 TDP) | 3 |

- `fes.spectrum` 0.5.0: package `dcf326ed8d1e2f6ad55b89908dfc9bd9124824a07f0abdcfdce0a0d0e812fcbd`, build `0688d81bf333d28869c851bfe6a4f4e5`, RBF sha256 `ed016bc1f40a7d1a63e85d6cc9d06285f42dc835b52f17a356401608d2fbc4af` (2,653,064 bytes)
- `fes.spectrum128` 0.3.0: package `526511752b04bbc718fade379aa00d07ff020708f8d5439559249dd88abd2102`, build `425a3408b4206980f3f77fbb5fdf5c7a`, RBF sha256 `a5f5c9aabaa0a056709eae9f8804b0aa1407f7b64b93db4592651e232cfa7342` (3,026,004 bytes)
- `fes.spectrum-plus3` 0.3.0: package `9a7a723663a03e5809e5fb37155bed5b96d41daa9ac0b1d6262d8432cb7d735a`, build `32745149308a466ac8013d2ec4eb85ec`, RBF sha256 `257b7c0ce8ca72d13c0c310cdd8ce291fd9fd52abdd1138b3cb24d7b8c231281` (3,080,194 bytes)
- `fes.pentagon` 0.3.0: package `35bd7a0175191f85d86747cf033aaf39d36fc798f77102e4a18653c3761eeb06`, build `708d271aa71da6c0f291f2712910ceca`, RBF sha256 `2ee1c31250f040082ac00b936ee9b10bd0a6e12bcc4d3d3977f98f3810a16fc4` (3,005,129 bytes)

The 128K, +2A/+3 and Pentagon closed on their recipe's first seed (5). The
48K closed on seed 2 after seed 5 did not finish routing in its lease and
seed 4 reached 47.27 MHz (0.905 of the system clock).

## Kit A check

Run `snapA1` (08:54 to 09:08 EET) ran the four sealed archives on kit A. The
running image `4db74864` was not changed. Kit software `115494f3…` (built from
`main` `0f3b35c9`, still retained by the kit) was activated over the baseline
`35d58489` and restored afterwards; the kit runtime has no change in this
branch. A private host built from `a520345a` imported each archive, checked
compatibility, linked its locally held ROM and, for each snapshot below,
selected it as the entry's cassette, launched, waited 10 s, grabbed HDMI and
stopped. During the first session of each package a second snapshot was
inserted live. Snapshots came from the local library and went only to the
private host. Every launch reported the sealed build ID.

| Core | Launched snapshot (class) | Screen after 10 s |
| --- | --- | --- |
| `fes.spectrum` 0.5.0 | Exolon `.z80` v3 (48K) | loading screen |
| | Fantasy World Dizzy `.sna` (48K) | title screen |
| | Acro Jet `.z80` v1 (48K) | credits screen |
| | live: Fast Food Dizzy `.sna` (48K) | title screen |
| `fes.spectrum128` 0.3.0 | Bubble Dizzy `.z80` (128K) | menu |
| | Xonix `.sna` (128K) | menu |
| | live: Exolon `.z80` (48K) | loading screen |
| `fes.spectrum-plus3` 0.3.0 | Nanako Descends to Hell `.z80` (+3) | title screen |
| | Exolon `.sna` (48K) | loading screen |
| | live: Bubble Dizzy `.z80` (128K) | menu |
| `fes.pentagon` 0.3.0 | Bubble Dizzy `.z80` (128K) | menu |
| | Exolon `.z80` (48K) | loading screen |
| | live: Fantasy World Dizzy `.sna` (48K) | title screen |

Selecting a snapshot of a machine the package does not run was refused with
`BAD_REQUEST` on the 48K (a 128K snapshot), the 128K and the Pentagon (a +3
snapshot); the host API reports that code with its generic public message, as
it does for tape conversion errors.

Run `tapeA1` (09:09 to 09:21 EET, same setup) ran the four sealed archives
with the turbo-loader `.tzx` of the
[2026-10-22 record](2026-10-22-spectrum-tape-ddr.md), which now passes
through the snapshot probe before the deck. Each model loaded it from DDR with
`LOAD ""` (48K) or its menu's tape loader, and a grabbed frame showed all eight
stripes. A 20-second capture had no HDMI relock, and `0xBA` read `0x20`
(read only). The software was restored to `35d58489` after each run, the
image fingerprints matched before and after, and kit A was released at 09:08
and 09:21.

## Not established

- Saving snapshots, `.szx`, TR-DOS (`.trd`/`.scl`) and the +3 disk.
- IFF2 independent of IFF1, and the T state within the frame: neither is
  restored.
- Game play beyond the first screen; the runs judge one grabbed frame per
  snapshot.
