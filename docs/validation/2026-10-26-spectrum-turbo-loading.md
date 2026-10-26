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

Source FES `97012888` (`sources/misteross`), producers
`scripts/build_fes_spectrum_oss.py --model {48,128,plus3,pentagon}`, lock
`cores/fes-spectrum/toolchain.lock` (nextpnr main `bd7a1c0d`, unchanged), HIP
router, first-pass placement search in each recipe's seed order. Every core
is constrained to a 52.224 MHz system clock, the 74.25 MHz pixel clock and a
12.288 MHz audio clock.

These seals replace the first turbo seals, built from `a8e165fc`. That source
had the Beta 128's Force Interrupt I2 raising INTRQ as the index pulse
starts; Fuse 1.6.0 raises it as the pulse ends, and `97012888` does the same.
That is the only change to the Spectrum RTL between the two sources.

| Core | Seed | System MHz | Pixel MHz | Audio MHz | COMB | FF | M10K | MUL9X9 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fes.spectrum` 0.6.0 | 4 | 63.49 | 95.00 | 169.61 | 9,395 | 4,821 | 129 (16 TDP) | 3 |
| `fes.spectrum128` 0.4.0 | 5 | 63.01 | 84.58 | 184.43 | 10,500 | 5,092 | 225 (32 TDP) | 3 |
| `fes.spectrum-plus3` 0.5.0 | 4 | 53.00 | 87.80 | 175.75 | 13,991 | 6,650 | 257 (32 TDP) | 3 |
| `fes.pentagon` 0.5.0 | 2 | 59.24 | 84.57 | 199.56 | 12,098 | 6,273 | 241 (32 TDP) | 3 |

- `fes.spectrum` 0.6.0: package `1e23466daf87ca32864bcf7133465f4904d2a3b8b68190bfe70385e43af5d72b`, build `fe5e6141ed40ec7cf6c3e34ceb382052`, RBF sha256 `98e256135e56d3f0f0ff6639c3bd308503ce6c19f7dbc09272a497f8fa2391f1` (2,637,984 bytes)
- `fes.spectrum128` 0.4.0: package `4a56eee238edb83566b2f8cd1118f7880a4966d29a8c6994a236e781a1636e4e`, build `0b93c88ce58f55d25aaa4a37de41cc53`, RBF sha256 `cc70ff8309fcd2015d60909a6790fc1957f492c4a73a57ecfe4b6c399cc817ac` (2,999,672 bytes)
- `fes.spectrum-plus3` 0.5.0: package `9b653a2048ec27fc5c10347885e8b4e39d067799353d74aa52ab763f0ae8507d`, build `9299fb91e1b68d3b25888720c53d235c`, RBF sha256 `c68fa392387a4ccdf68a1a75e2f0f16ee35c0e513c4cc9b91266ed78ed445472` (3,207,104 bytes)
- `fes.pentagon` 0.5.0: package `0e07c46e38f3f30a3413d9dc9d3d4cc79bb5b504ac7ae24bc9f3cecd7eb19ee6`, build `4a317ae75020cd8e11c97d9a2bc54ac8`, RBF sha256 `1223225a197621e9fb05c32c0a591d50ae24903843eb52fc8f2decae0a0938f9` (3,054,843 bytes)

The system clock closed with less margin than the +3 disk seals (70.67,
66.14, 61.27 and 57.81 MHz then). No system critical path runs through the
ULA's clock or screen fetch: the 48K's is inside the CPU's state decode, the
128K's the AY into the audio mix, the +2A/+3's the uPD765's CRC into its
interrupt, the Pentagon's the tape deck's state into its pulse preset. No
check was relaxed.

## Kit check

The kit runs below were made on the first turbo seals from `a8e165fc`:
`fes.spectrum` `eb002504`, `fes.spectrum128` `f07a38f5`, `fes.spectrum-plus3`
`4d501a44` and `fes.pentagon` `0afd828c`. Their RTL differs from the seals
above only in when the Pentagon's Beta 128 raises INTRQ for Force Interrupt
I2, which TR-DOS 5.03 does not issue. The seals above were not run on a kit.

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

## Not established

- Full-length game loads at the model's speed in simulation (still running),
  and loaders other than the three games above.
- A switch to load at the machine's own speed, for games that play music or
  a game while they load.
