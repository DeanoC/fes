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

## Amendments

1. **25 November 2026, before any fit. Scope, from the coordinating session
   (agreed with the DX session so the two PRs don't edit the same file).**
   Seen by then: no fit or Quartus run of this PR. The design changed after
   two findings:
   - `scanout/audio_reference.py` pins pc386's PLL IP by path and SHA-256 and
     looks the compiled IP up by that file's name, so any pc386 clock change
     must edit that shared file.
   - The coordinator asked this PR to own the `model` plumbing in
     `machine/board.py`, so that #2194 adds its DX in pc386's files alone.

   The change is now:
   - **`machine/board.py`.** `clocks`, `capture`, `component_files`,
     `top_after`, `machine_pll` and `shared_resets` take an optional `model`.
     `verify` and `generate` read the model from the prepared `inputs.json`.
     A spec opts in by defining `model_clock(model)`, which gives that
     model's rate, PLL IP and parameters, SDRAM clocks, board-top PLL comment
     and reviewed parent digest. A spec with no `model_clock` takes exactly
     the path it took before. A spec that defines both `model_clock` and the
     machine-wide names is refused.
   - **`producers/build_machine_package.py`** passes the model to `capture`,
     to `clocks` (the record's `machine_clock_hz`) and, for a per-model spec,
     to `record_parameters`.
   - **`scanout/audio_reference.py`** takes a per-model machine's PLL pin
     from that machine's own file, by the record's model, and checks that
     the record captured that file. It finds the compiled IP where the board
     writes it (`fes_mailbox/machine_pll.ip`).
   - **pc386** keeps its clock data in `pc386/model_clocks.py`: C0, PLL IP,
     IP digest and parent digest per model. That is the file #2194 adds the
     DX to. `build.py`'s `guest_rates`, `machine_parameters`,
     `component_files`, `record_parameters` and `machine_netlist` take the
     model. The 486SX elaboration is main's in every other respect: same
     sources, derivation, recipe and `I486`.

   "No shared DE25 file changes" above no longer holds. These three shared
   files are in every other DE25 package's source closure, so every other
   `BUILD_ID` moves. That adds:

   - **G7, D12 for every other DE25 package on the shared builder**, as
     #2036's `docs/validation/2026-11-20-de25-pc386-shell-s3/d12.md` did it.
     The packages are MENU, ZX81, Spectrum 48 and 128, ST, C64 `pal` and
     PC/XT `xt`. For each, there are three runs from clean checkouts, one job
     at a time under the lease:
     - **A:** main at this PR's merge base, `--compile-only`.
     - **B:** this PR's head, with the full producer and its gates.
     - **C:** this PR's tooling with main's `BUILD_ID` forced, using #2036's
       `d12_repro.py`.

     Each package must show all of:
     - its Quartus project inputs, A against B, byte-identical apart from
       `BUILD_ID` and paths (#2036's `d12_cmp.py`);
     - C's `core.rbf` byte-identical to A's;
     - B's producer passing its own timing and signoff gates.

     MENU's C stops at its own review, as in #2036, and is reported so.
     PC/XT's C is "not measurable" (#2182) unless main's own PC/XT build
     reproduces by then. If any package's generated inputs cannot be kept
     byte-identical, the work stops and that is reported.

   No other gate changes.

2. **25 November 2026, after seed 1 and the first seeds 2–4 run, before
   the rerun below.**
   - **Seen by then:**
     - Seed 1, through the registered producer, was admitted.
     - Seeds 2–4 compiled as SEED copies (one netlist, the same as seed 1's).
     - Their `LOADED` routes measured 2.51, 2.46 and 2.42 ns.
     - Their G3 admission through `admit_seed.py` failed before any check,
       because the layout lacked the producer's `combined/hps` build files.
       A rerun of that admission, with seed 1's `combined/hps` files copied
       in, was started.
   - **That admission is not measurable as run, and is recorded so.** The
     copied files are seed 1's (its record says `"seed": 1`), and the copy
     is not what G3 committed. Its output is kept as a diagnostic only.
   - **The rerun.** Deano chose (relayed, 25 Nov 2026): "Add --seed, rerun
     (Recommended)".
     - `build_machine_package.py` gains `--seed N`. It is recorded as the
       build record's `seed`, and `board.prepare` writes it into the QSF's
       one `SEED` assignment (`board.seeded_qsf`).
     - Seed 1, the default, changes no byte of the record or the QSF. A
       native-shell build refuses any seed but 1.
     - **G3's seeds 2–4 are run again, each as the registered producer end
       to end:** `build_pc386_package.py --model 486sx --seed N`, on the
       head that carries `--seed`. Each must admit its own package on the
       producer's own gates, and pass G4 and G5 as seed 1 did.
     - Seed 1's producer run, at `561bf8d73`, stands. It was built with the
       default seed, and its design inputs equal this head's for seed 1:
       the edits touch only the seed's path and tests. That equality is
       shown by the head's record for seed 1 equalling `561bf8d73`'s, apart
       from the closure digests of the edited shared files.
   - **G7 runs once, over the final head.**
     - The D12 runs already done (A for five packages, B for seven, at
       `561bf8d73`) are kept as diagnostics.
     - A depends on main only, so its results stand.
     - B and C run again at the final head.
