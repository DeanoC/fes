# #1984 S4: the pc386 producer writes design-v2 records (20 Nov 2026)

Slice S4 of [#1984](https://github.com/DeanoC/fes/issues/1984), as accepted on 18 Nov 2026 (D14, [decisions](../2026-11-18-1984-pc386-identity-scope/README.md#decisions-18-nov-2026)). The registered pc386 producer, `M/cores/fes-pc386/tools/build_pc386_product.py`, now records with the `design-v2` policy. This is the one-time v1-to-v2 design-ID migration. Under decision 4 it takes a normal reseal, not D12 evidence. The reseal also covers the x87 RTL drift since #1951. Path convention: `M/` means `sources/misteross/`.

## What changed

- **The producer** passes `policy=design-v2` to `with_identities`. Its record's `firmware` parameter now carries three digests: `bios` and `vgabios` as before, plus `rom_hex`, the digest `firmware.lock` pins for the ROM hex Yosys reads. This is option F2 (decision 3); S1 added the pin and the seal's check of the file.
  - The evidence role takes the oracle, the lockstep suite and the pc386 lockstep runners out of the design ID. They get their own `identities.evidence` digest.
  - The firmware-generator role takes the firmware recipe and `firmware.lock` out of the design ID; the design is bound by their outputs instead.
  - All of these stay in `source_inputs`, so the record identity still binds every byte.
- **`scripts/artifact_cache.py`** stores a record with separated identities under its own policy's namespace, `design-v1/` or `design-v2/`. Before this, every such record went under `design-v1/`. Existing v1 entries are where they were, and nothing is relabelled.
- **`M/tools/identity.py`** reports the record's own `identity_policy`, not a hard-coded `design-v1`.
- **Deleted:** S3's test stand-in that patched the producer text to write v2, and its `to_design_v2` helper. The producer writes v2 itself now.
- **Kept:** reading and validating design-v1 pc386 records. Every pc386 seal before this one holds a v1 record, and "historical combined identities are unchanged" (CLAUDE.md). `HistoricalDesignV1Tests` keeps a v1 entry under `pc386_seal_evidence.validate`. `design-v1` itself is untouched. The manual format-1 sealer is S5's.

## The three IDs

| What | Policy | BUILD_ID |
| --- | --- | --- |
| #1951's seal, the last sealed pc386 | design-v1 | `bb7369101d3a5a15d832f58af59c7123` |
| main `91fe3f2aa`, this PR's base, unsealed | design-v1 | `a0b4fd3ef95e2144f6ecb3aa71e7aece` |
| this PR at `53ff80b89`, sealed here | design-v2 | `f023cb1ebd324dc22114b75ab7c57da7` |

**Why pc386's ID moves.** design-v2 hashes the design projection under its own domain, `fes-design-v2`, so a v2 ID never equals a v1 ID, even for identical inputs. That is the planned one-time migration. The base was already unsealed: its v1 ID `a0b4fd3e…` differs from #1951's `bb736910…`, because of the x87 and other i386 RTL changes and S1's producer edits since. This seal is the one decision 5 names: "S4's reseal is the only seal".

## No other core's ID moves

[`no_id_moves.py`](no_id_moves.py) reuses S2's per-tree probe. Each tree is measured in its own interpreter, with that tree's own modules. Its pass rule was fixed before the run:

- every support-matrix closure-level row is the same;
- the pc386 producer row goes from design-v1 on tree A to design-v2 on tree B, with a different ID;
- #1951's sealed record validates on both trees with the same design identity.

Tree A is main `91fe3f2aa` and tree B is `53ff80b89`. Result: **PASS** ([`no_id_moves.txt`](no_id_moves.txt), [`no_id_moves.json`](no_id_moves.json)).

- **19 core rows and splash: same.**
- **pc386: v1 `a0b4fd3e…` → v2 `f023cb1e…`.**
- **#1951's record: validates on both trees, `bb736910…`.**

**The rule catches a non-migration.** A control with main as both trees fails it, with "fes.pc386 producer NOT THE PLANNED MIGRATION" ([`no_id_moves-main-vs-main.txt`](no_id_moves-main-vs-main.txt)).

**Which files moved no ID.** `scripts/artifact_cache.py` is in no core's record. `M/tools/identity.py`, docs and tests are not in pc386's 162-file closure. The only pc386 design-role edit is `build_pc386_product.py` itself.

## The migration seal

All of `53ff80b89`. It ran on chip-30x0-ai-dev, through the registered producer's normal path: `build_pc386_product.py --with-lockstep --gpu-router CUDA --cache-root /data/fes-toolchain-cache --seed 1`, from a clean detached worktree. The run took 11:15:53 to 11:45:46 UTC. That path does three things in order: it builds the firmware, runs the fresh full netlist lockstep, then runs the Quartus 17.0.2 Lite seal with every gate. `--gpu-router CUDA` only selects which toolchain slot is authenticated; nothing routes on a GPU.

| | |
| --- | --- |
| Lockstep | pass: the default suite with a complete corpus (941 files at corpus `459d49fb`), at `53ff80b89` with no tracked changes. CPU netlist `9fbc4a7f…`, report `976af498…` |
| Record | format 2, `design-v2`. `firmware` = `bios` `23b9df91…`, `vgabios` `8522568e…`, `rom_hex` `815620361ef2…9d18`. Identities: design `f023cb1e…`, evidence `ada051ff…`, toolchain `2a79d49d…`, record `7c7fa387…` ([`seal-build-inputs.json`](seal-build-inputs.json)) |
| Timing | pass. Worst slack: setup **+0.140 ns**, hold **+0.091 ns**, recovery +5.998 ns, removal +0.403 ns, minimum pulse width +1.122 ns, all TNS 0. Clocks: system 61.44 MHz, pixel 74.27, audio 12.29 |
| Resources | 36,462 of 41,910 ALMs, 31,997 registers, 282 of 553 RAM blocks, 41 of 112 DSPs |
| Package | `1d2ad78ffe1a3838401c2e159b916722020398b3dc3dde3ddd214027e584fd29`, RBF `f3a35056…`, 3,929,072 bytes ([`seal-build-summary.json`](seal-build-summary.json)) |
| Tools | Quartus 17.0.2 `quartus_sh` `71035cc1…`, Yosys `e240a465` (CUDA slot) |

**The setup margin is thin** but passes the seal's unchanged checks. The three #2022 seals, of other RTL, gave +0.528, +0.432 and +0.081 ns.

**The emitted record was checked before the kit run.** Format 2, `design-v2`, an `evidence` identity, `firmware` keys exactly `bios`, `rom_hex`, `vgabios`. The summary's `rom_hex` is the pinned digest.

**The head still builds the sealed design.** [`head_matches_seal.py`](head_matches_seal.py) runs the producer's own record selection-only on a checkout and compares its design ID with this seal's. At the code commit and at each later head of this PR it prints "SAME DESIGN"; on main it prints "DIFFERENT DESIGN". The commits after `53ff80b89` add only this record, outside pc386's closure.

## Kit B, run `k1984s4a`

Core-only on Kit B's baseline (image `179affbb…`, bundle `c9ae79e2…`, both read from the status file's last FREE line), on 20 Nov 2026 at 13:46–13:48 +02:00.

