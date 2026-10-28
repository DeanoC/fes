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

Source FES `7b99fc50` (`sources/misteross`), this branch with main merged, producers
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`, unchanged), HIP
router, first-pass placement search in each recipe's seed order. Every core
is constrained to a 52.224 MHz system clock, the 74.25 MHz pixel clock and a
12.288 MHz audio clock.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.5.0 | 4 | 65.19 | 83.70 | 209.38 | 9,484 | 4,821 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.3.0 | 4 | 70.81 | 96.11 | 156.52 | 10,476 | 5,083 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.3.0 | 5 | 66.84 | 91.30 | 174.70 | 10,472 | 5,086 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.3.0 | 5 | 60.68 | 92.70 | 183.18 | 10,341 | 5,069 | 225 (32 TDP) | 3 |

- `fes.spectrum` 0.5.0: package `08ada8a6742c192142e702d11f801cb94cd3b59bbf81ff68ac8275e6f9c35988`, build `650aef9b6e03d696ff9f364277fdf39f`, RBF sha256 `1a4261944ba75523aac22a6c9251687b755fdee763082b8e42c7566f955382dc` (2,658,755 bytes)
- `fes.spectrum128` 0.3.0: package `0320fe38269c73f1cb8bd1456aaa1676771cb9eb9b27ec530ab94feaf9f68bd6`, build `65732cf067424677f6a01afe7c39fb57`, RBF sha256 `e06ee459035cc86b671b53f1381753858ddbfc932a693eecffde1d3bfa5a032c` (2,934,844 bytes)
- `fes.spectrum-plus3` 0.3.0: package `b71f5edc562e5c9061a99ae4d248d0e229945d387661486c021fb77d39f730ce`, build `33b7e2e35b1a5b687411855a6267727d`, RBF sha256 `933dc50f833187a13c788fa50c51406b0c3b8d6fd15db4512f628c1eadd93efa` (3,090,740 bytes)
- `fes.pentagon` 0.3.0: package `3eec9f7a13714c64998d19d174645cb860175ecb785319c002b808bb678f4fb5`, build `52350ce89dcbeb4154bd2e3e0ca5150a`, RBF sha256 `9791370f1392de61ec92ce9d35b0b79c43b1e6ffddfd478133a97ce0809fcfe8` (3,022,761 bytes)

The +2A/+3 and Pentagon closed on their recipe's first seed (5). The 48K
closed on seed 4 after seed 5 reached 50.76 MHz (0.972 of the system clock),
and the 128K on seed 4 after seed 5 did not finish routing in its lease.

Superseded seals of these versions, from `a520345a` before main was merged
(main changed `scripts/pnr_recipe.py`, which is in the producers' audited
closure): `dcf326ed…`, `52651175…`, `9a7a7236…` and `35bd7a01…`, the
archives of kit A runs `snapA1` and `tapeA1`.

## Kit A check

Run `snapA2` (10:11 to 10:25 EET) ran the four sealed archives on kit A. The
running image `4db74864` was not changed. Kit software `115494f3…` (built from
`main` `0f3b35c9`, still retained by the kit) was activated over the baseline
`35d58489` and restored afterwards; the kit runtime has no change in this
branch. A private host built from `7b99fc50` imported each archive, checked
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

Every grabbed frame matched the same snapshot's frame from run `snapA1` on
the superseded archives (SSIM 1.000; 0.994 for the animated Fast Food Dizzy
title).

Selecting a snapshot of a machine the package does not run was refused with
`BAD_REQUEST` on the 48K (a 128K snapshot), the 128K and the Pentagon (a +3
snapshot); the host API reports that code with its generic public message, as
it does for tape conversion errors.

Run `tapeA2` (10:25 to 10:38 EET, same setup) ran the four sealed archives
with the turbo-loader `.tzx` of the
[2026-10-22 record](2026-10-22-spectrum-tape-ddr.md), which now passes
through the snapshot probe before the deck. Each model loaded it from DDR with
`LOAD ""` (48K) or its menu's tape loader, and a grabbed frame showed all eight
stripes. A 20-second capture had no HDMI relock, and `0xBA` read `0x20`
(read only). The software was restored to `35d58489` after each run, the
image fingerprints matched before and after, and kit A was released after
each run. Runs `snapA1` and `tapeA1` (08:54 to 09:21) passed the same checks
on the superseded archives.

## Not established

- Saving snapshots, `.szx`, TR-DOS (`.trd`/`.scl`) and the +3 disk.
- IFF2 independent of IFF1, and the T state within the frame: neither is
  restored.
- Game play beyond the first screen; the runs judge one grabbed frame per
  snapshot.
