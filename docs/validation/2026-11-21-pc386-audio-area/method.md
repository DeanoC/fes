# pc386 audio area: method (committed before the board fits)

The cut: the OPL2's log-sine and exp tables, upstream's `opl2_log_sine_lut`
and `opl2_exp_lut` case statements, become one initialised 512-entry array,
`opl2_phase_rom` in `phase_generator.sv`, read through two registered ports.
Same values, same one-clock latency, same power-on outputs. The two table
files are deleted. The cut is commits `745daabc7` and `0637e5def` on
`deano/pc386-audio-area`, on main `f3140cb33`.

## Ranking (choosing candidates only)

- Source: #2037's hierarchy-kept DE25 fit (`out-head-hier` on
  chip-30x0-ai-dev, 43,018.1 ALMs, seed 1). Its `audio` entity is 1,333.5 ALMs.
- Module-alone fits of `pc386_audio` (main and the cut) on both devices: the
  pc386 seal's Yosys program with `flatten` removed, at the 61.44 MHz guest
  rates; `scripts/fit_i386_device.py` with a scratch `audio` design (one clock,
  every other port a virtual pin), seed 1. These are a positive control and a
  per-entity estimate, never the measured saving.

## DE25 board pair (the measured saving)

- **Base:** draft PR #2036's head `3401bb193`, the 387-off DE25 pc386 board,
  built by `boards/de25nano/pc386/build.py build`. It contains main at
  `f3140cb33`, and its audio RTL is byte-identical to main's.
- **Cut:** the same commit with the cut's two commits cherry-picked, nothing
  else.
- **One worktree path for both arms**, base built first, so the machine
  elaboration runs at the same fixed path (DeanoC/yosys#80).
- **Seeds:** seed 1 is the board build itself. Seeds 2–4 copy the generated
  project and change only `SEED` in `golden_top.qsf`, as #2047 did.
- **Host and lease:** Quartus Prime Pro 26.3 on Powerboat. Every Quartus
  run (the board build, each seed) holds the Powerboat Pro slot
  `/tmp/fes-quartus-pro-1.lock` with `flock` for its whole run (#2050's pool;
  #2050 is not merged).

## Quantities

- **ALMs:** the whole-design `ALMs needed [=A-B+C]` row of the fit summary.
- **RAM blocks** (M20K) and DSP blocks from the fit summary.
- **Timing:** worst setup, hold, recovery, removal and minimum pulse width,
  per seed, as reported. No SDC, QSF timing setting or seed list is changed.

## Rules (fixed before any board fit)

- **Saving:** base median minus cut median over the four seeds, with each
  arm's spread (max − min).
- **Measurability:** the saving counts as measured only if it exceeds the
  larger of the two spreads; otherwise it is reported as within the spread.
  This decides measurability only.
- **Area band, one-sided:** the cut fails only if its median ALMs exceed the
  base median. A smaller figure never fails. RAM blocks are reported, with
  the expected growth stated in advance: +1 M20K (the module-alone fits:
  7 to 8 on the DE25, 9 to 10 M10K on the DE10).
- **Timing:** every seed of the cut must close: no negative slack in any
  analysis on any clock. A miss on any seed stops the work and is reported;
  no other seed or setting is tried.
- **Positive control:** the cut's fit has the RAM blocks for the two tables
  in its RAM summary (one M20K ROM named for `phase_rom`). Without it the pair is reported as not measured.

## Kit B run a2 (committed before either capture)

Run a1's committed music gate failed, and a1 stays a fail: its floor was the capture card's DC
offset (−281 counts on the left channel, no AC), not silence, and the gate compared total RMS.
Deano approved a second run, a2 ("Yes, run a2", relayed by the coordinator on 21 November), with
this method. Its thresholds were chosen after a1's recordings had been seen.

- **Packages, back to back on Kit B, core-only, on the baseline image and bundle:** main's sealed
  `1d09da2784baa6a10983ec3d2e0a7f0f9a29924306bf3cce0d7209b7089bcb70` (#2045's seal, `bf7afef73`,
  BUILD_ID `2b929631`; its machine RTL equals main `f3140cb33`'s), run `a2m`, then this branch's
  `53e2dca69cf76177cd93f6f641be1a655fac4725c97c44d340e19fd6cc8cd05f` (BUILD_ID `b44a1c8e`), run
  `a2c`. Each run claims Kit B in `~/kit-status/kitB-STATUS.md`, restores, verifies and logs it.
- **Same title and capture points for both:** `kit/hil_audio_wolf3d_a2.py`, which is a1's
  driver with its gate removed. Wolfenstein 3D on the user-supplied drive `real`, the same key
  script, and Kit B's HDMI capture `hw:CARD=D4KPRO` at 48 kHz stereo: 8 s at the FreeDOS prompt (the
  floor) and 40 s from the launch (the game).
- **The gate is [`kit/a2_analyse.py`](kit/a2_analyse.py)**, applied to both runs' recordings:
  - per whole second, the AC RMS (each channel's mean over the second removed, both channels);
  - game seconds from the 5th on are scored;
  - **presence**, each package: T = max(20 counts, 10 × the floor's median AC RMS); at least
    half of the scored seconds ≥ T;
  - **level**, the cut against main: |20 log10(cut median / main median)| ≤ 3 dB over the scored
    seconds;
  - a2 passes only if both presences and the level comparison pass.
- **Not measurable:** a recording that cannot be read or is shorter than 30 s (game) or 5 s
  (floor), or a run whose `freedos` or `wolf3d` goal fails.
- Whatever the gate says is the result. No other gate follows it.
