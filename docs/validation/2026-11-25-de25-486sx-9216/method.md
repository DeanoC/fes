# DE25 pc386 486SX at 92.156863 MHz, with a machine clock per model (method, committed before any fit)

Deano, 25 Nov 2026 (relayed by the coordinating session; his option, verbatim):
"Both, per-model clocks (Recommended)" — "Keep 486sx and add 486dx as a second
model, each with its own machine clock: the SX moves to 92.16 MHz (the clock
study's step), and the DX stays at 61.44 MHz until it's measured. #2194 comes
back to lane:deano to keep the SX model, and the producer gains a per-model
clock."

The clock study is #2224 (`docs/validation/2026-11-24-de25-486sx-fmax/`, tags
`evidence/2224-*`). At C0 = 34 its one seed passed every analysis (+2.011 ns
on the machine clock). The design assistant's findings equalled the
61.44 MHz control's. The reviewed `LOADED` basis's route budget (3.9 ns)
covered the measured route (2.54 ns with clock skew). That fit used the
control's Qsys output, not the producer; this PR is the producer's own build.

This file is committed and pushed before any fit. A later amendment is dated
and says what had been seen by then. No gate changes after a result exists.

## The change

- **A machine clock per model, in pc386's own files only.**
  - `boards/de25nano/pc386/build.py` gets `MODEL_CLOCKS`. For each model it
    gives:
    - its PLL output divider C0 on the board PLL's shared VCO
      (50 MHz × 188 / 3; C1 = 255 stays the 12.287582 MHz audio master);
    - its PLL IP file;
    - its reviewed Qsys parent digest.
  - `select_model(model)` sets the clock spec the board reads (`MACHINE_HZ`,
    `CLOCK`, `PLL_IP`, `PLL_PARAMETERS`, `PLL_COMMENT`, `PARENT_SHA256`) and
    the AUDIO ratio (C0 / 65280; 1/1280 at C0 = 51).
  - The pc386 producer selects the model in its `model()` step, which every
    record and build path calls before it reads a clock. The board CLI
    selects it from `--model`.
  - No shared DE25 file (`machine/board.py`, `build_machine_package.py`)
    changes, so no other DE25 machine's source closure or `BUILD_ID` moves.
- **`486sx` moves to C0 = 34: 92.156863 MHz.**
  - `MACHINE_HZ` is 92156863 and PLL output 0 is `92.156863`.
  - Its PLL IP is `pc386/machine_pll_486sx.ip`: today's `machine_pll.ip` with
    output 0's frequency changed and nothing else.
- **The slot for #2194.** Adding `486dx` takes one `MODEL_CLOCKS` entry
  (C0 = 51, its own IP at `61.437908`, today's parent digest) and its
  `MODELS` entry. A unit test builds two models with different clocks from
  one spec. It checks that each record and each board selection carries its
  own clock.
- **The bases' text** (`signoff_bases.py`) quotes the machine period: "81.4 ns
  at 61.437908 MHz" and "32.6 ns at 61.437908 MHz".
  - It is rewritten for the fastest model's clock: 92.156863 MHz gives 54.3 ns
    and 21.7 ns.
  - `LOADED`'s remainder is stated as what it is: 3.9 ns, measured against
    the route in every seed (gate G5).
  - No basis pattern, count or SDC line changes, and no basis is added.
- **No RTL changes.** Comments in `fes_de25_pc386_standard.v` and
  `pc386-clocks.sdc` that name 61.437908 MHz are corrected. Both are DE25
  board files; the DE10 reads neither.

## Gates (each passes or the PR does not go forward; nothing is relaxed)

- **G1, the DE10 does not move.**
  - `de10_netlist.py` (from #2171's record) runs at the merge base and at the
    head, from one checkout path, with the pc386 lock's Yosys and
    `YOSYS_MAX_THREADS=1`. The two netlists must be `cmp`-equal.
  - `de10_design_id.py`: the DE10 design, evidence, record and toolchain
    identities must be equal.
- **G2, the fresh review of the Qsys parent digest.**
  - First, the producer at the head, before `486sx`'s parent digest is
    changed, generates the 92.16 MHz parent and stops at the shared FIFO
    reset gate.
  - The review is then mechanical:
    1. In that generated parent, replace the window interconnect's module
       name (`peripheral_subsys_altera_mm_interconnect_1920_<hash>`) with
       the 61.44 MHz name (`…_xi5gepi`). The result's SHA-256 must equal
       today's reviewed digest (`10defc9a…`).
    2. Every other pinned generated input must have its pin.
    3. The interconnect module's body must equal the 61.44 MHz generation's
       apart from its name.
  - Only when all three hold does `486sx`'s digest become the 92.16 MHz
    parent's SHA-256. If any fails, that is a finding for Deano, and no
    digest is changed.
- **G3, four seeds, every analysis.**
  - **Seed 1** is the registered producer, `build_pc386_package.py --model
    486sx`. It must admit the package on its own gates: package timing
    admission and every signoff row reviewed.
  - **Seeds 2–4** are copies of seed 1's generated project with only `SEED`
    changed, compiled with `quartus_sh --flow compile`.
    - Each copy is laid out as the producer lays out its own
      (`build-inputs.json` beside `signoff/`).
    - The producer's unchanged admission function then runs on it:
      `timing_evidence`, with `async_inputs.collect` and
      `audio_reference.collect`. So every seed is admitted by the same code
      as seed 1.
  - The pass rule:
    - all four seeds fit (ALMs ≤ 46,800);
    - every row of every seed's timing summary is ≥ 0 at every corner;
    - every seed's `package_timing_admission` is true, which means
      0 unreviewed signoff rows.
  - A seed that misses is a fail, and three of four is a fail.
- **G4, the record.**
  - Seed 1's record carries `model` `486sx`, `machine_clock_hz` 92156863
    and the C0 = 34 guest rates (AUDIO 1/1920).
  - Every seed's PLL report shows M 188, N 3, C0 34 and C1 255.
  - All four seeds compiled one netlist.
- **G5, `LOADED`'s route.**
  - The route is `machine_presenter_loaded_flag` →
    `machine_presenter_loaded_sync[0]`, cut by a false path.
  - In every seed it must be under 3.9 ns at every corner, measured as data
    delay plus the magnitude of its clock skew (#2224's `loaded_route.tcl`).
  - 3.9 ns is what the basis leaves it: 5 × 10.851 − 50.4 ns.
- **G6, tests.**
  - The DE25 machine-package and pc386 tests pass, with `486sx`'s expected
    clock now 92156863.
  - The new two-model clock test passes.
  - `make test-changed` passes.

## Not in this PR

- The DX at any clock but 61.44 MHz.
- Period-tied crossing bounds and a re-argued `LOADED` for clocks above
  about 99 MHz (#2224's second step). That follows this PR.
- A kit run: DE25 pc386 has no kit path until slice 4's runtime lands
  (#2214, #2215).

## Running it

- Powerboat (or chip-30x0-ai-dev), with Quartus Pro 26.3.
- Every Quartus program runs under a Pro slot of the Quartus lease, with a
  300-minute cap.
- Each run starts from a clean detached worktree of the pushed head, under
  `/data/projects/fes-486sx-9216`.
- No GPU routes anything.
