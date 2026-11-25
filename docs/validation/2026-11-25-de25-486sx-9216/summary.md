# DE25 pc386 486SX at 92.156863 MHz, with a machine clock per model: results

The method is [method.md](method.md), committed before any fit (`eeee860f7`),
with amendment 1 (scope, G7) and amendment 2 (`--seed`), each dated and
committed before the runs it covers. All fits ran on Powerboat with Quartus
Pro 26.3, every Quartus program under a Pro slot of the Quartus lease. The
final head is `d484760a7`; the code is identical at `430b6dd4b`, and every
change after that is docs only.

## Gates

| Gate | Result |
| --- | --- |
| G1, DE10 does not move | **Pass.** The DE10 netlist is `cmp`-equal at the merge base `8d9a63200` and at the branch (`e037580e…`), from one checkout path. The DE10 design, evidence, record and toolchain identities are equal ([g1-de10.txt](g1-de10.txt)). |
| G2, Qsys parent digest review | **Pass** ([parent-review.txt](parent-review.txt), [parent_review.py](parent_review.py)). The 92.16 MHz parent, with the window interconnect's module name set back to the 61.44 MHz one, hashes to the reviewed `10defc9a…`. Every other pinned input has its pin, and the interconnect body is unchanged. `486sx`'s digest is `fefb39d6…`. |
| G3, four seeds through the producer | **Pass.** Seeds 1–4, each the registered producer `build_pc386_package.py --model 486sx --seed N` end to end at `d484760a7`. Every seed fit, every row of every timing summary is ≥ 0 at every corner, and every seed's producer admitted its own package. |
| G4, the record | **Pass.** Each record carries `model` `486sx`, `seed` N, `machine_clock_hz` 92156863 and AUDIO 1/1920. Each PLL report shows M 188, N 3, C0 34 and C1 255. All four seeds compiled one netlist (`d433345a…`). |
| G5, `LOADED`'s route | **Pass.** The worst corner (Slow fix4 0C), as data delay plus clock skew: 2.30, 2.44, 2.68 and 2.88 ns, all under 3.9 ns. |
| G6, tests | **DE25 tests: pass**: 162 tests, among them the new two-model clock, audio-reference counterexample and `--seed` tests, plus the four per-machine builder suites ([logs/de25-tests.log](logs/de25-tests.log)). **`make test-changed`: 2 failures of 2,900 on Powerboat** ([logs/test-changed.log](logs/test-changed.log)): `test_pcxt_on_the_standard_shell` and `test_real_ppi_reset_handshake_and_cancellation`. Both stop at Verilator: the PC/XT lint configuration names rule `PROCASSINIT`, which this host's Verilator 5.032 does not know (it needs 5.054). Both fail the same way on main at the merge base `8d9a63200`, run on that checkout. They are pre-existing on this host, not caused by this PR, and not a pass of G6's `make test-changed` here. |
| G7, D12 for every other DE25 package | **Pass** for all seven ([d12-evidence.txt](d12-evidence.txt), from [d12_evidence.py](d12_evidence.py)); table below. |

## The four seeds (G3–G5)

| Seed | BUILD_ID | ALMs | Setup | Hold | Recovery | Removal | Admitted | `LOADED` route (ns) |
| ---: | --- | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| 1 | `ef995872…` | 41,341.4 | +1.537 | 0.000 | +7.356 | +0.012 | yes | 2.30 |
| 2 | `0233177b…` | 41,384.6 | +1.322 | 0.000 | +6.714 | +0.168 | yes | 2.44 |
| 3 | `b41213cc…` | 41,392.1 | +1.538 | 0.000 | +7.730 | +0.040 | yes | 2.68 |
| 4 | `c7a3545e…` | 41,353.8 | +1.321 | 0.000 | +6.457 | +0.119 | yes | 2.88 |

