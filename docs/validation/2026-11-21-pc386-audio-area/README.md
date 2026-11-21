# pc386 audio area: the OPL2 phase tables in one RAM block

The pc386 audio (OPL2, Sound Blaster 2.0 DSP, mixer, PC speaker) is about 1,333 ALMs on the
DE25, of which the OPL2 is 881 (#2037's hierarchy-kept fit, ranked in #2047). This record profiles
it, lands the one cut that keeps the output bit-exact and is large enough to measure, and lists
the rest for the coordinator. The audio RTL is shared, so the DE10 pc386 moves too.

The method was committed before any board fit: [method.md](method.md).

## Result

| | Main | This branch | Change |
|---|---:|---:|---:|
| **DE25 board, 387 off, four seeds (median, ALMs needed)** | DE25_BASE | DE25_CUT | **DE25_SAVING** |
| DE25 board RAM blocks | DE25_BASE_RAM | DE25_CUT_RAM | DE25_RAM_DELTA |
| DE10 seal, seed 1 (ALMs) | 36,354 | **36,051** | **−303** |
| DE10 seal RAM blocks (M10K) | 282 | 283 | +1 |
| DE10 seal setup / hold slack (ns) | +0.267 / +0.093 | +0.703 / +0.106 | |
| `pc386_audio` alone, DE25 (ALMs, RAM) | 1,468, 7 | 1,131, 8 | −337, +1 |
| `pc386_audio` alone, DE10 (ALMs, M10K) | 1,460, 9 | 1,126, 10 | −334, +1 |

- **The output is bit-exact.** An exhaustive bench checks the shared ROM against upstream's two
  tables at every address pair. The OPL2 suite (Nuked-OPL3, sample for sample) and the audio suite
  pass. A 16-second Ultima VI intro in the machine bench gives a byte-identical mixer WAV and OPL2
  port log on main and on the branch.
- **DE10:** resealed with the registered producer, including a fresh full netlist lockstep. Kit B
  booted it and played Wolfenstein 3D, but the run's precommitted music gate **failed**
  ([below](#kit-b-run-a1)).
- **DE25:** PAIR_SENTENCE

## The cut

Upstream's `opl2_log_sine_lut` and `opl2_exp_lut` are 256-entry case statements with a registered
output. The pc386 seal's netlist program runs `proc -norom`, so Yosys writes them as multiplexers.
In #2037's DE25 fit they were 196.8 (exp) and 107.9 (log-sine) of the OPL2's 881.5 ALMs.

They are now one initialised 512-entry array, `opl2_phase_rom` (in
[`phase_generator.sv`](../../../sources/misteross/cores/fes-pc386/vendor/opl2_fpga/phase_generator.sv)),
read through two registered ports: log-sine at 000h–0FFh, exp at 100h–1FFh. Values, the one-clock
latency and the power-on outputs (0) are upstream's. The two table files are deleted, and the
vendor README and `source.json` record the change.

- **One RAM block, not two.** Two separate initialised arrays (commit `745daabc7`, measured and
  then superseded) took one block each: DE25 9 RAM blocks for 1,129 ALMs, DE10 11 M10K for 1,127.
  The shared array (`0637e5def`) gives the same ALMs for one block: DE25 8 for 1,131, DE10 10 for
  1,126.
- **Positive control:** in the module-alone fits the `exp_lut_inst` and `log_sine_lut_inst` rows,
  234.1 and 104.8 ALMs on the DE25, are gone, and an M20K ROM of 5,120 bits under `phase_rom`
  appears. On the DE10 they were 207.0 and 115.6 ALMs and become `altsyncram` ROMs.

## Bit-exactness

| Check | Result |
|---|---|
| [`rom_diff/run.sh`](rom_diff/run.sh): the shared ROM against main `f3140cb33`'s two case-statement modules, every (log-sine, exp) address pair, 65,536 clocks, and power-on outputs | PASS, 0 differences. `--mutate` (one exp entry 1018 → 1017) FAILS with 256 differences |
| `rom_diff/run.sh --netlist CACHE_ROOT`: the same bench on the seal recipe's Yosys netlist of `opl2_phase_rom` (pc386 lock's Yosys `e240a465`) | PASS, 0 differences; netlist SHA-256 `8a826d8d1a8729145fb73d3ebf4b1041754d7c6b1b2bd2e88fc9b88cc7d8c24a` |
| [`rom_diff/check_mif.py`](rom_diff/check_mif.py) on the DE10 seal's ROM init file, which Quartus wrote for the inferred 512×12 true-dual-port `altsyncram` ([`rom_diff/de10-seal-phase-rom.mif`](rom_diff/de10-seal-phase-rom.mif), from the seal project's `db/`) | PASS, all 512 words equal the two tables. A file with one word changed FAILS |
| `sim_fes_pc386.py --suite opl2` (random programs and the captured sound log, sample for sample against Nuked-OPL3) at `0637e5def` | PASS |
| `--suite audio` (speaker, DSP commands and DMA, OPL2 timers, mixer) at `0637e5def` | PASS |
| `--suite game --game ultima6` (16 s of the intro's AdLib music, oracle CPU), main `f3140cb33` against `0637e5def` | Mixer WAV byte-identical, SHA-256 `33ffb571e6da4eb8cb85b752042059c716ba35f6b73bf2127eb58c18c3a4ee35`; OPL2 port log byte-identical, `c26235274d4837636ae51c1fa1ab160cd7e17ed8f78e5a6941cd672e01b0ca8c`; both 795,454 OPL2 samples exact against Nuked-OPL3, 2,779 writes. The WAV and log are game-derived and are not committed |
| `--suite system`, `--suite sound` at `0637e5def` | SYSTEM_SOUND |
| pc386 unit tests (`tests/test_pc386_*.py`, `test_build_pc386_product`, `test_verify_pc386_*`) | All pass |

## DE10 seal

Registered producer `build_pc386_product.py --with-lockstep --gpu-router CUDA --seed 1` on
chip-30x0-ai-dev, holding the Lite lease slot `/tmp/fes-quartus-lite-1.lock`, from a clean
worktree of `890848477` (the cut plus the method commit). Files in [seal/](seal/).

- BUILD_ID `b44a1c8e075415a9701da94d7eed092c`, package `53e2dca69cf76177cd93f6f641be1a655fac4725c97c44d340e19fd6cc8cd05f`,
  RBF `07cc5121f9ec815ce0f692f32de8bf9f60e87ba34ba4254dce573d57dce25950`.
- 36,051 ALMs and 283 RAM blocks. Every analysis passes: setup +0.703, hold +0.106, recovery
  +7.006, removal +0.512, minimum pulse width +1.122 ns. System 61.44, pixel 74.27 and audio 12.29 MHz.
- The full i386 netlist lockstep ran fresh inside the producer and passed
  (`seal/lockstep-report.json.gz`).
- Main's reference is #2045's seal (`docs/validation/2026-11-21-i486-s2-isa/seal/`): `bf7afef73`,
  the same host, slot and seed, with 36,354 ALMs and 282 RAM blocks. Its machine RTL equals main
  `f3140cb33`'s; only two simulation stop lists differ between them.
- [`head_matches_seal.py`](head_matches_seal.py) (copied from the i486 slice 2 record) runs the
  producer's build record selection-only on a checkout. On this branch's head it prints SAME DESIGN,
  BUILD_ID `b44a1c8e…`; with one comment byte added to `pc386_mixer.sv` it prints DIFFERENT DESIGN.
- A single seed. The i386 area audit puts the DE10 fit-to-fit noise at about ±200 ALMs, so −303
  is one seed's reading, consistent with the −334 module-alone figure.

## Kit B run a1

Core-only on Kit B's baseline (image `179affbb…`, bundle `c9ae79e2…`), claimed and restored in
`~/kit-status/kitB-STATUS.md`, released idle and lease-free. Driver and wrapper:
[kit/](kit/). The sealed package booted FreeDOS and ran Wolfenstein 3D.

| Goal | Result |
|---|---|
| FreeDOS prompt | PASS, 3.3 s after launch |
| Wolfenstein 3D runs and walks in E1L1 | PASS: graphics, 93.8% lit, 56.2% of the picture changed after walking |
| Music: at least half of the game recording's seconds ≥ 20 dB above the floor's median second (written into the scratch driver before the run, not committed to git first) | **FAIL**: 0 of 38 seconds |

**The music gate failed, and a1's verdict stays fail.** The floor recording is the capture's DC
offset: the left channel sits at −281 counts with no AC at all, which is −44.3 dBFS of total RMS.
The gate compared total RMS, offset included, so the game seconds read only 3–8 dB over it.

What a1 establishes: the sealed package boots, runs a game and puts audio on the HDMI output. It
does not establish the music's level or correctness; the simulations above carry bit-exactness.

The following was measured **after** the gate failed and does not change a1's verdict:
[`kit/a1_ac_analysis.py`](kit/a1_ac_analysis.py) removes each second's mean. The floor's AC RMS is
0.0 counts (14.3 in its first second). The game recording's AC RMS is 72–448 counts on every
second after the third (the launch), with peaks to ±2,500 ([output](kit/a1_ac_analysis.txt)).
The recordings' digests are in `kit/kit-b-a1-audio.sha256`; the audio itself is game-derived and
is not committed.

A run that could decide this needs its gate on AC RMS (each second's mean removed), committed
before the capture, and preferably main's package and this one back to back on the same kit for a
level comparison. That is a new run, for the coordinator to decide.

## DE25 board pair

PAIR_TABLE

## Profile and candidates

From #2037's hierarchy-kept DE25 fit (seed 1; the rows rank, they do not measure):

| Entity | ALMs | Notes |
|---|---:|---|
| `audio` | 1,333.5 | |
| — `opl2` (wrapper own 44.1, queue 10.4, rate 15.2) | 881.5 | |
| — — `channels` own | 82.5 | channel accumulator, clamp, carrier delay |
| — — `control_operators` own and memories | 105 | six MLAB register files (10 ALMs each), `kon_mem` 16 |
| — — `operator` | 611.0 | one time-shared operator; its per-operator state is already in M20K |
| — — — `phase_generator` | 392.9 | **exp table 196.8, log-sine table 107.9**, own 53.3, rhythm 27.0 |
| — — — `envelope_generator` | 136.0 | own 46.2, rate shift 36.6, tremolo 24.9 |
| — — — `calc_phase_inc` | 37.5 | 1 DSP |
| — `dsp` (`pc386_sb`) | 289.5 | |
| — `mixer` | 148.1 | 32-step divider, DC blocker |

The OPL2 is already serial: one operator pipeline serves all 18 slots, and the phase accumulators,
envelopes and feedback are in block RAM. There is no second operator or adder to share.

**Bit-exact, implemented:** the phase tables (above).

**Bit-exact, not implemented.** Each is below the DE25 board's seed spread (#2047: about
100 ALMs), so none can be measured alone, and each would need its own differential bench:

| Candidate | Estimate | Why not now |
|---|---:|---|
| Merge the OPL2's same-address MLAB register files (`ar_dr` + `sl_rr`; `fnum_low` + `block_fnum_high`) | 20–30 | needs per-field write enables in one MLAB |
| One shared `op_num` pipeline instead of eight `pipeline_sr` copies | 10–25 | Quartus may already merge duplicate registers |
| `ksl_add_rom` into a RAM block | about 9 | the shared ROM's two ports are taken; a block of its own would cost a RAM block for 9 ALMs |

**Changing the output: listed for Deano, not implemented.** Savings are module-alone DE10 fits,
one seed, against the cut's 1,126 ALMs ([`ablate.sh`](ablate.sh) holds the patches):

| Candidate | Saving | What changes |
|---|---:|---|
| Drop SB ADPCM playback (commands 74–77, 7D, 7F, 16, 17, 1F play as PCM) | 75 | ADPCM sound effects become noise. Titles using 2-, 2.6- or 4-bit ADPCM lose those sounds |
| Mixer: a fixed divide (the ablation shifts by 10) instead of the exact divide by the period's clock count | 41 | Not measured against audio. If the period is a constant 1,280 clocks (both boards take the audio clock from the machine PLL; not checked here), a multiply by 1/1,280 would match outside the first period after reset. The shift ablation only bounds the saving |
| Mixer: drop the DC blocker | 23 | Static speaker drive and DAC offsets reach the output; every sample with a DC component changes |

None of these closes the 387-on gap on its own. The phase-table cut is the one large bit-exact
cut in the audio.

## Files

- [method.md](method.md): the board-pair method, committed before the fits.
- [pair_collect.py](pair_collect.py), [collect.json](collect.json): the board pair.
- [rom_diff/](rom_diff/): the exhaustive ROM bench.
- [seal/](seal/): the DE10 seal's inputs, summary and lockstep report.
- [kit/](kit/): Kit B run a1's driver, wrapper, result and analysis.
- [ablate.sh](ablate.sh): the output-changing ablations.