- **Load:** the sealed package `1d2ad78f`, packed as the canonical `.fcore` with `core_package._ustar_header`. The packer was first checked against #2022's `32409c43` package: byte for byte the same. The package was loaded through the agent under a kit lease, as a fresh library game on the user-supplied drive `real` `a6977555`.
- **Goals:** FreeDOS prompt in 0.6 s. Wolfenstein 3D drawn and walked: 94% of the picture lit, 56% changed after the walk.
- **Restore:** nothing installed, no image change, no reboot. The post-run snapshot has the same image, bundle and boot keys as before. The agent is idle, the lease free, and the run's FogCast data removed.
- **Logged:** claimed and released in `kitB-STATUS.md` ([result](kit-b-k1984s4a-result.json)).

As in the x87 runs, the package ships with the `i386dx+387` identity off.

## Tests

- **[`M/tests/test_pc386_design_v2_record.py`](../../../sources/misteross/tests/test_pc386_design_v2_record.py)** runs the real `create_build_record` on a synthetic checkout, with one edit per case. These are the D14 record's counterexamples. Each assertion message states its claim.
  - The record is design-v2, binds `rom_hex`, and has an evidence identity.
  - E1–E4 (`lockstep_tb.cpp`, `bochs_oracle.cpp`, `test_fes_i386_lockstep.py`, `pc386_run_lockstep.py`) keep the design ID and move the evidence and record IDs.
  - D1–D3 (CPU RTL, `yosys_netlist.py`, the pin QSF) move the design ID.
  - R1 (recipe text, #1983) and R2 (the dev86 mirror in the lock, #1978) keep it.
  - R3u (a re-pinned ROM hex) and a re-pinned BIOS move it.
  - A v2 record without `rom_hex` is refused.
  - The migration moves the cache key and BUILD_ID.
  - #1951's v1 record still validates, `bb736910…`.
- **The tests catch the defects they are about.** Reverting the producer's policy argument fails E1–E4 and R1. Reverting `rom_hex` makes every record refused.
- **[`M/tests/test_pc386_seal_evidence.py`](../../../sources/misteross/tests/test_pc386_seal_evidence.py)**:
  - The sealed-entry fixture is native design-v2.
  - The real resolver's cache entry is under `design-v2/`.
  - `HistoricalDesignV1Tests` validates a pre-S4 v1 entry, and shows that the producer never selects it.
- **`tests/test_artifact_cache.py` and `M/tests/test_identity_report.py`**: each policy gets its own cache namespace and is reported as itself.

## Open, not changed here

- **Firmware-generator drift refuses reuse.** S3 refuses reuse after a recipe- or lock-only change, as its brief required. The D14 record's R1 and R2 expected reuse. Under v2 the ID stays and S3 rebuilds. S3's record left this for Deano to confirm before S4; this PR keeps the conservative behaviour.
- **#2022** (x87 operand probes) changes CPU RTL and was still open when this seal ran. If it merges first, this branch merges main and reseals that head. If it lands later, it does so under the pc386 reseal exemption.
- **S5** deletes the manual format-1 sealer.

## Reproduce

From the repository root:

```bash
python3 -I docs/validation/2026-11-20-1984-s4-design-v2-migration/no_id_moves.py --tree-a <main checkout> --tree-b . --json /tmp/no_id_moves.json
```

```bash
python3 -I docs/validation/2026-11-20-1984-s4-design-v2-migration/head_matches_seal.py .
```

```bash
cd sources/misteross && python3 -m unittest tests.test_pc386_design_v2_record tests.test_pc386_seal_evidence tests.test_build_pc386_product tests.test_identity_report -v
```

The seal, on a host with the pc386 lock's toolchain slot and Quartus 17.0.2 Lite:

```bash
cd sources/misteross && python3 cores/fes-pc386/tools/build_pc386_product.py --with-lockstep --gpu-router CUDA --cache-root /data/fes-toolchain-cache --seed 1
```
