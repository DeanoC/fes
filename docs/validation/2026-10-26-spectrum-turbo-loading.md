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
  and frame T state at every instruction, 61,267 lines over 8 frames on the
  48K; 12 frames on the Pentagon) at the model's speed and with `--turbo`.
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

Source FES `a8e165fc` (`sources/misteross`), producers
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`, unchanged), HIP
router, first-pass placement search in each recipe's seed order. Every core
is constrained to a 52.224 MHz system clock, the 74.25 MHz pixel clock and a
12.288 MHz audio clock.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.6.0 | 5 | 57.46 | 86.34 | 208.07 | 9,451 | 4,821 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.4.0 | 5 | 57.69 | 88.71 | 186.57 | 10,504 | 5,092 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.5.0 | 5 | 53.75 | 79.83 | 196.66 | 13,950 | 6,650 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.5.0 | 5 | 63.33 | 92.76 | 194.25 | 12,126 | 6,273 | 241 (32 TDP) | 3 |

- `fes.spectrum` 0.6.0: package `eb0025040f8c0807706cf9505fbe08984d24cd5fe2443827334dd0c4ee8f4e5a`, build `da59f2b55836b0bd609dabd44e520287`, RBF sha256 `209957577b080f3f18723e75d26c10c828ae9b82b47d1975c020dbbfbc5515d5` (2,641,750 bytes)
- `fes.spectrum128` 0.4.0: package `f07a38f54f29dfe1696066766e73afe7cd311821d1127048c1bf6eeadc5a8212`, build `17913ec8a3bdb2395f4a04c8a233e124`, RBF sha256 `db53dd48b8080c0ce2bed0d4e94125d487afaf660940c2e5379681f48aa6e761` (2,949,562 bytes)
- `fes.spectrum-plus3` 0.5.0: package `4d501a44e4a113d0b01a4d06ca188b622d37d8b79d40380fc974554cbc15aff5`, build `e7670815327393e08e9a624712a59304`, RBF sha256 `7c058ce60c81442d0a2b4687531c68d0e3ef480b471c1bde3a0eac30eceb841b` (3,151,739 bytes)
- `fes.pentagon` 0.5.0: package `0afd828cc7f29f37fc92e04c28ee064618f8d03d0d7e464ca3cb2fdb69a6468c`, build `77c4f449f474d070798c4a1abfa2b685`, RBF sha256 `d70f4767ef5a42b7c0d533537caaa573b0cd82e575a353cd6b474bf09474feb1` (3,015,029 bytes)

The system clock closed with less margin than the +3 disk seals (70.67,
66.14, 61.27 and 57.81 MHz then). No system critical path runs through the
ULA's clock or screen fetch: the 48K's is the tape store into the mailbox,
the 128K's the AY into the audio mix, the +2A/+3's the disk drive's fetch
address into its weak-byte enable, the Pentagon's the CPU into the Beta 128.
No check was relaxed.

## Kit check

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
  turbo package failed to create its library entry (`STALE_REVISION`), as the
  same host already held an entry for another package of the same core, so
  run `tbB3` tested one package per core.
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

## Not established

- Sound muting in turbo: no simulation checks it, and the kit recordings
  carry no audio.
- Full-length game loads at the model's speed in simulation (still running),
  and loaders other than the three games above.
- A switch to load at the machine's own speed, for games that play music or
  a game while they load.
