# DE25 PC/XT: why one compile doesn't repeat (#2182)

Main's DE25 PC/XT (`fes.pcxt-dos`) compile gives a different RBF from run to
run, even at one `BUILD_ID` and one output path. This record finds where that
starts and how D12 should handle it.

**Finding.** The variation comes from inside the Quartus Prime Pro 26.3.0
Build 114 fitter. A fixed path, a fixed PID, disabled ASLR and a single
processor don't remove it. Each of those was tested on its own below. No input
FES controls makes the fit repeat, so FES can't make two PC/XT compiles give
the same RBF.

## Method

All runs used chip-30x0-ai-dev under the `/tmp/lander-quartus-3090.lock` Pro
lease (`scripts/quartus_jobs.py run --edition pro`).

1. Synthesis isn't the cause. Earlier D12 runs at `c7b4df32…` (A6, A6r, C6,
   C6s) synthesized the same design every time: 15,341 registers, and synthesis
   reports that differ only in merged register names. Atari ST and C64 show the
   same naming difference, and their RBFs still repeat. The runs first part at
   the place stage.
2. Every fit below starts from A6-pcxt's synthesized project
   (`signoff/combined/hps/project`), copied fresh to one fixed directory each
   time. Each run is
   `quartus_fit --read_settings_files=on --write_settings_files=off golden_top -c golden_top`,
   and only `NUM_PARALLEL_PROCESSORS` is changed.
3. A run is fingerprinted by the place, route and finalize reports, after
   dropping times, durations, memory, CPU and process-ID lines and collapsing
   whitespace.

## Results

| Processors | ASLR | PID | Fits | Placements | Routes |
| --- | --- | --- | --- | --- | --- |
| 1 | on | host | 3 | 2 kinds (2 + 1) | follow placement |
| 2 | on | host | 3 | 2 kinds (2 + 1) | follow placement |
| 8 | on | host | 3 | 3 kinds | — |
| 8 | off (`setarch -R`) | host | 4 | **1 kind** | 2 kinds (2 + 2) |
| 1 | off | host | 4 | 2 kinds (2 + 2) | follow placement |
| 1 | off | fixed (`unshare -p`, Quartus is PID 1) | 4 | 3 kinds (2 + 1 + 1) | follow placement |

Each placement had 12,604 or 12,624 ALMs. The earliest message where two fits
differ is the clock promotion, for example `CLOCK0_50 (43 fanout) drives clock
sector (1, 0)` against `sectors (0, 0) to (1, 0)`. That choice doesn't predict
the placement, though, so it is a symptom.

With ASLR off at 8 processors, all four placements were the same, but the
router still split two ways. The differences are a handful of wires per
resource class, for example Block interconnects 71,462 against 71,465. Turning
ASLR off narrows the spread but doesn't remove it. At one processor, with ASLR
off, it doesn't help at all.

An `strace -f` of one fit shows what else the fitter reads from the host:

- one 8-byte `getrandom` per process at start-up (from the C library);
- one `/dev/urandom` open, next to the SQLite qdb journal;
- about 2,600 `clock_gettime` system calls, plus any vDSO clock reads strace
  can't see;
- about 30 threads, even with one parallel processor.

None of these is a setting that FES passes or records. Even at
`NUM_PARALLEL_PROCESSORS 1`, the fitter's own threads and time-dependent work
are enough to change the result.

## What this rules out

- **Project path.** It is fixed here and the runs still differ.
- **Seed.** The QSF seed is fixed, and the fitter reads no other seed setting.
- **Timestamps in inputs.** #2036 found every input file byte-identical between
  differing runs.
- **8 parallel processors (#2249).** One processor varies too, so going back to
  one wouldn't make PC/XT repeat. It would also move every DE25 `BUILD_ID`.
- **ASLR and PID.** Fixing either, or both, still gives differing fits.

An ASLR-off wrapper around every leased Quartus launch was written and tested
during this work, then dropped. It doesn't give a repeatable fit, and the
codebase drops code that fixes nothing.

## D12 handling for PC/XT

- PC/XT's D12 RBF comparison is **not measurable**, as #2036 recorded and
  Deano accepted. It doesn't count as a pass or a failure. The other DE25
  machines (ZX81, Spectrum 48/128, Atari ST, C64) still need an exact RBF
  match.
- A PC/XT rebuild whose `BUILD_ID` moved only through its closure is checked
  by its own timing signoff. The record states that its RBF can't be compared
  with main's, and why, pointing to this README. The timing and seal checks
  stay as strict as for any other machine.
- Hardware acceptance follows exact bytes. The PC/XT RBF that gets sealed is
  the one that passes HIL, so a reseal (for example, after the DQ capture/hold
  fix in `2026-11-22-de25-pcxt-compile`) must HIL-test those exact bytes. It
  can't reuse an earlier RBF's acceptance.
- If a later Quartus Pro release repeats this fit, re-run this check and
  restore the RBF comparison for PC/XT.

The scripts and kept reports are on chip-30x0-ai-dev under
`/data/projects/fes-2182/`.