- Minimum pulse width is +0.220 on every seed.
- The files under [fits/](fits) hold the full per-clock, per-corner rows
  (`summary.json`, read by #2224's `rung_summary.py`), plus each seed's
  build summary, record, producer timing evidence and `LOADED` route.
- The logs are in [logs/](logs).

Earlier runs, kept as diagnostics only:
- **Seed 1 at `561bf8d73`.** It was admitted, but it compiled netlist
  `e15f914a…`, not `d433345a…`. The `--seed` edit moved the build ID, and the
  new worktree path moved Yosys's auto-names. So amendment 2's "seed 1
  stands" did not hold byte for byte, and seed 1 was run again at the final
  head with the others.
- **Seeds 2–4 as SEED copies.** Their admission is **not measurable as run**
  (amendment 2).

## D12 (G7)

- **A:** main at `8d9a63200`, `--compile-only`.
- **B:** this branch's full producer at `d484760a7`.
- **C:** the branch's tooling with A's BUILD_ID forced (#2036's
  `d12_repro.py`).

| Package | A BUILD_ID | A `core.rbf` | C `core.rbf` (A's ID) | B BUILD_ID, RBF, admitted |
| --- | --- | --- | --- | --- |
| MENU | `f1244390…` | `0d505c4e…` | `0d505c4e…`, identical (its compiled RBF; below) | `e6ce2f4b…`, `8f08ec5a…`, yes |
| ZX81 | `f60705d4…` | `93252eab…` | `93252eab…`, identical | `c36c7bde…`, `00aaf84c…`, yes |
| Spectrum 48 | `8e006078…` | `1c733c80…` | `1c733c80…`, identical | `d75cc6c3…`, `b1a70a5f…`, yes |
| Spectrum 128 | `beb9fd96…` | `08051471…` | `08051471…`, identical | `ebd12dfb…`, `37802ca6…`, yes |
| ST | `4fef7932…` | `cffa3a9b…` | `cffa3a9b…`, identical | `d5cb3a7a…`, `3dcfc0d5…`, yes |
| C64 `pal` | `57034e7b…` | `6b243042…` | `6b243042…`, identical | `e18dbf54…`, `a22b0da9…`, yes |
| PC/XT `xt` | `2e78db2a…` | `ab443aec…` | `ab443aec…`, identical | `b5a7591c…`, `ce0b05af…`, yes |

- **The evidence.** [`d12_evidence.py`](d12_evidence.py) writes
  [`d12-evidence.txt`](d12-evidence.txt), with every root it read and every
  digest in full:
  - A's and C's full RBF SHA-256, and whether they are equal;
  - B's BUILD_ID, RBF and admission;
  - the project inputs (`d12_cmp.py`'s file set), A against B. Each side's
    root path and its own BUILD_ID (hex, 128-bit binary and decimal) are
    replaced by one token, and every file that still differs is listed.
- **Project inputs, A against B.** Every package has the same file set on
  both sides. Six files differ before normalisation, by root path or
  BUILD_ID only:
  - the scanout component's `.ip`, `_bb.v`, `_inst.vhd` and
    `synth/…_scanout.v`;
  - `peripheral_subsys.qsys`;
  - `integrate-mailbox.tcl`.

  After normalisation, "differ otherwise: none" holds for all seven packages.
  `d12_cmp.py`'s own output, paths only, is in `logs/d12-cmp-*.txt`.
- **MENU's C** compiles and then exits 1 at its asynchronous-input review:
  "source/BUILD_ID mismatch". The forced ID is that mismatch, as in #2036.
  Its producer therefore copies no `core.rbf`. The RBF it compiled
  (`signoff/combined/hps/linux-boot.core.rbf`, the file the producer copies
  to `core.rbf`) equals A's, `0d505c4e…`, byte for byte.
- **PC/XT** reproduces here. #2182's "not measurable" therefore does not
  apply to this round.
- Earlier D12 runs at `561bf8d73` (A for five packages, B for seven, C for
  six) were stopped when `--seed` came in, and are kept as diagnostics in
  `logs/`.

## What follows

- **#2194** adds `486dx` in pc386's files only: one `MODEL_CLOCKS` entry in
  `pc386/model_clocks.py` (C0 = 51, its IP and digests) and its `MODELS`
  entry. No shared file changes, so no D12.
- **The ~130 MHz step:** period-tied crossing bounds and a re-argued
  `LOADED` (#2224).
- **No kit run.** DE25 pc386 has no kit path until slice 4's runtime (#2214,
  #2215).
