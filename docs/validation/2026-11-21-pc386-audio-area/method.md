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
