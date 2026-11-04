# pc386 Sound Blaster: ADPCM DMA playback, 4 November 2026

Issue [#1455](https://github.com/DeanoC/fes/issues/1455): Serpent Isle's
introduction goes black about 20 s in, on Kit B and on the machine bench.
The [diagnosis](https://github.com/DeanoC/fes/issues/1455#issuecomment-6616283737)
found the cause: the narration is 4-bit ADPCM (DSP 75h then 74h), the card
ignored the ADPCM commands, and the game waits for each block's IRQ.

**Result:** `pc386_sb.sv` now plays every SB 2.0 ADPCM DMA mode: 4-bit, 2.6-bit
and 2-bit, single-cycle and auto-init, with and without a reference byte.
Its decoders match DOSBox-X's. `audio_tb` checks each mode against a
reference decoder built from DOSBox-X's tables; the DAC matches it every
sample period. On the machine bench (Bochs CPU), Serpent Isle's 75h block
now completes after its 9,576 bytes and raises IRQ 7. The driver then chains
blocks, with 15 IRQs by 39 s where main had stopped at 2.
From bench 21 s, where main goes flat black for good, the intro's frames
change every second through 26 s, the end of the run.
The seal at `36ea8a2f3` (Quartus 17.0.2, package `de3173af…`) meets every
analysis at 61.44 MHz. The pc386 suites (system, boot, governor, sound,
audio) and `make test-changed` pass. No kit run has been made yet.

## What changed

- **Commands.** 74h/75h (4-bit), 76h/77h (2.6-bit) and 16h/17h (2-bit)
  start single-cycle ADPCM DMA for the length given; the odd command of each
  pair starts with a reference byte. 7Dh, 7Fh and 1Fh are auto-init with
  48h's block size and a reference byte. Before, all of these were consumed
  and ignored.
- **Decoding.** A byte holds 2, 3 or 4 codes, high bits first; 2.6-bit's third
  code is the low two bits doubled. Each code steps an 8-bit reference by a
  signed delta from its magnitude and the step row, clamped to 00h–FFh, and
  moves the row, as DOSBox-X's `decode_ADPCM_4/3/2_sample` do. The RTL works
  the delta out per row ((2m + 1) << (r − 1), and (2m + 1) × 5 for 2.6-bit's
  top row) rather than holding DOSBox-X's tables.
- **Timing.** The first code plays when its byte arrives, the others on the
  following sample periods, so the card takes one DMA byte per 2, 3 or 4
  sample periods. The IRQ comes with the block's last byte, after
  length + 1 bytes, as for PCM; that byte's later codes still play, in
  its encoding. A transfer started meanwhile, such as the next block started
  at that IRQ as Serpent Isle's driver does, takes its first byte after them.
  So a block without a reference continues from the true reference and step
  (Codex's review: the first version dropped them).
- **Choices where DOSBox-X is no guide or differs:**
  - A reference byte plays as itself and holds the DAC for its byte's
    periods; DOSBox-X plays no sample for it. The byte rate, and so the IRQ
    time, is the same.
  - Auto-init takes its reference byte only at the start, as DOSBox-X does.
  - Forms without a reference continue the previous reference and step.
    A DSP reset sets them to 80h and 0; DOSBox-X keeps them over a reset.
  - Creative's ADPCM rate limits (12 kHz for 4-bit, which DOSBox-X applies
    on an SB 2.0) are not applied, just as the PCM limits are not.
    Serpent Isle's narration runs at time constant B9h (14.1 kHz), so on the
    machine its blocks end about 15% sooner than under DOSBox-X's limit.
- **8-bit PCM** is unchanged: every PCM command starts with the PCM encoding,
  and the audio suite's PCM checks pass as before.
- **Docs.** The RTL header and the machine design's Sound Blaster section
  describe the modes. The "not modelled" Audio row no longer lists ADPCM, and
  the line deferring ADPCM ("none of the acceptance games uses it") is gone.

## Inputs

- **Branch.** FES `deano/pc386-sb-adpcm`, from main `bb51f4454`:
  `4395fabee` (the RTL and `audio_tb`), `17d84eca1` (the design doc),
  `a6bfe4a04` and `f8dea53b0` (this record). `2d6d77a58` merges main
  `2dcfa05ca`, which brings
  [#1452](https://github.com/DeanoC/fes/pull/1452)'s BIOS change (INT 15h
  C2xx) and [#1472](https://github.com/DeanoC/fes/pull/1472)'s kit record.
  `36ea8a2f3` fixes the chained-block tail Codex found.
- **Seal.** `fes.pc386` from a clean checkout of `36ea8a2f3`, which holds
  the merged BIOS ([Seal](#seal)). The first seal, at `17d84eca1`
  (package `73e9fa9d…`, setup +1.236 ns), is superseded.

## Checks

- **`audio_tb` (134 checks, 45 new).** Each check runs at 220h, IRQ 7; the
  single-cycle and auto-init ones at 50 kHz:
  - **Single-cycle, per encoding.** First the reference form (75h, 77h, 17h)
    for 40 bytes, then the plain form (74h, 76h, 16h) for 24 bytes, which
    continues the reference and step left by the first block. For each block:
    - IRQ 7 comes with the block's last (length + 1th) acknowledge;
    - nothing more is acknowledged after it;
    - acknowledges are 4, 3 or 2 sample periods apart, within ±30 clocks;
    - the DAC, sampled in the middle of every period from the first
      acknowledge, equals the reference decoder, the last byte's later codes
      included.
  - **Chained at the IRQ.** 75h then 74h, 77h then 76h, 17h then 16h,
    with the second command sent as soon as the first block's IRQ is taken.
    The run is at 5 kHz, so the command lands within the last byte's
    periods, and the boundary falls in random data. One 8237 block covers
    both blocks. The DAC equals the continuous reference decode across the
    boundary, and the second block's first acknowledge comes only after the
    last byte's codes. Before the fix these failed (4-bit: period 51 read
    00h, expected 13h).
  - **Coverage.** The streams climb to the top step row and hit both
    reference clamps; a check fails if they don't.
  - **Auto-init (7Dh, 7Fh, 1Fh).** With 48h's block of 12 bytes over a
    20-byte 8237 auto-init buffer, an IRQ comes every 12 acknowledges, 4 times.
    The DAC equals the reference decoder with the reference byte only at the
    start, and the buffer's wrap decodes as data. DAh ends it after the block.
  - **Reference decoder.** It uses DOSBox-X's scaleMap and adjustMap values
    as tables, apart from the RTL's per-row formula. A script compared the
    tables with DOSBox-X 2025.02.01's `sblaster.cpp`: all six match.
  - **Mutations.** Changing the 2.6-bit top row's delta, the 2-bit adjust
    rule or the 2-bit code shift fails 5 checks each. Stopping a single-cycle
    block's last codes after the IRQ fails 7.
  - The old "74h consumes its two argument bytes" check now uses 41h, an
    SB16 command the card still ignores.
- **Suites.** `system`, `boot`, `governor`, `sound` and `audio` pass on
  `17d84eca1`, and `make test-changed` passes there. `system`, `boot`,
  `sound` and `audio` pass again on the merge `2d6d77a58`, and `audio` on
  `36ea8a2f3` ([reports](.); the audio report is `36ea8a2f3`'s).

## Serpent Isle on the machine bench

The run was outside the repo, since the game can't be committed. It used
the Bochs CPU (`--cpu oracle`) and the plain-SB disk `g-u7si-b16-sb.img`
(`826af7e5…`, #1443's setup). It typed `cd \serpent` and `serpent` at the
prompt. The machine bench was built from this branch plus the scratch
logging hooks the diagnosis used; they change no RTL. Frames, audio and logs
stay in `~/fes-scratch/si1455/` on Powerboat; only measurements are given
here.

| Time (s) | DSP | IRQ 7 | Channel-1 bytes so far |
| ---: | --- | ---: | ---: |
| 5.65, 5.68 | detection: 14h for 1 byte, twice | 1, 2 | 1, 2 |
| 9.435 | 40h B9h, **75h 2567h** (4-bit, reference, 9,576 bytes) | | |
| 10.794 | (block done) D0h, 74h 55C0h | 3 | 9,578 |
| 13.912 | (block done) | 4 | 31,531 |
| 14.575 | 40h B9h, 75h 2567h again | | |
| 15.935 | (block done) 74h 55C0h | 5 | 41,107 |
| 19.052 | (block done) | 6 | 63,060 |
| 23.757 | 40h A6h, 14h (8-bit PCM) | | |
| 24.619, 24.645, 26.295 | PCM blocks | 7, 8, 9 | 72,636 … 76,703 |
| 32.2–39.1 | further blocks | 10–15 | 193,620 at 39.05 s |

- The 75h block took 1.359 s for 9,576 bytes: 19,152 sample periods at 14.08 kHz
  (time constant B9h, 71 µs), as one byte per two periods with the
  reference byte holding for its own two.
- On main the same run stops after the 2 detection IRQs. The game then
  loops on the driver's status at `1908:0360` (the diagnosis).

**Frames.** Frames are captured once a guest second (`--frames`, 320×400).
A second 27 s run (`p2`) on a fresh copy of the same disk had the same IRQ
times and byte counts as the table up to 26.3 s. Its frames are compared
with main's run `s1` from the diagnosis (same disk, same typing; frames
captured the same way). The share is the fraction of non-zero bytes in each
frame; a flat black frame is 0.

| Second | main `s1` | this branch `p2` |
| ---: | --- | --- |
| 0–20 | | identical to `s1`, frame for frame (same SHA-256) |
| 20 | 0.796, `371036e1` | 0.796, `371036e1` |
| 21 | 0.000, `d4f3e162` (black) | 0.631, `4c2a61e4` |
| 22 | black | 0.767, `009c7750` |
| 23 | black | 0.769, `0a1f57fc` |
| 24 | black | 0.772, `a781a5b4` |
| 25 | black | 0.772, `72ad6554` |
| 26 | black | 0.773, `76334948` |

`s1` stays black to its end at 32 s. Those `p2` frames match, hash for
hash, the diagnosis's scratch-patch run `f1`, which showed the throne-room
scene there.

The 40 s run `p1` and `p2` were of `17d84eca1`. After the chained-block fix,
`p3` reran the 27 s run on `36ea8a2f3`, which also has the merged BIOS. It
is the same as `p2` throughout: the same DSP bytes and times, the same IRQ
times and byte counts, and the same frame hashes every second. The fix
changes only which codes the DAC plays, not the timing the game sees.

## Seal

`make build-fes-pc386-quartus SEED=1` with Quartus Prime Lite 17.0.2, from a
clean checkout of `36ea8a2f3`
([`seal-summary.json`](seal-summary.json)). Every analysis is met at
61.44 MHz.

- Package `de3173afc9a209e77f2f87f2edc7fcccde0039567c5de26ea47c2f8d49d2fd00`,
  build `1c080129abed249df6803ccb5ab25f74`, RBF `7696be9c…` (3,669,892
  bytes).
- Worst slacks: setup +1.174 ns, hold +0.088 ns, recovery +6.903 ns,
  removal +0.440 ns, minimum pulse width +1.122 ns; no negative TNS. The
  slow 100 °C model's system-clock Fmax is 69.34 MHz.
- 29,211 of 41,910 ALMs, 26,314 registers, 259 of 553 RAM blocks,
  1,275,290 block memory bits, 36 of 112 DSP blocks.
- Machine elaborated by Yosys `0a7927bf1`; firmware BIOS `26b37cca…` (with
  #1452), VGABios `69401dc8…`. The sealed `pc386_sb.sv` is `be3ff91b…`, the
  head's.

## Not done

- **Kit B.** Not run yet: a run needs Deano's OK. It would load
  package `de3173af…` with #1426's Serpent Isle setup and check the intro
  past 24 s and the narration audio.
