# DE25 pc386 486SX clock ladder, second step (method, committed before any fit)

Asked by the coordinating session on 28 Nov 2026 for Deano: raise the DE25
pc386 `486sx` machine clock above 92.156863 MHz (C0 = 34) with the crossing
work #2264 did for the `486dx` (now C0 = 28, 111.904762 MHz).

This file is committed and pushed before any fit, generation preflight or
simulation for this work. A later amendment is dated and says what had been
seen by then. No gate is added, changed or relaxed after a result exists.
A failed gate is recorded as "fail", and a gate that could not be run as
written as "not measurable".

Base: FES main at `17cbddbec`. The `486sx` is at C0 = 34 with the original
five-clock presenter. The `486dx` is at C0 = 28 with the eight-cycle
presenter and the live retimed-tension proof.

## What was seen before this method

All of it is read from committed records. Nothing was fitted or simulated
for this work.

- #2224 (`2026-11-24-de25-486sx-fmax`): raw SX timing closes four seeds at
  C0 = 28 and C0 = 24. The first failing step is C0 = 20. Signoff findings
  equal the control's up to C0 = 32. From C0 = 30 up, ten CDC-50003 rows
  appear on seven held bundles. The fits used the control's generated
  project, and no producer gate ran above 61.44 MHz. The raw projects and
  DRC reports were deleted on 25 Nov 2026.
- #2228 (`2026-11-25-de25-486sx-9216`): the SX at C0 = 34. Its four seeds
  had LOADED routes of 2.42 to 2.54 ns against a 3.851 ns budget
  (5 × T − 50.4 ns).
- #2264 (`2026-11-25-de25-486dx-clock`): the merged treatment is as follows.
  - The held-bundle bound in `pc386-clocks.sdc` is min(10 ns, one machine
    period rounded down with 1 ps margin). This removed the ten CDC-50003
    rows at C0 = 30 and is model-independent.
  - `derive.retarget_presenter_loaded` gives eight machine cycles between
    LOADED reports (DX only).
  - The live TimeQuest retimed-tension proof
    (`retimed_tension.py`/`.tcl`, `retimed_structure.tcl`) admits
    `RETIMED_CONTROL_TENSION` rows (DX only). Without it, the SX admits only
    `CPU_CONTROL_TENSION`.
  - The DX qualified C0 = 28 and missed C0 = 26. The 2.250 ns LOADED flag
    datapath target and the 5.046 ns display command datapath target it
    tried at C0 = 26 are not on main.

What follows from that:

- With the five-clock presenter, the SX's LOADED budget is 0.66 ns at
  C0 = 32 and negative at every rung from C0 = 30. The SX cannot pass any
  rung above C0 = 34 without the eight-cycle re-arm.
- Spellings that only the retimed proof admits (`~DUPLICATE_Duplicate`,
  `..._dff`) refused DX seeds at C0 = 28. The SX's DRC reports at those
  clocks no longer exist, so whether the SX produces them cannot be checked
  before fitting.

## The change (the candidate, fixed before the first rung fit)

#2264's merged treatment is applied to the SX unchanged. Nothing is invented
for the SX.

1. **Presenter re-arm for both models.**
   - `build.py` `elaboration` derives `pc386_presenter.sv` with
     `retarget_presenter_loaded` for the SX as well as the DX: same
     function, same anchors, same `LOADED_REARM_CYCLES = 8`.
   - `record_parameters` records `loaded_rearm_cycles` for both models.
   - `sim.py` `derived_sources` swaps in the same derived presenter for both
     models, so every simulation runs the fitted presenter bytes.
   - The `LOADED` basis prose in `signoff_bases.py` is changed to describe
     both models at eight cycles. No basis pattern, count or SDC line
     changes.
   - The DX's own presenter tests are extended to the SX derivation.
2. **Live retimed-tension proof for both models.** `RegisteredBoard.compile_board`
   runs the same `reviewed_targets`/`collect` path for the SX as for the
   DX, with the same matchers. It is decided now, because no gate can be
   added after a refusal. The proof is no weaker than the DX's: every
   subject still needs exact machine-clock ownership, nonnegative slack at
   all four corners and the other checks in `RETIMED_TENSION`.
