# #1984 S3: reuse after an evidence-only change (20 Nov 2026)

Slice S3 of [#1984](https://github.com/DeanoC/fes/issues/1984), per decision 2(b) of the [D14 record](../2026-11-18-1984-pc386-identity-scope/README.md#decisions-18-nov-2026). A sealed pc386 package can stand for a tree whose only changes since the seal are to evidence inputs: the Bochs oracle, the lockstep suite and the pc386 lockstep runners (`EVIDENCE_INPUTS`). That reuse needs a fresh full lockstep of the selected tree, which must pass against the regenerated CPU netlist. Quartus never runs, and the package bytes do not change.

## What changed

`M/cores/fes-pc386/tools/pc386_seal_evidence.py` (design-v1 packaging):

- **`evidence_drift`** compares the sealed record with the selected record.
  - It refuses if the design identity differs, or if any changed input has the design-v2 role `design` or `firmware-generator`.
  - Otherwise it returns the changed evidence paths.
  - Packaging and toolchain drift stays tolerated, as reuse already allows.
- **`reuse_gate`** returns one of four results:
  - `unchanged`: no evidence drift. This is always the result for design-v1 records.
  - `refused`
  - `needs-fresh-lockstep`
  - `fresh-lockstep`, with the digest of the recorded run.

  Before it accepts a recorded run, it re-hashes the selected tree and rechecks the run's report with the seal's `check_lockstep_metadata` at that tree. It then requires the run's CPU netlist binding to equal the sealed summary's: netlist digest, Yosys program, CPU source closure and Yosys identity. Finally it rebuilds the receipt, which binds:
  - the package ID;
  - the sealed record's digest;
  - the sealed evidence receipt;
  - the design ID;
  - the selected tree's full evidence projection, including both runners and `oracle_sources.toml`;
  - the lockstep result.
- **`attest_fresh_lockstep`** runs the report through the seal's own gate, `check_lockstep_report`, which regenerates the CPU netlist with the authenticated Yosys. If the regenerated netlist is not the sealed one, it refuses. It records the result as a sealed directory at `build/packages/.pc386-fresh-lockstep/<record id>.<evidence digest>/`.
- **`run_fresh_lockstep`** runs the stock `pc386_run_lockstep.py --netlist --download` in a private run directory, then attests. A failure leaves no record.
- **`_historical_sources`** writes every file in the original record's `source_inputs`, at its revision. It no longer uses today's `SUITE_SOURCES`, `ORACLE_SOURCES` and `CPU_SOURCES`.
- **`validate`** accepts either identity policy, `design-v1` or `design-v2`.

`scripts/bundle.py` (in no core record):

- Every pc386 candidate whose record differs from the selection goes through the gate.
  - A `refused` candidate is skipped and logged, so the resolve becomes a cache miss and the package is rebuilt.
  - A `needs-fresh-lockstep` candidate is held back.
- If nothing else matches, the resolver runs the fresh lockstep and matches again.
  - If the lockstep fails, the resolve is refused. It never falls back to the old package or to a rebuild.
- The selection provenance records:
  - the original record's identity policy (previously hard-coded to `design-v1`);
  - `pc386_evidence_drift`;
  - `pc386_fresh_lockstep_sha256`.

## Counterexamples

[`counterexamples.txt`](counterexamples.txt) is the `unittest -v` output of `EvidenceReuseTests` and `DesignV1ReuseTests` in `M/tests/test_pc386_seal_evidence.py`. Each test is named for its claim.

- **The sealed entry.** It is a synthetic Git checkout sealed under design-v2. Its producer copy is patched to write v2 records, as S4 will. That copy is a stand-in only.
- **The resolver tests.** They run the real `bundle.resolve_core_package` with three stubs:
  - A cache miss stops at the authentication step and prints `cache miss: the resolver would rebuild`.
  - `lockstep-fails` mode makes the fresh-lockstep step fail.
  - `_build_package` raises.

| Counterexample | Test | Result |
| --- | --- | --- |
| Evidence-only edit keeps the design ID and moves only the evidence ID | `…keeps_the_design_id_and_moves_only_the_evidence_id` | drift = `lockstep_tb.cpp` |
| Design input edit (`fes_i386_cpu.sv`) | `…design_input_change_refuses_reuse_and_the_resolver_rebuilds` | refused; resolver misses and would rebuild |
| Firmware-generator edit (`build_pc386_firmware.py`): the design ID is unchanged under v2 | `…firmware_generator_change_refuses_reuse…` | refused; resolver misses and would rebuild |
| Evidence-only, no fresh lockstep, and the lockstep fails | `…without_a_fresh_lockstep_refuses_reuse` | resolver refuses with no fallback; nothing recorded |
| Fresh report that failed, has an incomplete corpus, is not the default suite, or names the sealed suite rather than the current one | `…failed_or_partial_fresh_lockstep_refuses_reuse` | each refused; nothing recorded |
| Runner exits non-zero | `…lockstep_runner_that_fails_records_nothing` | refused; nothing recorded |
| Fresh lockstep of a different CPU netlist | `…fresh_lockstep_of_another_netlist_refuses_reuse` | refused |
| Evidence-only change with a passing fresh lockstep | `…passing_fresh_lockstep_reuses_without_quartus` | the original package is selected and its run's digest is in provenance. `run_quartus`, `elaborate_machine` and `write_project` raise if reached. |
| Earlier evidence state's run reused under a later state | `…earlier_evidence_state_does_not_carry_over` | gate needs a new run; a copied record is refused |
| Run record naming another package | `…record_for_another_package_refuses` | refused |
| Today's lists gain files the sealed record lacks | `…historical_sources_follow_the_original_records_list_not_todays` | the original list is written exactly; a changed digest is refused |
| Under design-v1, an evidence edit moves the design ID | `…design_v1_evidence_change_moves_the_design_id…` | refused: S3 changes nothing for today's records |

Mutation check: with the gate call removed from `bundle.py`, four of these tests fail.

## No ID moves

Measured with [`no_id_moves.py`](../2026-11-19-1984-s2-design-v2/no_id_moves.py), tree A = `main` `e8f922ab6`, tree B = this PR's code commit `be0607de7`. Output: [`no_id_moves.txt`](no_id_moves.txt) and [`no_id_moves.json`](no_id_moves.json).

- **Every closure-level row is the same.**
- **The pc386 producer's design-v1 `BUILD_ID` is the same:** `d7f3f4d8fd5f3559ff3a018b983ac6c9`.
- **The sealed #1951 record validates on both trees,** with `bb7369101d3a5a15d832f58af59c7123`.

## Limits and follow-ups

- **Firmware-generator drift refuses reuse.** That follows this slice's brief ("if any design or firmware-generator input changed, there is no reuse"). The D14 record's cases R1 and R2 instead expect a #1983-type recipe edit to keep the ID *and* be reused. Under design-v2 the ID stays, but S3 rebuilds. This is for Deano to confirm before S4.
- **Historical validation uses the retained record's oracle sources.** The validator derives the report's oracle identity from the source files pinned by the original record, including that revision's `oracle_sources.toml` and provisioner. It checks the report against those historical bytes, so retained-evidence tampering still fails while current oracle/list drift reaches the evidence gate as a fresh-lockstep requirement or cache miss.
  - Focused tests cover both provisioner drift and `oracle_sources.toml` drift, plus rejection of a tampered retained report.
- **The fresh-lockstep record is local to the checkout.** It is not published to the shared artifact cache. The selection provenance names its digest.
- **No real lockstep ran for this record.** The runner call is unit-tested with the job runner mocked. It is the same stock `pc386_run_lockstep.py --netlist --download` that the producer's `--with-lockstep` path uses.
- **Every pc386 record today is design-v1,** so this policy still applies only when the selected and sealed records share a design identity that permits evidence drift (design-v2).
