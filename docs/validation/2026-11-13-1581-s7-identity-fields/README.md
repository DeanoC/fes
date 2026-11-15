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

**The D12 evidence is in.** [#1752](https://github.com/DeanoC/fes/pull/1752) moved every
BUILD_ID as well, so evidence gathered before it landed would not have survived it; it merged as
`04bf27bb6` and this branch merged main forward, which is what unblocked the campaign.

### Result

Twelve of the thirteen cores discharge D12's conditions 1 and 2. `base` is the merge base
`1a4bb8d41`; each core's netlist and RBF are byte-identical between the base build and a build of
this branch with the base's `BUILD_ID` forced.

| core | base `BUILD_ID` | RBF sha256 | netlist sha256 | reproduced |
| --- | --- | --- | --- | --- |
| `sg1000` | `7934cd7de29e79d7fb035d0228f3a091` | `33c5bfb5d0825e81…` | `2391423461a89232…` | yes |
| `zx81` | `206e0c11ead6c524a5921162bb0485a9` | `2b119ad28633092c…` | `66bf33174544549e…` | yes |
| `sms` | `ae427058d7af3fc0d5b4d1bcdd931857` | `13bdd21e2bcf0a3d…` | `96bd06d9be2499e6…` | yes |
| `coleco_socket_v2` | `4c89ac89fa17748f98eb933d11a42448` | `b96654ed2215a8f5…` | `625f45a5de90868b…` | yes |
| `apple2` | `8f2b0f5c44d80fd3cc859d2279df535b` | `f13e1a07375ff8c7…` | `7bcf92be4a480162…` | yes |
| `spectrum` | `48d10766ec24b2e78dc1d51617d5dfb7` | `dea25f242efbb936…` | `0bb6d9e18fc65beb…` | yes |
| `atari_st` | `33b9e9dd654a7d81a08428145f176f07` | `ab47e3937b51bdbb…` | `d475f5413aa990bb…` | yes |
| `c64` | `8016924c62c6148f47a59e405b61ab1f` | `b0eccc7fe8296302…` | `08644e85311fa460…` | yes |
| `pong` | `1d32b88b34acf5c5e953c2e721bac236` | `b668bbea9891ac6d…` | `951d41c8b2a2d2ac…` | yes |
| `menu` | `177d3678bfc8e8b4187ed489066bef2f` | `f85ec56563d14ee7…` | `fddf97b917a61b9f…` | yes |
| `ramtest` | `7a60de20fc0fb448849cd24eed50a253` | `d3774d8027b61a94…` | `a5e0d01b501fb772…` | yes |
| `riscv` | `e0a17b8e0d06d1b5b5fae611bdedc308` | `a7224f83777d917a…` | `99f92fafc5b0c591…` | yes |

"reproduced" is the load-bearing column, and it is not the same as "matched". Each core's forced
side was built **twice**. A single pair cannot tell an inert change from two runs that produced the
same wrong netlist, and that is not hypothetical: the pinned Yosys `3a5accbd7` exits 0 with a
materially different, smaller netlist about one run in six on this hardware, and segfaults in
`OPT_CLEAN` about one in five (DeanoC/yosys#77). One such run made `spectrum` read as a condition-2
failure here before repetition showed the change was inert. `base == forced1 == forced2` would need
the bad variant three times.

### How it was run

- `sg1000`, `zx81`, `sms`, `coleco_socket_v2`, `apple2`, `spectrum`, `atari_st`, `c64` on
  `chip-30x0-ai-dev` with `--gpu-router CUDA`; `pong`, `menu`, `ramtest`, `riscv` on Powerboat with
  HIP, because those four take no `--gpu-router` and `fes_build_common` pins
  `FES_GPU_ROUTER = "HIP"`, so no NVIDIA-only host can build them (DeanoC/fes#1862 addresses that).
- Per core, both sides in **one checkout path**, one tool install, build directory cleared between
  runs. CLAUDE.md holds that HIP, CUDA and Metal give bit-identical route outcomes and that their
  route evidence is interchangeable; each comparison is internally consistent regardless, since both
  of its sides ran on one host against one install.
- Every build's artifacts are checked for existing before any comparison: a run that exits 0 having
  written nothing is treated as a failure, not as a match. That guard caught `ramtest`, whose
  producer writes `build/fes-ramtest-100` and `build/fes-ramtest-130` rather than a single
  directory.

### What this evidence does not cover

- **`fes.pcxt` and `fes.pcxt-dos` are the thirteenth closure, and only condition 1 is done.** Their
  QSF is generated deterministically from the tree, so it can be compared without running Quartus,
  and it is byte-identical between the base and this branch for **all eight** combinations of
  `package_profile`, `video_mailbox_contract` and `audio_contract` — so the result does not depend
  on which combination the producer uses. The QSF carries no `BUILD_ID` line, only `SEED`, so this
  is plain byte-identity with no exemption invoked. Condition 2 needs a real Quartus 17.0.2 compile
  and has not been run.
- **The forced-build driver is unverified by a control.** Every forced build goes through
  `forced_build_id.py`, which patches `build_identity` and stubs the package export. No run here
  shows that an *unpatched* build reproduces what the driver produces, so a flaw in the patching
  would present as a passing comparison. The campaign for DeanoC/fes#1862 does run that control and
  found its adapters faithful; this one does not.
- **The harness has no clean-tree guard of its own.** The producers do: each calls
  `_require_clean_source` and refuses a modified tree, and both campaign worktrees held no modified
  tracked files. So the evidence is sound, but by the tooling's enforcement rather than this
  harness's.
- **`ramtest`'s 130 MHz variant is not measured.** A default run produces only
  `build/fes-ramtest-100`. If `fes-ramtest-130` is separately sealed, it needs its own comparison.
- Condition 3, the PR's own build passing the core's normal timing and seal gates, is satisfied by
  each base and forced build completing its producer's own checks; no separate seal run was made.

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
