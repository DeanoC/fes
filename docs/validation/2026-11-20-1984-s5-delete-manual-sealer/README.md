# #1984 S5: delete the manual format-1 pc386 sealer (20 Nov 2026)

Slice S5 of [#1984](https://github.com/DeanoC/fes/issues/1984). It was accepted on 18 Nov 2026 as D14 decision 6 ([decisions](../2026-11-18-1984-pc386-identity-scope/README.md#decisions-18-nov-2026)). Path convention: `M/` means `sources/misteross/`.

## What changed

- **Deleted.** The manual sealer, `M/scripts/build_fes_pc386_quartus.py`, is gone. It wrote a format-1 record, whose ID moves on every commit. The parts deleted with it:
  - its `build`, `main`, `record_fields`, `create_build_record`, `manifest`, `toolchain_label`, `run_quartus` and `require_clean_source`;
  - the `build-fes-pc386-quartus` Makefile target;
  - the manual mode of `M/cores/fes-pc386/tools/build_pc386_with_lockstep.py`, with its fixed `build/sim/fes-i386-lockstep` output and its `--product` flag (the producer's mode is now the only mode);
  - the tests of all of these.
- **Moved.** The seal's shared definitions now live in `M/scripts/pc386_quartus.py`, a library that writes no record. These are the machine and its sources, guest rates, firmware pins, Quartus project, timing gate, tool authentication and lockstep gate. The registered producer reads them, and so do the simulators, the i386 fit tools and the x87 spike.
  - The producer's duplicate `project_qsf` and `write_project` are deleted. The library's versions take the thread count: the producer passes its recipe's count, and other callers keep `ALL`.
  - The producer, `M/cores/fes-pc386/tools/build_pc386_product.py`, imports what it uses explicitly.
  - Its record closure is now the transitive closure of its own `scripts` imports. Before, it was the closure of the old sealer's imports. The 162-file set differs from S4's sealed record only by the rename.
- **Kept.** Historical format-1 pc386 records stay readable. Both seal verifiers still accept them: `M/scripts/verify_pc386_quartus_seal.py`, and `M/cores/fes-pc386/tools/verify_pc386_product_seal.py`, which reads formats 1 and 2. The new test `M/tests/test_pc386_historical_format1_record.py` uses the [PR #1780 seal](../2026-11-13-pr1780-pc386-seal/summary.md). It decodes that record, reproduces BUILD_ID `3e9d57407dad8e3e94f314a927ba9ba2`, and passes the seal through both verifiers. The design-v1 #1951 record is still covered by `HistoricalDesignV1Tests` and the probe below.
- **Docs** that named the old path or target now name the producer. Historical validation records are unchanged, except for two links to the deleted file. Those now point to it at the commits the records describe.

Run against the sealed revision itself, both verifiers still accept #1780's seal. That revision is `9ba4f85d8`, which is not on main, so the unit test reads recorded digests instead. Give the verifier the `firmware.lock` from that revision, because main's BIOS pin has moved on since:

```sh
git show 9ba4f85d8092e27d40e66a27ff1a22c98768055b:sources/misteross/cores/fes-pc386/firmware/firmware.lock > /tmp/fw1780.lock
python3 sources/misteross/scripts/verify_pc386_quartus_seal.py \
    --summary docs/validation/2026-11-13-pr1780-pc386-seal/seal-summary.json \
    --inputs docs/validation/2026-11-13-pr1780-pc386-seal/build-inputs.json \
    --head 9ba4f85d8092e27d40e66a27ff1a22c98768055b --firmware-lock /tmp/fw1780.lock
```

Each verifier prints `inputs.bound 119 files` and `SEAL_OK`.

## pc386's BUILD_ID moves, and nothing else's does

`scripts/build_fes_pc386_quartus.py` was a design-role input of pc386's design-v2 record. So deleting it moves pc386's ID, as does renaming its library or editing it. The move was first measured with the smallest possible deletion, the manual `main()` alone. The result went to Deano. On 20 Nov 2026 he chose to land S5 with the move under the pc386 reseal exemption, without a reseal (relayed by the coordinating session, verbatim "Yes, option (a)"). The next planned pc386 reseal covers it. That reseal is already owed: main's pc386 has been unsealed since #2022, because its design-v2 ID `e5890933…` is not S4's sealed `f023cb1e…`.

[`no_id_moves.txt`](no_id_moves.txt) is S2's per-tree probe, [`../2026-11-19-1984-s2-design-v2/no_id_moves.py`](../2026-11-19-1984-s2-design-v2/no_id_moves.py), run with main `8a162c536` as tree A and this slice's code commit `6fa8e4dcb` as tree B. [`no_id_moves.json`](no_id_moves.json) has the full output. The rule was fixed before the run: every other row the same, and #1951's record validating on both trees with the same identity.

| Row | main `8a162c536` | S5 `6fa8e4dcb` | Verdict |
| --- | --- | --- | --- |
| fes.pc386 producer (design-v2) | `e58909332cf79624c8c6398889ffa4ba` | `bb30395d55b0ccf9f5eb940d4c940b63` | moved, as decided |
| the 20 other support-matrix rows | — | — | all the same |
| sealed #1951 record (design-v1) | `bb7369101d3a5a15d832f58af59c7123` | `bb7369101d3a5a15d832f58af59c7123` | validates on both, same |

The probe prints `MOVED OR UNANSWERED: fes.pc386 producer` and exits 1. It names the pc386 row only, the move decided above.

## Reproduce

```sh
cd sources/misteross && python3 -m unittest tests.test_pc386_historical_format1_record tests.test_pc386_quartus tests.test_pc386_lockstep_seal tests.test_build_pc386_product tests.test_pc386_seal_process -v
python3 -I docs/validation/2026-11-19-1984-s2-design-v2/no_id_moves.py --tree-a <main checkout> --tree-b <this PR checkout>
```
