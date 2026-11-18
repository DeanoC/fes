# #1984: scoping pc386's build identity (proposal, 18 Nov 2026)

A proposal for [#1984](https://github.com/DeanoC/fes/issues/1984), for Deano to decide. It changes nothing: no producer, `scripts/input_identity.py`, lock or AGENTS.md edit, no Quartus run, no kit. Tree: `main` at `1ec592670` (the scripts ran on this branch, whose `sources/misteross` is identical to main's). Path convention: `M/` means `sources/misteross/`.

The aim is that pc386 edits that cannot change the bitstream stop moving `BUILD_ID`, with D12 and the seal unchanged. D12 stays right wherever the ID really moves: on the DE10 a `BUILD_ID` change alone has moved clocks by MHz (#1894, #1926).

**Why this lives in `docs/validation/`.** It is a measured proposal waiting on a decision, with its generator scripts and their outputs, like the #1581 S1 spike records (for example [`2026-11-10-1581-pro-probe`](../2026-11-10-1581-pro-probe/README.md)). The [RTL front-end lanes design](../../design/rtl-frontend-lanes.md) holds decisions that have been made. If Deano accepts this, slice S2 below folds the decisions into that design and into AGENTS.md.

## Result

1. **The issue's premise is out of date.** Since #1951 (merged 17 Nov, 22:25) the *registered* pc386 producer, `M/cores/fes-pc386/tools/build_pc386_product.py`, already writes a format-2 `design-v1` record (`with_identities` in its `create_build_record`). What moves `BUILD_ID` on #1930/#1976/#1983-type edits is not a missing migration. Inside design-v1, the oracle, the lockstep suite and the firmware recipe all have the default `design` role. The old manual sealer, `M/scripts/build_fes_pc386_quartus.py`, still writes a format-1 record. A format-1 `build_identity` hashes the whole record, including the Git `revision`, so the manual sealer's `BUILD_ID` moves on every commit.
2. **The record has 161 files at `1ec592670`; 154 at #1951's sealed head.** Yosys reads 108 of them plus `build/firmware/pc386/pc386-rom.hex` (measured with strace). Quartus is declared to read 8 more (the shell sources and `clocks.sdc`), and Python copies one constraints file into `top.qsf`: 117 in all. Five Python files generate compiler input, and `firmware.lock` pins the firmware images they lay out. The other 38 configure tools, package, record or validate. Eighteen files feed `BUILD_ID` today without being read by a compiler: the oracle, the suite and the lockstep runners (16), and the firmware recipe and lock (2). The recipe is the one real generator among them ([classification](classification.md)).
3. **design-v1's exempt sets cannot be extended.** Adding the oracle and suite to v1's `PACKAGING_INPUTS` makes the sealed #1951 record fail `validate_identities` ("build record identities do not match canonical inputs"). Its 154 files include all 18 candidates. That needs a new policy, `design-v2`, with v1 frozen.
4. **The oracle and suite can leave `BUILD_ID` safely only once the seal process stops importing them.** `scripts/fes_i386_oracle.py` and `scripts/build_pc386_firmware.py` are loaded into the seal process. Case H1 below is a one-file edit to `fes_i386_oracle.py` that changes `top.qsf` while every design parameter stays the same. It would be invisible if that file were simply reclassified, because `quartus_qsf_sha256` is a toolchain parameter.
5. **The firmware can be bound by its output.** On two hosts the pinned images rebuild to `firmware.lock`'s digests: Powerboat (Debian, gcc 14.2.0) and a GitHub `ubuntu-24.04` runner. `pc386-rom.hex` is a pure function of those images: Powerboat's ROM is `815620…9d18`, the same digest the #1951 seal recorded. The producer's existing ROM path check is circular (case R4).
6. **Cost:** four code slices and one pc386 reseal, which is owed anyway. Main's design ID is already `6bf0bfd1…`, not the sealed `bb736910…`, because of x87 RTL changes since #1951. The identity-module slice moves no core's ID (case P1). The producer slice touches files that only pc386's record contains.

## 1. Current state: every file in the build record, classified

[`classify.py`](classify.py) computes the record's file set the way `build_pc386_product.create_build_record` does: its `PINNED_INPUTS` (the legacy sealer's `PINNED_INPUTS` plus 11 product files), `source_closure.imports("build_fes_pc386_quartus")`, and `bootstrap.sh`, `source_closure.py` and `toolchain_cache.py`. Each column is measured:

| Column | How it was established |
| --- | --- |
| Yosys reads | [`trace_yosys_reads.py`](trace_yosys_reads.py) builds the firmware, then runs the seal's own `machine_program` under `strace -f -e trace=open,openat,openat2` with the pc386 lock's Yosys (`e240a4653`). It lists every regular file opened read-only under `M/`. Output: [`yosys-reads.json`](yosys-reads.json), 109 files, Yosys exit 0, 73 s. |
| Quartus reads | Every `VERILOG_FILE`, `SYSTEMVERILOG_FILE` and `SDC_FILE` that the generated `top.qsf` names, plus the files those shell sources `` `include `` through the QSF's `SEARCH_PATH`. This set is **declared by the QSF, not traced**: no Quartus run was made for this record. |
| Inlined in QSF | `constraints-oss.qsf`, which `project_qsf` reads and pastes into `top.qsf`. |
| Generator code | `sys.settrace` while the producer generates the Yosys program, `top.qsf`, the guest-rate parameters and the ROM hex. Lists every file whose code ran. |
| Runs in seal process | Either loaded at import (files under `M/` in `sys.modules` after the producer is imported) or in the static import closure: an AST walk of every import in the producer's modules, function bodies included, so lazy imports such as `pc386_seal_evidence` count. |
| Hashed for lockstep | Files that `oracle_identity` reads only to hash. |

Summary ([`classification.json`](classification.json), full table in [`classification.md`](classification.md)):

| Kind | Files | Examples |
| --- | ---: | --- |
| Supplies compiler bytes (Yosys or Quartus reads it, or it is pasted into the QSF) | 117 | all RTL, the 3 generated headers, 2 vendor `.svh` files, `clocks.sdc`, `constraints-oss.qsf` |
| Generates compiler bytes in the seal process | 5 | `build_fes_pc386_quartus.py` (program, QSF, rates), `build_pc386_product.py` (QSF), `yosys_netlist.py` (the SEAL program), `fes_build_common.py` (target, top, file reads), `build_pc386_firmware.py` (the ROM hex layout) |
| Generates compiler bytes, output pinned | 1 | `firmware.lock` (image digests; the rombios and vgabios sources are bound only through these and are not in the record at all) |
| Configures or authenticates tools (v1 `toolchain`) | 11 | the three locks, `quartus_tools.py`, `pc386_execution.py` |
| Runs in the seal process: packages, records or checks | 11 | v1 packaging modules (including the lazily imported `pc386_seal_evidence.py`), `nextpnr_lease.py`, `pnr_recipe.py`, **`fes_i386_oracle.py`** |
| Validates: oracle or suite, hashed by the seal | 13 | `lockstep_tb.cpp`, `bochs_oracle.cpp`, the Bochs patch, the four `test_fes_i386_*`/`i386_verilator` scripts |
| Validates: runs lockstep and seal as subprocesses | 2 | `build_pc386_with_lockstep.py`, `pc386_run_lockstep.py` |
| Wraps the Quartus command | 1 | `run_logged.sh` |

Checks the script makes:

- No file Yosys or Quartus reads is missing from the record.
- No evidence candidate is read by a compiler.
- Yosys reads one file that is not under source control: the ROM hex in `build/`.
- Two candidates run in the seal process, both at import and in the static closure: `fes_i386_oracle.py` and `build_pc386_firmware.py`. No other candidate is reachable from the producer by any import.

`yosys_netlist.py` is in `fes_i386_oracle.SUITE_SOURCES`, but it is also the seal's Yosys program, so it stays `design` (case D2).

Two more facts the classification depends on:

- **No pc386 compiler run goes through `compiler_read_audit`.** Neither do the PC/XT or ZX81 Quartus producers: no Quartus producer passes `audit_source_root` to `_run_tool`, and none has a `source_closures.json` entry. The read audit and the closure manifest cover the OSS producers only. The audit would also not see the ROM read, because `verify_traces` skips paths under `build/`.
- **The audited producers already leave testbenches out.** `source_closure.minimize` refuses any recorded read under `sim`, `testbench` or `tests`. The generated check finds no such root in any of the 12 manifest entries, so for every audited OSS core an edit to a testbench already leaves `BUILD_ID` alone. PC/XT's closure, which is not in the manifest, records two `tests/` files that its producer reads (`other_producer_closures_with_sim_or_test_roots`).

## 2. Moving the producer onto design-v1, and the migration

**Done in #1951 for the registered producer.** Its `create_build_record` follows the PC/XT (`build_fes_pcxt_quartus.create_build_record`) and ZX81 (`build_fes_zx81_quartus`, #1891) template:

- a format-2 record with `source_roots` and `source_inputs = source_input_closure(...)`;
- Quartus version and threads as toolchain parameters (`quartus_version`, `quartus_threads`), the rest of the recipe hashed into `quartus_recipe_sha256`;
- `front_end = "yosys"`, `front_end_program_sha256` over the program with `$MACHINE_SOURCES`, and `quartus_qsf_sha256` over the QSF with a zero ID;
- the whole thing wrapped in `with_identities(fields, root=root)`.

**How that migration was evidenced.** None of the three claimed D12:

- #1882 introduced the policy. Its production qualification built new MENU, Pong and ZX81 DE10 packages through the normal authenticated producers and took them through kit HIL. Its same-ID controls reproduced the merge-base netlists and RBFs, but "the new-ID netlists change mapping, so the full D12 exception is **not** claimed". PC/XT opted into the policy in that PR, but its package is not in the qualification table.
- #1891 resealed the ZX81 Quartus producer's four models.

 - #1951 "takes the normal exact-head reseal path … it does not claim the D12 no-reseal exception or main-RBF reproduction": a fresh full lockstep (941 corpus files) and a seed-1 Quartus seal with worst setup slack +0.805 ns, `BUILD_ID` `bb7369101d3a…`, package `056729ea…`.

**A design-v2 move is a second one-time design-ID migration.** The v2 domain (`fes-design-v2`) gives every pc386 design a new ID even with unchanged inputs, so AGENTS.md's D12 clause ("the one-time design-ID migration") applies. D12's exception needs a netlist comparison, a main-ID build reproducing main's RBF, and the PR's own timing and seal. That is at least two Quartus builds, plus the lockstep the seal needs. A normal reseal is one Quartus build plus the lockstep, so the recommendation is a **normal reseal**, as in #1882, #1891 and #1951.

**The existing sealed package and record.** The #1951 record stays a valid design-v1 record forever: v1 is not edited, and [`counterexamples.py`](counterexamples.py) shows its stored identity still validates. It is already not main's design (`6bf0bfd1…` against `bb736910…`; the diff is x87 and other i386 RTL, the oracle and suite, the firmware recipe and lock, `fes_build_common.py`, `pnr_recipe.py` and `yosys_netlist.py`). It is not reusable at main for a second reason: `pc386_seal_evidence._historical_sources` takes *today's* CPU-source list and requires each file in the original record. At main that fails on the new `fes_x87_core.sv` (the "reuse" result in [`counterexamples.md`](counterexamples.md)). A v2 ID can never equal a v1 ID, so after migration the #1951 package stays as the historical seal of `8383b03b` and is never selected for a v2 build. **The manual sealer** (format 1, an ID per commit) is superseded by the registered producer. Under the project's delete-superseded rule it should go in its own PR (slice S5). The product imports its library functions, so only its `main`, its format-1 record and the manual lockstep wrapper path would be deleted.

## 3. The evidence set (oracle and lockstep suite)

**v1 cannot take them.** v1's docstring makes its semantics immutable so that "existing sealed records must remain independently verifiable", and the immutability check above is the mechanical answer: editing v1's sets orphans the #1951 seal. v1 also has no slot for "bound but not design" other than `packaging`. Packaging has the wrong meaning: AGENTS.md treats packaging edits as inert for reuse, while a suite edit changes what the seal claims was checked.

**Proposal: `design-v2` adds an `evidence` role** (prototype: [`proposal.py`](proposal.py)):

- **Members (16 files):**
  - the oracle: `i386_oracle.h`, `bochs_oracle.cpp`, `bochs/instrument.h`, the Bochs patch and the provisioner `fes_i386_oracle.py`;
  - the suite: `fes_i386_oracle.SUITE_SOURCES` without `yosys_netlist.py`;
  - the two pc386 lockstep runners.
- **Membership rules,** each one a test in S1/S2:
  - (a) no compiler reads the file: Yosys trace, QSF declaration and, once pc386 runs under it, `compiler_read_audit`;
  - (b) the file is not in the seal process: neither in `sys.modules` after importing the producer, nor in the static AST import closure of the producer's modules (function bodies included, so lazy imports count; `classify.py` has both checks);
  - (c) it is listed explicitly. Unknown files stay `design`, as in v1.
- **Projections:** evidence files stay in `source_inputs`, so they move the **record** identity as now. They leave the **design** projection, and v2 adds a third digest, `identities.evidence`, over them.
- **How the set grows.** The trap that freezes v1 recurs inside v2. A new suite or oracle file defaults to `design`. Once a sealed v2 record holds it as design, reclassifying it would orphan that record.
  - Rule: the v2 sets are append-only, and only for paths that no sealed v2 record contains.
  - The PR that adds a file to the oracle or suite list adds it to the evidence set in the same commit.
  - S2's test enforces this: `ORACLE_SOURCES` and `SUITE_SOURCES` (less `yosys_netlist.py`), the Bochs patch and the provisioner must be a subset of the evidence set.
  - The set itself is a literal in `input_identity.py`, not derived from the lists. A derived set would let an edit to the list, which is evidence, move a file out of `design`. For example, adding `fes_i386_cpu.sv` to the suite list would make it evidence; rule (a) would catch that only in CI.
  - S1's new data file, which lists the oracle and suite for the sealer to hash, is itself an evidence member.

Rule (b) is what makes this sound:

- `fes_i386_oracle.py` is loaded in the seal process today. The sealer imports it for `oracle_identity`, and `check_lockstep_report` calls it before `write_project` and `elaborate_machine`.
- Case H1 appends twelve lines to it that wrap `fes_build_common._regular_input`. They add an `OPTIMIZATION_MODE` assignment to the pin file the producer pastes into `top.qsf`. The generated QSF changes, and so does the record's `quartus_qsf_sha256`, but that is a toolchain parameter, so the prototype v2 design ID does not move.
- H2 does the same to the ROM. So S1 must first make the sealer hash the oracle and suite as data, with no import, before any of them can leave `BUILD_ID`.
- The v1 packaging modules have the same exposure, since they run in the seal process. v1 accepted that through module-boundary review. This proposal does not change it.

**How the seal keeps binding a lockstep report to the exact suite and oracle that produced it:**

1. **At seal time,** unchanged by this proposal:
   - `check_lockstep_report` refuses a report whose `oracle` object (Bochs revision, patch, provisioner, adapter and suite digests) differs from `oracle_identity(root)` of the tree being sealed.
   - It regenerates the CPU-top netlist with the seal's Yosys and requires the report's netlist digest.
   - `require_same_oracle` re-reads the tree after Quartus.
   - Existing tests cover this: `test_oracle_identity_and_digest_evidence_are_required`, `test_each_input_identity_must_match`, `test_cross_root_oracle_source_must_match_running_pin` and `test_a_default_suite_report_must_prove_a_complete_corpus` in `tests/test_build_pc386_product.py`.
   - A stale or different suite's report fails here, whatever `BUILD_ID` does.
2. **In the record:** every evidence file's digest stays in `source_inputs`, and `identities.evidence` summarises them. Cases E1–E3 move the evidence digest.
3. **On reuse:**
   - `scripts/bundle.py` selects a cached entry by `functional_key`, which is the design ID. It then runs `pc386_seal_evidence.validate`, which checks the retained report against the evidence files fetched from the **original record's revision**.
   - Measured: at the sealed head, the validator accepts the #1951 entry, and it still accepts it after the lockstep testbench is edited (`sealed_head_suite_edited: accepted`). Reuse never looks at the current suite.
   - Under v1 that is harmless, because the suite edit also moves the design ID and the lookup misses. Under v2 it would reuse a package whose lockstep ran the old suite.
   - That is the question for Deano (Q2). The options are in section 6.
   - Whichever is chosen, S3 should make `_historical_sources` read the list of required files from the original record, not from today's constants, so that reuse is refused for the right reason.

## 4. The firmware recipe

`scripts/build_pc386_firmware.py` is a real generator. Its `rom_hex()` lays out the bytes Yosys reads through `pc386_rom.sv`'s `$readmemh(ROM_HEX)`. The image bytes are already bound by output: `firmware.lock` pins both images' SHA-256, and `build()` and `images()` raise unless the built images match. The vendored rombios and vgabios sources are not in the record at all. Three things are not bound by output:

- the layout code (`rom_hex`, `ROM_BEAT`, `IMAGES` sizes);
- the ROM path;
- the lock's own non-output fields: dev86 revision, mirror, bxversion, dates.

The producer's ROM check, in `firmware_rom`, compares the written file's path with `root / ROM_HEX`, but both sides come from the recipe's own constant. Measured in case R4: with that constant moved, `firmware_rom` still passes, and the file Yosys reads (the RTL's `ROM_HEX` default) was not written by that step. Yosys then reads whatever is there: a stale ROM from an earlier build, or nothing in a fresh checkout.

| Option | What binds the ROM | Still catches | Stops catching | Cost |
| --- | --- | --- | --- | --- |
| **F0** status quo | the recipe's text and the whole lock | everything, including every non-byte edit (R1, R2 move the ID) | nothing | none, and #1983-type PRs keep needing exemptions |
| **F1** split the recipe | a small design module with the layout (`rom_hex`, sizes, path), imported by the seal. The builder (dev86 fetch, bcc, failure reporting, `--update-lock`) runs only as a subprocess and becomes a firmware generator. The lock stays design. | every byte change: layout edits move the ID, builder edits fail the image-digest check until the lock is updated | builder-only edits such as R1 | a refactor. Lock metadata edits such as #1978's mirror (R2) still move the ID. |
| **F2** output binding (recommended) | `firmware.lock` gains `rom_hex.sha256`, which `--update-lock` writes. The record's `firmware` parameter, already in the design projection, carries the BIOS, VGA BIOS and ROM digests read from the lock. The seal checks the file at the path the RTL reads (`pc386_rom.sv`'s `ROM_HEX` default, which a test pins to the sealer's constant) against the pinned digest, before and after Yosys. That needs no RTL edit. Passing the path by `chparam` instead would mean forwarding a parameter through `pc386_system`, which is an RTL change. The recipe and the lock leave `design`. | every byte change: a layout edit without a new pin makes the seal refuse (R3, H2), a re-pinned one moves the ID (R3u), and a moved path is refused (R4) | recipe and lock edits that leave all three digests unchanged (R1, R2) | the S1/S4 work below. A selection-only record still needs no firmware build, because the pin is in the lock. |

**Reproducibility, for F2's pin.**

- **Powerboat:** `trace_yosys_reads.py` rebuilt dev86 and both images from the vendored sources on Powerboat (Debian, gcc 14.2.0). They match `firmware.lock` (`images_match_firmware_lock: true`), and the ROM hex is `815620361ef2…9d18`, the `rom_hex` digest the #1951 seal recorded on its build.
- **GitHub runner:** CI run [44351051164](https://github.com/DeanoC/fes/actions/runs/44351051164), job `fpga-simulation (pc386, sim-fes-pc386-scroll)` at `1ec592670`, on `ubuntu-24.04`. It fetched and built dev86 and the images, because `run_scroll` calls `firmware.images(download=True)`, which raises unless both images match the lock. The suite passed.
- **What that proves:** the images reproduce on two hosts with different distributions and gcc versions; gcc is only the preprocessor. `rom_hex` is pure Python over the image bytes, so equal images give an equal ROM. CI does not print the ROM digest, so the cross-host ROM equality is derived from the image equality, not measured.
- **If a host ever builds different images,** F2 refuses to seal there rather than seal different bytes under the same ID.

## 5. Counterexamples

[`counterexamples.py`](counterexamples.py) applies each edit to a scratch worktree of `1ec592670` and runs [`measure.py`](measure.py), which calls the producer's own tool-free `create_build_record`, `project_qsf`, `machine_program`, `rom_hex` and the seal's current ROM step, `firmware_rom`, on the edited tree. Columns:

- **v1** is today's design ID.
- **v2 (E+F2)** is the prototype with the evidence role and option F2.
- **v2 (E only)** is the evidence role with firmware option F0.
- **The F2 gates** are the proposed seal checks: the ROM digest equals the pin, and the producer's ROM path equals the path the RTL reads.
- **Today's ROM step** is `firmware_rom` as the seal runs it: whether it passes, and whether it wrote the file Yosys reads.

The harness is calibrated: at #1951's sealed head it reproduces that seal's `BUILD_ID`, `bb7369101d3a5a15d832f58af59c7123`.

| Case | Edit | Should move | v1 | v2 (E+F2) | v2 (E only) | Generated compiler input changed | F2 ROM gate | F2 path gate | Today's ROM step |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| E1 | `lockstep_tb.cpp` (#1930) | no | moves | **stays** | stays | none | pass | pass | pass, wrote it |
| E2 | `bochs_oracle.cpp` (#1976) | no | moves | **stays** | stays | none | pass | pass | pass, wrote it |
| E3 | `test_fes_i386_lockstep.py` | no | moves | **stays** | stays | none | pass | pass | pass, wrote it |
| D1 | `fes_i386_cpu.sv` | yes | moves | moves | moves | (RTL itself) | pass | pass | pass, wrote it |
| D2 | `yosys_netlist.py` (a suite source that is the seal program) | yes | moves | moves | moves | none | pass | pass | pass, wrote it |
| D3 | `constraints-oss.qsf` | yes | moves | moves | moves | QSF | pass | pass | pass, wrote it |
| H1 | `fes_i386_oracle.py` import-time QSF patch | yes | moves | **stays: unsafe until S1** | stays: unsafe | QSF | pass | pass | pass, wrote it |
| H2 | `fes_i386_oracle.py` import-time ROM patch | yes | moves | stays, **seal refuses** | stays: unsafe | ROM | refuse | pass | **pass**, wrote the altered ROM |
| P1 | `input_identity.py` (what S2 edits; v1 packaging) | no | **stays** | stays | stays | none | pass | pass | pass, wrote it |
| R1 | recipe failure-report path (#1983) | no | moves | **stays** | moves | none | pass | pass | pass, wrote it |
| R2 | dev86 mirror URL in recipe and lock (#1978 kind) | no | moves | **stays** | moves | none | pass | pass | pass, wrote it |
| R3 | ROM layout edit, pin not updated | yes | moves | stays, **seal refuses** | moves | ROM | refuse | pass | pass, wrote the new layout |
| R3u | the same edit, pin updated | yes | moves | moves | moves | ROM | pass | pass | pass, wrote it |
| R4 | `ROM_HEX` constant moved; RTL reads the old path | yes | moves | stays, **seal refuses** | moves | path | pass | refuse | **pass, did not write it** |

Reading the table:

- Every "should move" case either moves the v2 ID or makes the proposed seal refuse. H1 is the exception, so slice S1 must come before any reclassification, and S1's test for rule (b) is the counterexample's guard. With the import gone, H1's edit no longer runs in the seal process.
- Every "should not move" case stays.
- Under v1, R4 moves the ID, but nothing refuses the stale ROM, and H2's altered ROM passes today's ROM step too (v1 catches H2 only because the edited file is `design`). The R4 gap exists today, independent of this proposal.
- P1: an `input_identity.py` edit moves no pc386 ID under v1, because the file is in v1's `PACKAGING_INPUTS`. It is in all 14 other recorded closures (`other_producers_recording_each_slice_file`) with the same role.

## 6. Options, recommendation, cost and slices

| Option | What changes | Work | Seals |
| --- | --- | --- | --- |
| A. Status quo | nothing. Per-PR exemptions continue (#1976, #1983). | none | a reseal or exemption each time |
| B. Evidence role only (v2, E + F0) | the oracle and suite leave `BUILD_ID`; the firmware recipe stays design | S1 (oracle half), S2, S3, S4 | one migration reseal |
| **C. Evidence role and firmware output binding (v2, E + F2)**, recommended | B, plus #1983/#1978-type firmware edits leave `BUILD_ID`, and the R4 gap closes | S1, S2, S3, S4 | **one** migration reseal |

**Seals.** The registered producer is already off its seal at main (`6bf0bfd1…` against `bb736910…`), so a pc386 reseal is owed anyway. If S1–S4 land before that reseal, the migration costs no additional seal. The generated blast radius (`other_producers_recording_each_slice_file` in `classification.json`):

- **S2** edits only `input_identity.py`, its tests and docs. It moves no core's `BUILD_ID`: the file is v1 packaging (case P1), and no record changes policy in S2.
- **S1** edits `build_fes_pc386_quartus.py`, `build_pc386_product.py`, `build_pc386_firmware.py`, `fes_i386_oracle.py` and `firmware.lock`. None of the other 14 recorded closures contains any of them, so S1 moves pc386's v1 ID only, with no RTL change. Under D12 that needs a reseal or D12 evidence, unless it is exempted like #1976 and #1983 while pc386 is in progress (Q5). S1 must not edit `yosys_netlist.py` (also in PC/XT's record) or `fes_build_common.py` (in all 14). That is why F2 checks the RTL's default path rather than passing it by `chparam`.
- **S3** edits the parent's `scripts/bundle.py`, which is in no core record, and `pc386_seal_evidence.py`, which is v1 packaging. It moves no ID.
- **S4** is the migration reseal.

**Slices, in order:**

1. **S1: stop running evidence and firmware-recipe code in the seal process.**
   - The sealer reads the oracle and suite lists from a data file and hashes them itself, with no `import fes_i386_oracle`.
   - The ROM hex is written by the firmware recipe as a subprocess, which `build_pc386_with_lockstep` already runs first. The sealer verifies the file at the RTL's `ROM_HEX` path against a digest `firmware.lock` now pins (`rom_hex`), before and after Yosys. This closes R4 under v1 too.
   - Tests: no evidence or firmware-generator file loaded at import or in the static import closure (rule b); H1, H2, R3 and R4 as unit tests.
   - Moves pc386's v1 ID only.
2. **S2: `design-v2` in `input_identity.py`.**
   - v1 untouched and still validated, with the #1951 record as a fixture.
   - New `EVIDENCE_INPUTS` and firmware-generator sets, and the `evidence` identity.
   - The growth test: the oracle and suite lists, the patch and the provisioner are a subset of `EVIDENCE_INPUTS`.
   - The counterexample matrix as unit tests over synthetic records.
   - Moves no `BUILD_ID` (P1).
   - The AGENTS.md wording for the evidence role, and the decisions folded into `rtl-frontend-lanes.md`.
3. **S3: reuse under evidence drift,** per Q2. `_historical_sources` reads its required list from the original record. It must be on main before S4 enables design-v2. Otherwise, in the window between them, an evidence-only edit keeps the design ID, `functional_key` selects the cached package, and `validate` checks only the evidence from the package's original revision (the measured `sealed_head_suite_edited: accepted`). That would reuse packages without the fresh lockstep Q2(b) requires.
4. **S4: the pc386 producer writes design-v2 records,** with the `firmware` parameter carrying `rom_hex`. This is the one-time migration: a normal reseal (fresh full lockstep and the seed-1 Quartus seal) on the PR head. No kit (pc386 HIL is N/A).
5. **S5 (separate cleanup):** delete the manual format-1 sealer path (Q6).

Each is one scope and one PR. S3 depends on S2. S4 depends on S1, S2 and S3: the reuse policy is in force before any design-v2 record exists.

## Questions for Deano

1. **Evidence role.** Should the Bochs oracle, the lockstep suite and the two pc386 lockstep runners (the 16 files listed in `proposal.py`) leave the design ID under a new `design-v2` policy? They would stay in the record identity and get an `evidence` digest. The alternative is to keep them design and keep exempting per PR.
2. **Reuse after an evidence-only change.** When only the suite or oracle changed, and the design ID still matches a sealed package, should the package be reused:
   - **(a)** with its original lockstep, as advisory like toolchain drift;
   - **(b)** only after a fresh full lockstep on the current suite passes against the regenerated CPU netlist (no Quartus, recorded as an additional evidence sidecar, package bytes unchanged); recommended;
   - **(c)** never: a rebuild?
3. **Firmware.** F0 (no change), F1 (split the recipe), or F2 (pin the ROM digest in `firmware.lock` and bind the design to the three output digests)? F2 is recommended.
4. **Migration evidence.** A normal reseal for the v1→v2 design-ID migration (recommended; about half the Quartus time D12 evidence would need), or D12 evidence?
5. **S1's seal.** S2 moves no ID. S1 moves only pc386's v1 ID, with no RTL change. Should S1 merge under the in-progress pc386 exemption, so that S4's migration reseal is the only seal and also covers the x87 RTL drift since #1951? Or should S1 carry its own reseal or D12 evidence?
6. **The manual sealer.** May S5 delete the format-1 manual sealer path, whose ID moves on every commit, now that the registered producer replaces it?

## Found in passing (follow-ups, not fixed here)

- **R4, the circular ROM path check.** `firmware_rom` checks the path it wrote against the same constant it wrote to, not the path `pc386_rom.sv` reads. A moved constant leaves Yosys reading a stale or missing ROM while the check passes. S1 closes it. As a stand-alone fix it would be one test pinning the two paths equal.
- **`_historical_sources` uses today's lists.** Reuse validation of a sealed pc386 entry builds its list of required lockstep sources from today's `CPU_SOURCES` and `SUITE_SOURCES` constants and demands each in the original record. Any later addition to either list, even an evidence-only one, makes every older seal fail validation with "original record omits required lockstep source". The measured case is `fes_x87_core.sv` against #1951. The list should come from the original record. S3 includes this.

## Reproduce

From the repository root on a host with the pc386 lock's Yosys (`e240a4653`), with network access for `--download`:

```bash
python3 -I docs/validation/2026-11-18-1984-pc386-identity-scope/trace_yosys_reads.py sources/misteross /tmp/1984-trace /path/to/yosys-e240a465/bin/yosys
```

```bash
cp /tmp/1984-trace/yosys-reads.json docs/validation/2026-11-18-1984-pc386-identity-scope/
```

```bash
python3 -I docs/validation/2026-11-18-1984-pc386-identity-scope/classify.py sources/misteross
```

```bash
python3 -I docs/validation/2026-11-18-1984-pc386-identity-scope/counterexamples.py --sealed-entry /path/to/9348b73d…/
```

`--sealed-entry` is the retained #1951 entry (package, `build-inputs.json`, `evidence/`). It is needed only for the reuse check. [`sealed-1951-build-inputs.json`](sealed-1951-build-inputs.json) is that entry's record, copied unchanged (record identity `9348b73d…`), for the immutability check. The runs here: Powerboat, 18 Nov 2026, at `1ec592670`.

## Limits

- The Quartus read set is declared by the QSF, not traced. Running Quartus under strace would close that gap.
- The Yosys trace and the counterexamples ran on one host. The firmware's second host is CI, which proves the image digests, not the ROM digest.
- `proposal.py` models the projections; it is not the S2 implementation.
- H1 and H2 show that in-process code can reach the compilers. They do not enumerate every way it can.
