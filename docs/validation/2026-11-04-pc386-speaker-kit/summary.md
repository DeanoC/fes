# pc386: Ultima VI's lute on Kit B with PIT counter 2 programmed, 4 November 2026

[#1457](https://github.com/DeanoC/fes/issues/1457), kit check of
[#1458](https://github.com/DeanoC/fes/pull/1458). POST now programs PIT
counter 2 as an AT leaves it ([record](../2026-11-03-pc386-speaker-timer/summary.md)).
#1458 merged without a seal under the pc386 reseal exemption, and no kit had
run it.

**Result.** Ultima VI's CPU-timed speaker routine (Iolo's lute) now plays
tones on the kit. Each key played a note, 70 of 70. Each note is a gated
~19.7 kHz carrier about 100 ms long, and its envelope pitch depends on the
key. #1453's `u6i` run found nothing tonal on either package
([ISA bus time record](../2026-11-03-pc386-isa-bus-time/summary.md#the-cpu-timed-routine-run-u6i)).
The stray speaker pulses are **not** gone. They are now short bursts, about
3 a second outside the notes against 0.5-0.7 before, on the same ~58.6 ms grid as the old clicks. They come before the
lute starts as well as between notes.

## Inputs

- **Seal.** `make build-fes-pc386-quartus` at seed 1 from a clean checkout
  of main `bb51f4454`, which holds #1458 (`c25e8de27`) and #1453
  (`0686f5601`) ([`seal-summary.json`](seal-summary.json)). Quartus Prime
  Lite 17.0.2 on the pc386 lock's Yosys `0a7927bf` netlist. The checkout's
  dev86 tree came from a copy of another worktree's pinned `0b72e907`, not
  from a download.
  - **Firmware.** BIOS `8057fada97565abb…`, the image the #1458 record names.
    The VGA BIOS (`69401dc8…`) is unchanged.
  - **Identity.** Build `ac127806e1b578006784bc24d2dc17ce`, package
    `a8c5a3930755b699b8fe749d5b110eb5b9f2530934d08c86831a42cfc15b0879`,
    RBF 3,626,664 bytes, SHA-256 `9ca76fa3…`.
  - **Timing.** Every analysis passes on every clock: setup +1.055 ns, hold
    +0.079 ns, recovery +8.348 ns, removal +0.272 ns, minimum pulse width
    +1.122 ns. Clocks: system 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.
  - **Coverage.** All 109 pinned inputs hash the same at this record's head.
  - **Resources.** 28,976 ALMs of 41,910, 26,188 registers, 259 RAM blocks
    and 36 DSP blocks.
  - Quartus took 8 min 21 s (45 CPU-minutes) on an otherwise idle Powerboat.
    The fitter took 5 min 17 s, against 3 h 5 min for #1453's seal beside
    another seal. It was a full compile, not a rapid recompile.
- **Disk.** #1453's `u6-speaker` disk (`00246ed2…`): the FreeDOS games image
  plus the user-supplied Ultima VI from eXoDOS, with `CONFIG.U6` set to no
  music, so every sound goes through the PC speaker. The disk, the WAV and
  the frames stay on Powerboat. Only measurements are here.

## Kit B run `k1`

Run `k1`, 4 November 08:08-08:17 EET, claimed and released in
`kitB-STATUS.md`. It was core-only on the kit's baseline software `a8cd30c4`
(main `bb51f445`), image `f904961e`. Nothing was installed, and there was no
reboot or host change. The post-run snapshot matched the pre-run one. The
agent launched the package as a fresh library game under a kit lease at the
286-12 preset (record word 0 = 1). The driver was #1453's `u6i` flow
unchanged: new game, character creation, then the opening fight at the
shrine.

At the first `Avatar:` prompt: F4 (Iolo's inventory), U, Tab, Right, Enter.
The grab shows `Use-lute`. Then 70 digit keys 0.66 s apart: 30 × `6`,
30 × `1`, then `1`-`9`, `0`. Then Esc. HDMI audio was recorded (ALSA,
48 kHz) from 2 s before Enter to 2 s after Esc: 62.0 s, WAV `a13d2026…`.

### Tones (`u6_tones.py`, 5 ms frames, RMS threshold 300)

There are 74 events: 70 during the keys and 4 in the 2 s after Esc, when the
fight goes on. Those 4 are left unidentified: 919 Hz for 170 ms, about 4 kHz
for 70 ms, 587 Hz for 150 ms, and about 4 kHz for 70 ms. They are not
`u6i`'s 213 Hz PIT tones. Every key gave exactly one note, 0.66 s apart (the
median gap between note starts). Each note:

- is 100-110 ms long, with RMS 4,408-4,608;
- has a raw edge rate of 18.5-20.2 kHz: the routine's PIT counter 2 carrier
  (divisor 60, 19.9 kHz), sampled at 48 kHz;
- has an envelope pitch that depends on the key. The envelope is |signal|
  smoothed over 21 samples, the rate at which the CPU-timed loop gates the
  carrier.

| Key | Notes | Envelope pitch (median) |
| --- | ---: | ---: |
| `6` | 30 + 1 | 839.7 Hz (837.8-842.1) |
| `1` | 30 + 1 | 501.4 Hz |
| `2` | 1 | 565.9 Hz |
| `3` | 1 | 634.2 Hz |
| `4` | 1 | 675.6 Hz |
| `5` | 1 | 749.4 Hz |
| `7` | 1 | 948.5 Hz |
| `8` | 1 | 998.7 Hz |
| `9` | 1 | 1,099.4 Hz |
| `0` | 1 | 1,232.6 Hz |

The repeated notes hold their pitch to about ±0.3% (the `6`s 837.8-842.1 Hz).
Steps between neighbouring keys are 1.13, 1.12, 1.07, 1.11, 1.12, 1.13, 1.05,
1.10 and 1.12. That is whole, whole, half, whole, whole, whole, half, whole,
whole: a major scale from `1`, played by the CPU loop. Whether the tonic is
in tune is not measured here (see "Not done here").

### Clicks (`u6_clicks.py`)

The click detector finds 257 excursions over 3,000 LSB. 76 fall inside the
notes. The other 181 do not. 37 of them come before the first note (11.0 s), and 13 come in the
first 4 s, while the inventory is open, around Enter (2 s). They are not
#1453's 2-4-sample single steps. Each is a burst of pulses 2-11 samples long, near
+16,000, lasting 0.3-1 ms. Their spacing is close to multiples of 58.6 ms:
57-61, 117-119 and 174-178 ms are the most common gaps.

| Capture | Package | Length | Hits outside notes | Rate | In the first 4 s |
| --- | --- | ---: | ---: | ---: | ---: |
| `k1` (this run) | main `bb51f445`, `a8c5a393` | 62.0 s | 181 (of 257) | 2.9/s | 13 |
| `u6i`, 286 | #1453 branch, `c86c5ebf` | 34.5 s | 24 | 0.7/s | 2 |
| `u6i`, 286 b | #1453 branch, `c86c5ebf` | 48.0 s | 33 | 0.7/s | 1 |
| `u6i`, 286 b | main before, `c7e06e04` | 48.0 s | 23 | 0.5/s | 1 |

The `u6i` captures had no notes, so every hit there counts. The first 4 s of
each capture comes before any note, from the inventory screen to Enter.

#1453's record already saw clicks pairing 60-62 ms and 118-119 ms apart, so
these bursts come from the same source. Now that counter 2 runs, it is
audible more often. The bursts also come while the game waits at the
inventory and in the lute's key loop, so they do not come from the lute
routine's notes. Their source is not found here.

## Not done here

- **The source of the 58.6 ms bursts.** Open. An idle-time speaker path in
  the game, or the machine's port 61h/PIT, would fit what is seen. Telling
  them apart needs a port 61h/43h/42h trace of the same scene, from the
  bench or Bochs.
- **#1453's pitch comparison.** #1453 merged before this run. The ~2.5% drop
  that its ISA charges predict for this routine needs a second package,
  sealed at `c25e8de27` (#1458 without #1453), run through the same flow.
  Neither earlier package (`c86c5ebf`, `c7e06e04`) plays tones, so neither
  can be the "before". This record gives the absolute pitches on main only.
- **Whether the scale is in tune.** That needs the routine's note steps and
  a reference machine's pitch (Bochs, or a real 286-12).
- **Kit leftovers.** The run left 10 paths in FogCast's data directories on
  Kit B: the game's record and its disk delta (listed in the status log).
  The staged game disk was already on the kit from #1453's runs. None of
  these have been removed.
