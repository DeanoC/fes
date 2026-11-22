# #2096: PC/XT sim lint and testbenches as design-v2 evidence (22 Nov 2026)

The measured record behind [#2096](https://github.com/DeanoC/fes/issues/2096). Path convention: `M/` means `sources/misteross/`.

## Why

fes.pcxt-dos's record binds every tracked file under `M/cores/fes-pcxt` (`quartus_fes_pcxt.input_paths` runs `git ls-files`). Under design-v1 every one of those files is a design input, the Verilator lint files `M/cores/fes-pcxt/sim/cpu.vlt` and `chipset.vlt` included. A lint waiver there therefore moved the `BUILD_ID` and needed a reseal.

The Verilator 5.054 CI pin is the case that hit this. Every PC/XT sim fails 5.054 lint: SIMILARNAME in vendor mcl86, and PROCASSINIT on power-on initialisers. The only places to waive those warnings are these two files, because `sim_fes_pcxt.py` takes no extra flags and is itself a design input.

**Decision (Deano, 22 Nov 2026): "Move sim lint out of inputs".** Use design-v2's `evidence` role, as pc386 did in #1984.

## What moves

`PCXT_EVIDENCE_INPUTS` in `M/scripts/input_identity.py` lists 48 files under `M/cores/fes-pcxt/sim`:

| Kind | Count |
| --- | ---: |
| lint configuration (`cpu.vlt`, `chipset.vlt`) | 2 |
| testbench C++ (`*_tb.cpp`, `*.h`, `*.hpp`) | 27 |
| testbench tops (`*_top.sv`, `*_tb.sv`) | 19 |

The producer, `M/scripts/build_fes_pcxt_quartus.py`, now writes design-v2. The 48 files stay in `source_inputs` and are bound by `identities.evidence`. `EVIDENCE_INPUTS` is now the union of the pc386 and PC/XT literals.

## What stays design

- **`M/scripts/sim_fes_pcxt.py`.** The seal runs it: `quartus_fes_pcxt.prepare_elaboration_inputs` executes a frozen copy to call `verify_vendor`, `board_inputs`, `cpu_roms` and `boot_roms`. The producer also imports `cga_font` from it, and `quartus_fes_pcxt` imports `SYSTEM_SOURCES` from it. AGENTS.md never gives the evidence role to code the seal runs.
- **`sim/bios.lock` and `sim/freedos.lock`.** The seal's generator code opens them (measured).
- **The `.S` programs under `sim/`.** They are assembled programs rather than Verilator-only inputs, so they are outside the candidate class.

## Measurement: [`classify.py`](classify.py) → [`classification.json`](classification.json)

The classification ran at `4f1b39754` (main) with the PC/XT lock's Yosys (`3a5accbd7`, sha256 `a17b4c8a…`). It took one CPU-only run of under a minute; no Quartus, GPU or kit. Each candidate under `sim/` is checked for every way the seal could read it:

| Column | Method | Candidates hit |
| --- | --- | ---: |
| `yosys_read` | The DOS-package elaboration (`quartus_fes_pcxt.elaboration`, package and DOS profiles) under `strace`. Yosys opened 69 files under `M/`, 63 of them RTL, and exited 0. | 0 |
| `quartus_read` | The file's name appears in the generated project QSF (declared, not traced). | 0 |
| `inlined_in_qsf` | The file's text appears in the generated QSF. | 0 |
| `generator_code` | An `open()` audit hook over `prepare_elaboration_inputs`, the elaboration program, the project QSF and `dos_assets`. Under `sim/` it opened only `bios.lock` and `freedos.lock`, which are not candidates. | 0 |
| `proof_read` | The two proofs the seal runs, `tests/test_pcxt_video_mailbox.py` (4 tests) and `tests/test_pcxt_audio_cdc.py` (3 tests), under `strace`. Both pass. | 0 |

No candidate was dropped. `refuse_compiler_read` accepts the set against this table.

## Counterexamples on real commits: [`counterexamples.py`](counterexamples.py) → [`counterexamples.json`](counterexamples.json)

Each case is one edit committed in a temporary worktree of the migration commit. The pcxt-dos producer records it as a selection-only record.

| Case | Policy | `BUILD_ID` | `BUILD_ID` moves | evidence moves |
| --- | --- | --- | :---: | :---: |
| main, before this change | design-v1 | `41a7a45217a21c46c34ea84ea3909494` | (one-time move) | |
| migration commit | design-v2 | `e7cfd8ff5a929039b5a62c528ead7a08` | | |
| `cpu.vlt` lint waiver | design-v2 | `e7cfd8ff5a929039b5a62c528ead7a08` | no | yes |
| `chipset.vlt` lint waiver | design-v2 | `e7cfd8ff5a929039b5a62c528ead7a08` | no | yes |
| testbench C++ edit | design-v2 | `e7cfd8ff5a929039b5a62c528ead7a08` | no | yes |
| RTL edit (`pcxt_system.sv`) | design-v2 | `f4131662525cefc98dcd8e24ed9e1883` | **yes** | no |
| `sim_fes_pcxt.py` edit | design-v2 | `75760e3d1de11077141b920400a0c8d3` | **yes** | no |

`M/tests/test_input_identity_pcxt_evidence.py` checks the same properties on every CI run, including that design-v1 would still move on a `cpu.vlt` edit.

## Rules

- **Growth.** A new lint configuration, testbench C++ or testbench top under `M/cores/fes-pcxt/sim` goes into `PCXT_EVIDENCE_INPUTS` in the same commit. The test fails on any tracked file of that kind left out. Each member is also checked live: it must not be in the seal's RTL list or named in the elaboration program or the project QSF.
- **Reuse (Deano, 22 Nov 2026).** After an evidence-only change under `M/cores/fes-pcxt/sim`, the sealed fes.pcxt-dos package may be reused once the pcxt sim targets pass in CI on the new configuration.
- **One-time migration.** Moving to design-v2 changes the `BUILD_ID` once, as every v1-to-v2 move does. This PR therefore carries a normal pcxt-dos reseal and a kit run.
