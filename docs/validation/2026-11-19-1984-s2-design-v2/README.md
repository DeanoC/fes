# #1984 S2: design-v2 moves no BUILD_ID (19 Nov 2026)

Slice S2 of the [accepted #1984 proposal](../2026-11-18-1984-pc386-identity-scope/README.md#decisions-18-nov-2026) adds the `design-v2` identity policy, with its `evidence` and `firmware-generator` roles, to `sources/misteross/scripts/input_identity.py`. The record says S2 moves no `BUILD_ID` (case P1). This directory measures that claim. It does not take it on trust.

## What changed

- **`scripts/input_identity.py`.**
  - `design-v1` is unchanged: its sets, roles, domains and `with_identities`' default.
  - `design-v2` adds `EVIDENCE_INPUTS` (17 files: the record's 16 plus S1's `oracle_sources.toml`) and `FIRMWARE_GENERATOR_INPUTS` (the recipe and `firmware.lock`). Both leave the design projection and stay in the record identity. v2 adds `identities.evidence` and uses its own domains.
  - A v2 record that holds a firmware-generator input must carry the firmware output digests, `rom_hex` included, in its `firmware` parameter. Otherwise it is refused.
  - `refuse_compiler_read` checks both sets against a measured classification.
  - The superseded constant `POLICY` is replaced by `DESIGN_V1`, `DESIGN_V2` and `POLICIES`.
- **`scripts/export_core_package.py`.** Its S7 front-end contract imported `POLICY` and applied to design-v1 records only. It now applies to every record with separated identities, so a v2 Quartus record still has to name its front end. Like `input_identity.py`, this file has the v1 `packaging` role.
- **Tests:** `tests/test_input_identity_design_v2.py`, with one added case in `tests/test_export_core_package.py`.
- **Docs:** `docs/design/rtl-frontend-lanes.md` (Design identity policies, D14) and `AGENTS.md`.
- No producer passes `policy=design-v2` yet. That is S4.

## Measured: no BUILD_ID moves

[`no_id_moves.py`](no_id_moves.py) measures each tree in its own interpreter, so each side runs its own `input_identity.py` and its own producers:

- **Every support-matrix row, at closure level.** [`design_identity.py`](../2026-11-14-1861-gpu-router-netlists/design_identity.py) (#1861's tool) closes the row's producer's own source roots. That tree's `with_identities` then computes a design-v1 identity over the closure, with empty parameters.
  - This is a stand-in, not the row's `BUILD_ID`. It shows that the design-role closure and the identity function are unchanged.
  - The parameters a producer records are computed by its own code, which is in that closure. The only edited code files are `input_identity.py` and `export_core_package.py`. Both have the v1 `packaging` role, and neither changes the fields a producer passes in.
  - Several rows share a closure; for example, the four ZX81-family rows give one ID.
- **pc386.** The registered producer's own `create_build_record`, run selection-only with no tool. Its record's design identity is the `BUILD_ID` a seal of that tree would carry. `design_identity.py` cannot answer pc386, whose closure is an explicit path set, so this row stands in for it.
- **The sealed #1951 record.** Validated by each tree's module, with its stored design identity recomputed.

Tree A is `main` at `a14663bd9`. Tree B is this PR's latest code commit, the one `no_id_moves.txt` names. Output: [`no_id_moves.txt`](no_id_moves.txt) and [`no_id_moves.json`](no_id_moves.json).

| Row | A | B |
| --- | --- | --- |
| 19 support-matrix design-v1 rows (closure level), and splash (format 1, pinned inputs) | | every row the same |
| fes.pc386 producer (`create_build_record`) | `611961f7042912d5a33492174ac624a8` | `611961f7042912d5a33492174ac624a8` |
| sealed #1951 record (validates on both) | `bb7369101d3a5a15d832f58af59c7123` | `bb7369101d3a5a15d832f58af59c7123` |

The script ends `NOTHING MOVED` and exits 0.

**The check catches a move.** In a scratch tree B, v1's design domain was edited to `fes-design-vX`, and nothing else changed. The script then reported 21 rows as moved and exited 1: every design-v1 producer, the pc386 producer (`611961f7…` → `e1810997…`) and the sealed record, which no longer validates. Splash is a format-1 record and has no design projection, so it stayed the same, as expected.

## The role table

The tests check the v2 sets against the record's generated `classification.json`, not against a hand list:

- **The sets equal the table.** `EVIDENCE_INPUTS` minus `oracle_sources.toml` equals the rows whose `proposed_role` is `evidence`. `FIRMWARE_GENERATOR_INPUTS` equals the `firmware-generator` rows. `oracle_sources.toml` did not exist when the record was measured; its README (section 3) names it an evidence member.
- **A member with no row is refused** unless the caller clears it by a live check. The test clears `oracle_sources.toml` live: it is not in the sealer's machine, shell or include lists, its QSF, SDC or ROM path, it is not named in any of those files, and it is not named in the QSF the producer generates. In the counterexample, `fes_x87_core.sv` loses its row, so the record appears to predate it, and is added to the set. The live check finds it in the machine sources, so it is not cleared and is refused.
- **No compiler reads a member.** `refuse_compiler_read` refuses any member that has `yosys_read`, `quartus_read` or `inlined_in_qsf`, and any evidence member that has `generator_code`. The test gives `fes_i386_cpu.sv` and `constraints-oss.qsf` each role, and gives `yosys_netlist.py` the evidence role. Each is refused. A member edited to `yosys_read` is refused too.
- **The table's `seal_process` column is not used.** It predates S1, and it still marks `fes_i386_oracle.py` as loaded. Rule (b) is checked live instead: every Python member must be in S1's `SEAL_EXCLUDED`, which `tests/test_pc386_seal_process.py` proves is never loaded or importable by the seal.
- **`classify.py` no longer runs at main.** S1 removed `build_pc386_product.firmware`, so the script stops with an `AttributeError`. That is why the committed `classification.json` is the table.

## Reproduce

From the repository root, with a checkout of the base at `<base>`:

```bash
python3 -I docs/validation/2026-11-19-1984-s2-design-v2/no_id_moves.py --tree-a <base> --tree-b . --json /tmp/no_id_moves.json
```

```bash
cd sources/misteross && python3 -m unittest tests.test_input_identity_design_v2 tests.test_input_identity tests.test_export_core_package -v
```

## Left for later slices

- **S3:** reuse after evidence drift, through a fresh lockstep. `pc386_seal_evidence._historical_sources` reads the original record's list.
- **S4:** the pc386 producer writes v2 records. These consumers hard-code v1 and must change with it or before it:
  - `scripts/artifact_cache.py` stores every entry with identities under a `design-v1` directory;
  - `scripts/bundle.py` writes `identity_policy: design-v1` into a reused package's `source_selection`, and `scripts/core_dev_accept.py` refuses any other value;
  - `sources/misteross/cores/fes-pc386/tools/pc386_seal_evidence.py` refuses a pc386 record that is not design-v1 (S3 edits this file);
  - `sources/misteross/tools/identity.py` reports `design_classification: design-v1`.
- **Rules (a) and (b) were measured for pc386's seal only.** Another producer that adopts design-v2 must measure them for its own compilers and seal process first.
- **`classify.py` is broken at main** (above). Repairing it is a follow-up; this PR does not touch it.
- **S5:** delete the manual format-1 sealer.
