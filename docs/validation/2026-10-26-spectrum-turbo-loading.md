# 2026-10-26: Spectrum turbo tape loading, seals and checks

Record of turbo tape loading on the four Spectrum models (#999): the machine
runs its T states faster while the tape plays and is unchanged in T states,
the simulations behind that, and routed, timing-closed bitstreams of all four
models. It is not acceptance of an image or release.

## Design

While the deck plays (Fuse's loader detection starts and stops it),
`rtl/spectrum_ula.sv` runs each T state in 5 clocks of the 52.224 MHz system
clock instead of 14 or 15: the CPU runs at 10.44 MHz, 2.98 times the 48K and
Pentagon and 2.94 times the 128K-class models. Speed is chosen at each T
state's tick_p; everything else is counted in T states, so contention, the
frame interrupt, the screen and floating bus, the tape pulses, the AY and the
disk controllers' timers are unchanged. The machine returns to its own speed
at the T state after the deck stops. The shell mutes the sound in turbo
(`spectrum_audio.v` `mute`); HDMI keeps showing the newest complete frame, so
the picture skips frames.

5 clocks is the shortest exact T state. `fes_z80` presents a new bus cycle
two clocks after ce_p, and the ULA registers the contention request a clock
before tick_n, so tick_n comes three clocks after tick_p; ce_n and ce_p may
not be on consecutive clocks. A 4-clock T state was built and missed the
first contended write of the 48K diagnostic by one T state (the request was
registered a clock late); making ce_n combinational on the CPU's bus state
would put a long path into every model's system clock.

The screen fetch was restructured so reads keep their order against CPU
writes at either speed. A CPU write reaches RAM two clocks after its T2
ce_n: clock 9 of that T state at the model's speed, in turbo the end of the
next T state's tick_p clock. The ULA now reads at the ends of clocks 1 to 3
of a T state: the floating-bus byte of the following T state, then the
chunk's bitmap and attribute, with banks 5 and 7 read in parallel
(`spectrum_ram.v` has a screen port per bank). Before, it read up to nine
clocks into a T state, one bank after the other. The floating byte is read a
T state early, which no IN can tell: a write or OUT is at least three T
states before an IN samples the bus.

Versions: `fes.spectrum` 0.6.0, `fes.spectrum128` 0.4.0,
`fes.spectrum-plus3` 0.5.0 and `fes.pentagon` 0.5.0. Interfaces and M10K
counts are unchanged.

## Host checks

- `make sim-fes-spectrum` passes. Each machine and snapshot simulation now
  runs twice, at the model's speed (where the diagnostic tape itself plays in
  turbo) and with every T state in turbo (`--turbo`), and must give the same
  result: marker T states 14950, 14960, 14955 and 14946, the tape, paging, AY,
  keyboard and probe checks, and every restored register and RAM byte. The
  machine simulation now takes the marker from its write cycle's T2 on the
  bus; RAM changes a clock later, which in turbo falls in the next T state.
- `run_tb` with the diagnostic firmware gives the same instruction trace (PC
  and frame T state at every instruction: 61,267 lines over 8 frames on the
  48K, 89,633 over 12 frames on the Pentagon) at the model's speed and with
  `--turbo`, on the final build.
  Those traces were matched to Fuse 1.6.0 at the model's speed, so the turbo
  machine follows Fuse the same way.
- With the real 48K and 128K ROMs (held locally, never committed), Manic
  Miner (48K) and Head Over Heels (128K, custom loader) loading from their
  `.tzx` gave the same frames on the pre-turbo build (`62d69987`) and the
  turbo build through frame 1500. Manic Miner with every T state in turbo
  matched the turbo build through frame 7500, mid-load. Full-length runs at
  the model's speed take over three hours in simulation, and they had not
  finished when this record was written.
- The producer tests pass.

## Artifacts

Source FES `9da56232` (`sources/misteross`), producers
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`, unchanged), HIP
router, first-pass placement search in each recipe's seed order. Every core
is constrained to a 52.224 MHz system clock, the 74.25 MHz pixel clock and a
12.288 MHz audio clock.

These seals replace the first turbo seals, built from `a8e165fc`. Two
controller fixes came between the two sources, and they are the only
changes to the Spectrum RTL:

- The Beta 128's Force Interrupt I2 raised INTRQ as the index pulse started;
  it now raises it as the pulse ends, as Fuse 1.6.0 does.
- The uPD765's Sense Interrupt Status left the seek interrupt pending after
  returning its status; it now clears it unless another unit still has a
  completed seek, as Fuse does.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.6.0 | 5 | 59.30 | 92.16 | 231.80 | 9,443 | 4,821 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.4.0 | 5 | 65.59 | 89.12 | 189.83 | 10,491 | 5,092 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.5.0 | 4 | 58.85 | 95.95 | 229.89 | 14,712 | 6,650 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.5.0 | 5 | 64.53 | 81.38 | 154.51 | 12,114 | 6,273 | 241 (32 TDP) | 3 |

- `fes.spectrum` 0.6.0: package `057a422d1b7869608f122cd2c07b3615525be6d7ac9e9db776cf84c258e91acb`, build `6a69d61b0b453c7b45c928ecb340f2d3`, RBF sha256 `c24df2181fdac2060fb4733273f2a594fbe3771e5334f8f5a1aa81d9138b1e0d` (2,623,604 bytes)
- `fes.spectrum128` 0.4.0: package `ee164e9f26ffb24ca3a3db8ac6274fe8e1b368a187f44561b1bc998b1db80015`, build `5c8f2002b38012ebed7125624679d0ea`, RBF sha256 `008f8435dd3242707cec788dc9f7f0d47340876130b4f9ba0abc9d7749dfeddb` (3,011,808 bytes)
- `fes.spectrum-plus3` 0.5.0: package `a1fa87c585f87b8719cd566cb7f316d63bdfd2ca3246832cb22de1571c47e1a7`, build `9b0ab58102e38d9d281e6aed8039cbd0`, RBF sha256 `96d4ef62245904d329fda12a690e8739d6bf89143f5bcbb5566d46fe6558609f` (3,180,659 bytes)
- `fes.pentagon` 0.5.0: package `effa14fb5abb77e4e47d9f69fcfb2760bda215857db24c3608fa91b430175f71`, build `b172d9cdc6529ea18c651c3bb4a7155a`, RBF sha256 `48103181e4506942c38c0d1b1bb6faec971bbefae9907c41e95ecf9069e8a3d0` (3,071,581 bytes)

The system clock closed with less margin than the +3 disk seals (70.67,
66.14, 61.27 and 57.81 MHz then). No system critical path runs through the
ULA's clock or screen fetch: the 48K's, 128K's and +2A/+3's are inside the
CPU's state decode, the Pentagon's the AY into the audio mix. No check was
relaxed.

## Kit check

The kit runs below were made on the first turbo seals from `a8e165fc`:
`fes.spectrum` `eb002504`, `fes.spectrum128` `f07a38f5`, `fes.spectrum-plus3`
`4d501a44` and `fes.pentagon` `0afd828c`. Their RTL differs from the seals
above only in the two controller fixes: the Pentagon's Force Interrupt I2,
which TR-DOS 5.03 does not issue, and the +2A/+3's Sense Interrupt after a
seek. Run `i2B2` below repeated the disk and snapshot checks on the seals
above.

Kit B, on the running image `4db74864` with the stacked branches' kit
software (bundle `c8b2a0c5`, built from `a5de9abb`; FogCast and the runtime
are unchanged since) installed and activated, and a private FogCast host
built from `a5de9abb`. Bundle install-then-restore: after each run the kit
software is back on `35d58489`, and the image, boot state, idle and launcher
files and card image fingerprints are as found. The kit was released idle
and lease-free.

- Run `tbB1` (00:30-00:44 EET) loaded the diagnostic turbo-loader `.tzx` on
  the previous seals (the +3 disk record's packages). The time from `LOAD`
  being typed to the first frame showing its eight stripes was 10.5 s
  (48K), 9.7 s (128K), 15.7 s (+2A/+3) and 9.7 s (Pentagon). In that run each
  turbo package failed to create its library entry (`STALE_REVISION`). The
  same host already held an entry for another package of the same core,
  which appears to be the cause: run `tbB3`, one package per core and host,
  created every entry.
- Run `tbB2` was interrupted during its first session's setup; no session
  launched, and the kit was restored by hand and logged.
- Run `tbB3` (06:14-06:38 EET), every automatic check passed
  (`HIL_VERDICT PASS`):
  - The four turbo packages loaded the same tape to its stripes in 2.5 s
    (48K), 2.7 s (128K), 7.7 s (+2A/+3, whose menu and tape loader take
    longer before the loader runs) and 2.7 s (Pentagon), judged at one frame
    a second. HDMI held: ADV7513 `0xBA` = `0x20` and no relocks in every
    session.
  - `fes.spectrum` 0.6.0 loaded Manic Miner (standard loader, 203 s of
    tape) to its title screen about 70 s after `LOAD ""`, and its attract
    mode then ran the caverns. It loaded Bubble Bobble (Firebird, 188 turbo
    blocks, 358 s of tape) to its control menu in about 130 s.
  - `fes.spectrum128` 0.4.0 loaded Head Over Heels (Ocean, custom
    pulse-sequence loader, 244 s of tape) through its Tape Loader to the
    joystick menu in about 90 s.
  - Bubble Bobble's menu has a black border, so the dropout count reads its
    whole recording as black frames; `0xBA` and the recording show HDMI held.
  - The deck's tape stream is read from DDR about three times as fast in
    turbo. It has no underrun counter on the kit, so the evidence is that
    every tape loaded.

- Run `tbB4` (06:43-07:06 EET) repeated the +3 disk record's disk and
  snapshot checks on these packages, and every automatic check passed
  (`HIL_VERDICT PASS`):
  - `fes.spectrum-plus3` 0.5.0 booted Bubble Bobble, Batman: The Caped
    Crusader and Beyond the Ice Palace from `.dsk` to the same screens as
    before, and Chase H.Q. II inserted live changed only the medium.
  - `fes.pentagon` 0.5.0 booted `tetris.trd` and refused a `.dsk`.
    `fes.spectrum` 0.6.0 refused a disk.
  - Every model restored the same `.z80` and `.sna` games as before, live
    inserts included, and the 48K, 128K and Pentagon refused a class they do
    not run.

- Run `tbB5` (07:45-08:04 EET) recorded the HDMI audio as well (the capture
  card's ALSA device, 48 kHz), and every automatic check passed. Loudness is
  per-second RMS in signed 16-bit units; the capture's floor is about 60.
  - Manic Miner on the previous 48K seal: the tape's sound at about 2,340
    through the load, silent only in its gaps.
  - The same tape on `fes.spectrum` 0.6.0: silent through the turbo load
    apart from blips up to 270 around typing `LOAD` and in the tape's first
    gaps, then the title's beeper tune at 2,000-6,000 from the end of the load
    (79 s). The mute holds while the deck plays, and sound returns when it
    stops.
  - Head Over Heels on `fes.spectrum128` 0.4.0 stayed silent through its load
    and joystick menu.
  - The Bubble Dizzy 128K snapshot on `fes.spectrum128` 0.4.0 and
    `fes.pentagon` 0.5.0 played its title music at about 2,500 for the first
    10 s, then went silent with the title still showing (the script presses
    ENTER at 10 s; why the music stopped is not established). Fuse's save of
    the same title has the AY playing three tone channels at volumes 12-15,
    so this is the AY heard on hardware.

- Run `i2B2` (16:34-16:57 EET) repeated run `tbB4`'s disk and snapshot
  checks on the seals from `9da56232`, with the same kit software and a
  private FogCast host built from `6ff4e487`, and every automatic check
  passed (`HIL_VERDICT PASS`):
  - `fes.spectrum-plus3` 0.5.0 booted Bubble Bobble, Batman: The Caped
    Crusader and Beyond the Ice Palace from `.dsk` to the same screens as
    before, and Chase H.Q. II inserted live changed only the medium.
  - `fes.pentagon` 0.5.0 booted `tetris.trd` to its menu and refused a
    `.dsk`. `fes.spectrum` 0.6.0 refused a disk.
  - Every model restored the same `.z80` and `.sna` games as before, live
    inserts included, to the same screens, and the 48K, 128K and Pentagon
    refused a class they do not run.
  - Kit B was restored to software `35d58489` on image `4db74864`, with the
    card images and launcher files as found, and released idle and
    lease-free.

## Not established

- Full-length game loads at the model's speed in simulation (still running),
  and loaders other than the three games above.
- A switch to load at the machine's own speed, for games that play music or
  a game while they load.
