# DE25 window latency: method (committed before the recorded fits)

Issue #2117. This is the shell area study's **S-lat** (#2047,
[`../2026-11-20-de25-shell-area/`](../2026-11-20-de25-shell-area/README.md)). There it
measured 946 ALMs (spread 58.9).

## Deano's decision

On 22 Nov 2026 Deano chose **window profile only**. Only a machine with
`fes.fabric.de25-hps-window` (pc386) sets qsys_top's `maxAdditionalLatency` to 0.

## The change

**Where:** `machine/board.py`'s window additions to the qsys_top integration
(`WINDOW_TOP`). Only a spec with `WINDOW = True` appends them.

**What it does:** `set_domain_assignment {$system} qsys_mm.maxAdditionalLatency 0`.

**Record:** the machine record notes `max_additional_latency`.

**The verifier** (`verify_window_latency`) refuses a generated project unless:
- every remaining qsys_top connection carries 0;
- the F2SDRAM interconnect has no pipeline stage.

It refuses #2036's build `j`, which has latency 4 and 16 stage references. It
accepts the study's S-lat generation.

## The pc386 measurement

**Board:** the 387-off DE25 pc386 board build, from #2036's head as merged at the
time, with the fixed Yosys lock.

**Arms:**
- **Baseline:** #2036's head. It is shared with the A-fifo (#2108) and A-dma2
  (#2118) pairs.
- **Arm:** the baseline plus this branch's `machine/board.py`.

**Seeds:**
- Seeds 1–4 per arm.
- Seed 1 is the board build. Seeds 2–4 are copies of its generated project with
  only `SEED` changed.

**Lease:** every Quartus run holds a Pro slot of the Quartus lease, through
`quartus_jobs.py run` or `remote`, one slot per seed.

**The quantity:**
- **ALMs:** whole-design `ALMs needed`, the median of four seeds.
- **Saving:** the baseline median minus the arm median.
- **Measured** only when the saving exceeds the larger spread (max − min).
  Otherwise the result is reported as "within the spread".
- **Bands:** area bands are one-sided.
- **Also reported:** RAM blocks and DSPs.

**Timing:**
- For **every seed** of both arms, report the worst setup, hold, recovery,
  removal and minimum pulse width over every clock.
- Hold and removal are listed per seed alongside setup. The exploratory seed
  showed hold 0.000 and removal +0.046.
- Every slack must be ≥ 0 at 61.437908 MHz. A seed that misses stops the work and
  is reported. No check is relaxed.

## The other DE25 cores (AGENTS.md D12)

`machine/board.py` is in the closures of Spectrum 48, Spectrum 128 and ST, so
their `BUILD_ID`s move. Each takes the D12 comparison, by the method of #2036's
`d12.md`:
- **A:** the base producer with `--compile-only`.
- **B:** this branch's full producer, with its gates.
- **C:** this branch's tooling with A's `BUILD_ID` forced.

What must hold:
- Project inputs are byte-identical apart from `BUILD_ID` and paths.
- C's `core.rbf` equals A's.
- B's gates pass.
- Both sides use one tool install and the fixed Yosys lock.

For MENU and ZX81, the record shows that `machine/board.py` is outside their
producers' closures and that their `BUILD_ID`s do not move.

If #2097 merges first, main is merged in and the comparison is redone.
