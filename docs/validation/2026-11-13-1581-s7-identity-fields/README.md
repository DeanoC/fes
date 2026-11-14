# #1581 S7: front-end identity fields

S7 of [`docs/design/rtl-frontend-lanes.md`](../../design/rtl-frontend-lanes.md) records which tool
elaborated a core's logic. `toolchain` keeps its meaning, the place-and-route tool; `front_end` is new.

## What a record now says

| Field | Value | Rule |
| --- | --- | --- |
| `parameters.front_end` | `yosys`, `quartus-lite` or `quartus-pro` | On a format-2 record with `toolchain = "quartus"`, and on no other record |
| `parameters.device_family` | `cyclonev` or `agilex5` | The family that front end targets |
| `parameters.quartus_qsf_sha256` | The canonical QSF's digest | On every Quartus record |
| `parameters.front_end_program_sha256` | The canonical Yosys program's digest | Exactly where Yosys elaborates the logic, the same condition as `tools.yosys` |

Format 1 has no validated Quartus discriminator, so a format-1 record carries none of these. The
pc386 and Z80 sealers gain them when they move to format 2 (S9b–S9f, S10).

The recipe registry carries the same pairing. A `toolchain = "quartus"` row names its `front_end`,
and its `[recipes.quartus]` table gains `edition` and `device_family`. An OSS row elaborates with
Yosys and says so by omission. The producer checks the recipe's pair against its `quartus.lock`,
which is what the installed Quartus is authenticated against, and records the lock's value.

Yosys on Pro/Agilex 5 is design decision D3 and is rejected until that decision is taken.

## The two canonical forms

The BUILD_ID derives from the record that holds these digests, so neither form can carry it. Each
also leaves out what changes between runs of one build:

- **The canonical Yosys program** is `elaboration()` with `$SOURCE`, `$WORK` and `$YOSYS_LIB` in
  place of the three absolute roots and a BUILD_ID of 32 zeros. The signoff and repeat runs
  elaborate under different work trees, so without the placeholders no single digest could cover
  both. `executed_elaboration()` renders the program to run and refuses it unless it equals the
  canonical form with the roots and the real ID substituted, so one renderer makes both.
- **The canonical QSF** is the package project's QSF without its `SEED` assignment, the only line
  that varies across a signoff's seeds. The board script refuses to compile a seed whose QSF does
  not reduce to it. PC/XT's QSF has no BUILD_ID line at all: the ID reaches the design through
  Yosys.

Both are written beside the seal summary, as `front-end-program.ys` and `quartus-project.qsf`, so a
reader can recompute either digest by hand. The seal summary itself gains `front_end`, `toolchain`,
`quartus_edition` and `quartus_version`.

The producer records the digests before any tool runs, because the BUILD_ID is derived from the
record. After the build, `require_timing` compares them with what the board script recomputed from
what it actually ran.

## Scope

PC/XT is the only producer in this slice. DE25's `fes.menu` and `fes.zx81` write format-2 Quartus
records through their own exporter, `boards/de25nano/package/export_core_package.py`, which this
slice does not change, so those records still validate unchanged. Their recipe rows carry
`front_end = "quartus-pro"` because the registry is shared. Giving the DE25 records the fields
belongs with S6b or S16.

## D12

`scripts/export_core_package.py` is in every audited closure, so this change moves the BUILD_ID of
every format-2 core with no RTL change. PC/XT moves twice over: its producer and board script are in
its closure, and `quartus_recipe_sha256` hashes the whole recipe entry, which now has three more
keys.

The list is generated, not written by hand:
[`affected_cores.py`](affected_cores.py), output in
[`affected-cores.txt`](affected-cores.txt). Thirteen of thirteen closures hold a changed file:
twelve audited producers through `export_core_package.py`, and PC/XT through that plus its own two
scripts. `fes.pcxt` and `fes.pcxt-dos` share one producer and so one closure.

**The D12 evidence is not in this commit.** [#1752](https://github.com/DeanoC/fes/pull/1752) moves
every BUILD_ID as well and is still open, so evidence gathered against today's main would not
survive it. The campaign runs once main is settled, and this record gains its results before the PR
leaves draft.

## Review fixes

A review of the first two commits found one real defect, fixed in `1a314bde3`.

**DE25 builds would have aborted after the fit.** DE25's producers encode their records with
`boards/de25nano/package/export_core_package.py`, but `boards/de25nano/scanout/async_inputs.py`
read them back through the shared exporter's `build_identity`. A DE25 record is format 2 with
`toolchain = "quartus"` and none of the new parameters, so the new rule rejected it, and both
`build_menu_package.py` and `build_zx81_package.py` call that review on every fit.
`tests.test_build_de25_menu_package` failed with two errors. DE25 now reads its records with the
exporter that wrote them. The two exporters derive a format-2 identity identically, so no DE25
value changes, and nothing else under `boards/de25nano` uses the shared exporter.

**A non-string `front_end` crashed the loader.** `front_end` is optional in the recipe schema, so
the generic non-empty-string check does not reach it, and a TOML array or table raised `TypeError`
from the membership test instead of the loader's own error. Both the loader and the support matrix
now check the type first, and the reject cases cover an array and a Boolean.

A third finding, that the matrix's new cross-check never reaches the two DE25 rows, was refuted:
`validate()` filters the registry to `de10-nano` by design.

## Tests

No core was built for this commit. The new tests:

- one QSF digest covers seeds 1, 2 and 3, and a QSF with no `SEED` line is refused;
- one program digest covers the signoff and repeat runs at two different BUILD_IDs, and a program
  that leaves the canonical form is refused;
- the record binds the front end, its device family and both digests, and the two PC/XT profiles
  differ in both digests;
- a compiled project that differs from the record is refused in each of the three fields;
- a Quartus record that misstates its front end, device family or digests is rejected, in six
  shapes, as is a `quartus-lite` record carrying a program digest and a `yosys` record without one;
- only a Quartus record carries the four fields: a format-1 record with any of them is rejected, and
  so is a format-2 record without `toolchain`;
- two records differing only in `front_end` get different identities;
- an OSS record encodes to the same bytes and the same identity as before;
- a recipe row's front end must fit its edition and device family, in six rejected shapes, and an
  OSS row cannot name one.

The whole repository suite passes on this tree: `python3 -m unittest discover -s tests` is 1,830
tests, 47 skipped, no failure, in 1,019 s, and `docs/support-matrix.py --check` reports ok. In
`sources/misteross`, `tests.test_build_fes_pcxt_quartus` (23, 1 skipped),
`tests.test_export_core_package` (23), `tests.test_build_de25_menu_package` (20),
`tests.test_pnr_recipes` and `tests.test_core_package` pass.