3. **SDC:** no edit. The period-tied bound already applies to the SX.
4. **The SX clock:** `model_clocks.py`'s `486sx` entry moves to the rung's
   C0. `machine_pll_486sx.ip` changes only output 0's frequency. The
   parent digest is reviewed as below.
5. **Nothing else.**
   - No RTL outside the board's derivations changes. No DE10 file changes.
   - The DX's derivation, clock, PLL IP and parent stay as they are.

Before the first fit, the candidate must pass the DE25 pc386 tests,
`make check`, the DE10 identity check (G-ID1) and the D12 input-selection
check (G-ID2).

## Rungs

The order is #2224/#2264's, upward from the shipped SX clock. Start at
C0 = 28 and test each listed C0 in turn. Stop at the first rung that fails.

| Order | C0 | Machine MHz | Period ns | LOADED budget 8T − 50.4 ns | Allowance min(3.9, budget) |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 28 | 111.904762 | 8.9362 | 21.09 | 3.900 |
| 2 | 26 | 120.512821 | 8.2979 | 15.98 | 3.900 |
| 3 | 24 | 130.555556 | 7.6596 | 10.88 | 3.900 |
| 4 | 22 | 142.424242 | 7.0213 | 5.77 | 3.900 |
| 5 | 20 | 156.666667 | 6.3830 | 0.66 | 0.664 |

- C0 = 20 is #2224's measured first failing step. It is listed only so the
  ladder has an end.
- If C0 = 28 fails, the SX stays at C0 = 34. This method tests no rung
  between 34 and 28. That needs a new method from the coordinator.
- If a higher rung fails, the SX ships at the last rung that passed.

## Per-rung generated inputs (before the rung's fits)

The machine clock is the only per-model input to the Qsys generation. So at
C0 = 28, 26 and 24 the SX should reproduce #2264's reviewed parents for the
same C0:

- C0 = 28: `0cccfdc8…`, the shipped DX pin.
- C0 = 26: `b900cf95…`, from `parent-c26.txt`.
- C0 = 24: `66d997f6…`, from `parent-c24.txt`.

The SX PLL IP at each of these rungs must be byte-identical to the DX's IP at
the same C0. At C0 = 28 that is `b6bfea6b…`.

Each rung's preflight is the #2228 G2 review:

1. Generate with the SX at the new C0, stopping at the shared FIFO reset
   gate.
2. Replace the window interconnect's module name with the reference's. The
   resulting parent must equal the reference digest.
3. Every other pinned generated input must have its pin.
4. The interconnect body must equal the reference's apart from the name.

The reference is the C0 = 34 SX parent (`fefb39d6…`), or #2264's parent at
the same C0 where one exists. Only when all of this holds does the SX's
`parent_sha256` change. If any check fails, the rung is "not measurable" and
the result goes to the coordinator. Rungs from C0 = 22 have no prior review,
and the same check stands alone there.

## Control

The control is a fresh registered producer fit of **main's** `486sx` at
`17cbddbec` (C0 = 34, five-clock presenter), seed 1. It runs under the same
lease, tools and host class as the rungs.

## Fit and admission (every rung reached)

For each of seeds 1 to 4, run
`build_pc386_package.py --model 486sx --seed N` at one committed candidate:

- the pinned Yosys with `YOSYS_MAX_THREADS=1`;
- Quartus Pro 26.3;
- a clean worktree of the pushed commit, output on `/data`;
- a separate Pro lease job per seed (`scripts/quartus_jobs.py run|remote
  --edition pro --cap-minutes 300`).

A timeout, failed build or missing evidence cannot qualify a rung.

**Pass for a rung: all of the following on all four seeds.** Three of four
is a fail.

- **F1, fit:** ALMs ≤ 46,800, and the PLL report shows M 188, N 3, the
  rung's C0 and C1 255.
- **F2, timing:** every row of every timing analysis (setup, hold, recovery,
  removal, minimum pulse width, and the max-skew, net-delay and data-delay
  checks) is ≥ 0 at every corner, from #2264's `corners.tcl` and the
  producer's summary.
- **F3, sealing/admission:** the registered producer admits and seals the
  package on its own gates. That means package timing admission, every
  signoff row reviewed, and, where subjects exist, the live retimed-tension
  proof passing.
