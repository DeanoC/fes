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

Each case is one edit committed in a temporary worktree of this PR's head (after the DE25 decoupling), measured against main `4f1b39754`. The pcxt-dos producer records it as a selection-only record.

| Case | Policy | `BUILD_ID` | `BUILD_ID` moves | evidence moves |
| --- | --- | --- | :---: | :---: |
| main, before this change | design-v1 | `41a7a45217a21c46c34ea84ea3909494` | (one-time move) | |
| this PR's head | design-v2 | `16a916c1eba005fa38bd3cd80b917035` | | |
| `cpu.vlt` lint waiver | design-v2 | `16a916c1eba005fa38bd3cd80b917035` | no | yes |
| `chipset.vlt` lint waiver | design-v2 | `16a916c1eba005fa38bd3cd80b917035` | no | yes |
| testbench C++ edit | design-v2 | `16a916c1eba005fa38bd3cd80b917035` | no | yes |
| RTL edit (`pcxt_system.sv`) | design-v2 | `05b87dc5c757853c758e3479beafa9eb` | **yes** | no |
| `sim_fes_pcxt.py` edit | design-v2 | `15fdd898d4e0f62d9427a3baa216a002` | **yes** | no |

`M/tests/test_input_identity_pcxt_evidence.py` checks the same properties on every CI run, including that design-v1 would still move on a `cpu.vlt` edit.

## Rules

- **Growth.** A new lint configuration, testbench C++ or testbench top under `M/cores/fes-pcxt/sim` goes into `PCXT_EVIDENCE_INPUTS` in the same commit. The test fails on any tracked file of that kind left out. Each member is also checked live: it must not be in the seal's RTL list or named in the elaboration program or the project QSF.
- **Reuse (Deano, 22 Nov 2026).** After an evidence-only change under `M/cores/fes-pcxt/sim`, the sealed fes.pcxt-dos package may be reused once the pcxt sim targets pass in CI on the new configuration. CI's smoke tier leaves out `sim-fes-pcxt-dos`, which compiles `dos_tb.cpp`, so a change under `M/cores/fes-pcxt/sim` escalates pcxt to its full suite (`FULL_TIER_INPUTS` in `scripts/ci_simulations.py`; tested in `tests/test_ci_gate.py`).
- **One-time migration.** Moving to design-v2 changes the `BUILD_ID` once, as every v1-to-v2 move does. This PR therefore carries a normal pcxt-dos reseal and a kit run.

## DE25 decoupling (Deano, 22 Nov 2026: "Accept + decouple here")

Switching the pcxt-dos producer to design-v2 also moved five DE25 `BUILD_ID`s: atari-st, c64, menu, spectrum and zx81. The DE25 menu producer imported `tree_digest` from `M/scripts/build_fes_pcxt_quartus.py`. Both it and the DE25 ZX81 producer added that module's import closure to their record roots, so the PC/XT producer was a design input of every DE25 record built through them. A producer has to name its own identity policy, so there was no way to make the switch without editing that file.

Deano chose to accept the five moves and decouple in the same PR:

- `tree_digest` moves into `M/scripts/quartus_tools.py`, a toolchain input.
- Neither DE25 producer names `build_fes_pcxt_quartus` any more.
- `M/tests/test_de25_production_identity.py` commits a PC/XT producer edit and checks that neither DE25 record's design or toolchain identity moves. Against the old coupling, the same test fails.
- The five DE25 producers are resealed through the D25 HILQ path (#2052).

## Identity moves at this PR's head: [`identities.py`](identities.py)

All registered producers, compared with the merge base:

| Producer | Moves | Why |
| --- | --- | --- |
| de10-nano:fes.pcxt-dos | `BUILD_ID`, design, toolchain | the design-v2 migration (one time) |
| de25-nano:fes.atari-st (520st), fes.c64 (pal), fes.menu, fes.spectrum (48, 128, plus3, pentagon), fes.zx81 | design, toolchain | the decoupling edits to the DE25 producers (one time) |
| de10-nano:fes.pc386, fes.zx81, fes.zx80, fes.ts1000, fes.ts1500 | toolchain only | `quartus_tools.py`, a toolchain input in their closures, gained `tree_digest`. Their `BUILD_ID` does not move, and toolchain drift is advisory for reuse (AGENTS.md), so they need no reseal. |

No other producer's identity moves.

## fes.pcxt-dos reseal: [`seal-timing.json`](seal-timing.json), [`seal-build-summary.json`](seal-build-summary.json)

The registered producer (`build_fes_pcxt_quartus.py`, Quartus 17.0.2 Lite, Yosys `3a5accbd7`) sealed head `0c16ab4da` on Powerboat. Docs-only commits after that head do not move the `BUILD_ID`.

- **Identity.** `BUILD_ID` is `16a916c1eba005fa38bd3cd80b917035` and the package is `3a92e1fb49fa767a64f34d7848bddf71a49054c5cb05d8ff1c61bd45ed2b4e07`.
- **Timing.** The producer's timing gate passed on all three seeds, with no failing endpoint in any analysed corner:

  | Seed | Worst setup | Worst hold |
  | --- | ---: | ---: |
  | 1 (shipped) | +0.264 ns | +0.121 ns |
  | 2 | +0.578 ns | +0.126 ns |
  | 3 | +0.889 ns | +0.117 ns |

- **Repeat.** An independent seed-1 rebuild reproduced the RBF byte for byte (`104bc9b2…940e`).

## Kit B: [`kit-b/`](kit-b/)

The kit run was core-only on Kit B, claimed and released in `kitB-STATUS.md`, against baseline image `179affbb…` and bundle `c9ae79e2…`. It used the exact package above with the locked FreeDOS 1.4 XT 360 KiB floppy (`140a2d0a…`, matching `freedos.lock`).

- **Boot.** The package launched with an empty drive. The floppy was then inserted through `/v1/library/media/insert` on unit 0, and FreeDOS booted to `A:\>`. `scripts/pcxt_dos_hil_acceptance.py` passed on the 8 captures: the prompt was found at row 21, column 0 with score 1.0, and the boot frames changed ([`acceptance.json`](kit-b/acceptance.json)).
- **DIR.** Typed through the agent input stream as an informational check, it listed the five files on the disk ([`dir.png`](kit-b/dir.png)).
- **Stop and restore.** The session stopped to idle and the run data was removed. The live check confirmed the restore: same image, bundle and boot identity, no pending update, the agent idle and the lease free, and Home on HDMI.
- **Verdict.** PASS ([`verdict.json`](kit-b/verdict.json)).

The first claim did not boot, and it is kept as a harness result rather than counted as a pass. The BIOS had already tried the empty drive and was waiting at "Disk Boot Fail … Press the Any Key". The current core README says inserting a disk into a running guest does not restart its boot; the 31 Oct record predates that change. The second claim pressed Space once at that prompt, and FreeDOS then booted. This PR changes no file under `M/cores/fes-pcxt`. The run did not exercise the host's launch path that sends the floppy with the package (`fogcast/core_initial_disk.go`).