- **F4, LOADED:** on every corner, the
  `machine_presenter_loaded_flag` → `machine_presenter_loaded_sync[0]`
  route's data delay plus absolute clock skew (#2224's `loaded_route.tcl`)
  is ≤ the rung's allowance in the table above.
- **F5, no new CDC findings:** #2224's `drc_findings.py`, restricted to the
  CDC-* and RDC-* rules, reports no row in the seed that the control lacks.
- **F6, one netlist:** all four seeds record the same raw machine netlist
  SHA-256, and the record carries `model` `486sx`, the rung's
  `machine_clock_hz` and guest rates, and `loaded_rearm_cycles` 8.

At a failing rung, keep all four results and resolve the failing endpoints
to RTL or to a specific constraint. That is reported. It is not repaired
under this method:

- A display-path setup miss or a LOADED route over 3.9 ns is a fail. It is
  not a reason to bring back #2264's unmerged C0 = 26 targets.
- If the SX needs a constraint, basis or proof the DX does not have, the work
  stops and that is reported to the coordinator.

## Acceptance at the selected clock (G2/G4/G5 as in #2194/#2264)

These run at the selected rung's candidate, against the C0 = 34 SX at
`17cbddbec` as the control. Simulation measures behaviour in machine clocks
and guest-virtual time. It is not hardware timing.

- **G2, lockstep L3/L4.** Run `scripts/test_fes_i386_lockstep.py` (level 3,
  #2194's settings) and `scripts/test_fes_i486_lockstep.py --identity
  486sx` (level 4) at the candidate and at `17cbddbec`.
  - Pass: the candidate's report has no failure line that the base's lacks
    (#2194's `g2_compare.py`).
  - The SX CPU's RTL is unchanged, so the x87 suites are not part of the
    SX's G2.
- **G4, DOOM and 3DBench.** Use #2264's `sim_games.py` workloads, windows,
  fixtures and DDR model, through an SX copy that:
  - runs the shared CPU at the SX model's clock, guest rates and derived
    presenter;
  - runs the same workloads at C0 = 34 with main's presenter as the
    control.

  Pass for each workload at the candidate: it reaches its expected end with
  RTL retirements, no `STOP`/`SHUTDOWN` and the expected exit code.
  3DBench must also have captured frames. CPI and the ratio to the control
  are reported, not gated: #2194's DX-33 bound is a DX bound.
- **G5, Quake.** The SX has no 387, so #2194's G5 (the 387 enabled on the DX
  bench) does not apply as written. The SX runs #2264's Quake command
  without `--x87`, at the candidate and at the C0 = 34 control.
  - Pass: the candidate's execution outcome equals the control's. The
    outcome is the end marker, the presence of a `STOP`/`SHUTDOWN`, and the
    exit code. The final captured frame is retained for both.
  - It is reported as an SX regression check, not as a demo run.

## Identity checks

- **G-ID1, DE10:** the DE10 pc386 raw netlist is `cmp`-equal to main's
  (`17cbddbec`), from one checkout path with the same tools. The design,
  evidence, record and toolchain identities are also equal (#2264's
  `de10.sh`).
- **G-ID2, D12:** for MENU, ZX81, Spectrum 48 and 128, Atari ST, C64 PAL
  and PC/XT XT, run #2264's `d12.sh` selection comparison against
  `17cbddbec`.
  - Where the generated identities and Quartus project inputs are
    identical, that shows the input closure is unchanged.
  - Where they differ, the full A/B/C D12 runs (#2228 amendment 1) are
    required.
  - PC/XT's RBF comparison is "not measurable" under #2182, as in #2264.
- **G-ID3, the `486dx` is unchanged.** The candidate's DX must equal main's
  in each of:
  - raw machine netlist (`cmp`);
  - derived presenter and CPU bytes;
  - PLL IP and parent digest;
  - SDC bytes;
  - `record_parameters('486dx')`.

  Its `BUILD_ID` may move only through the closure digests of the pc386
  files this change edits. That is reported.

## Tooling and hygiene

- Every Quartus program runs under the Quartus lease. `uptime` is checked
  before each launch on Powerboat, and chip-30x0-ai-dev is used when
  Powerboat is loaded.
- Any GPU work goes through `gpu-run`. None is expected: the producer is
  Quartus.
- Build output goes under `/data`. nextpnr/Quartus scratch in `/tmp` is
  deleted when done.
- No kit action.
