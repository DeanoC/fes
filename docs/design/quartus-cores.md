# Quartus-built cores

Status: accepted policy (issue #1163, Deano, 27 October 2026), including the
15% threshold under [What "significantly" means](#what-significantly-means).
The documentation updates are rollout step 1. Product support is not
implemented yet: current registered recipes and package selection support
HIP/nextpnr only, and `FES_OSS_ONLY` is not a live control. Until the remaining
[Rollout](#rollout) steps land, Quartus is used only as an explicit
recipe-documented oracle/check.

## Decision

FES allows cores built with Quartus alongside cores built with the OSS flow
(Yosys + nextpnr-mistral). The policy allows both labelled variants to ship
when the selection rule below requires them. A core build says which toolchain
made it, and the planned OSS-only image option excludes every Quartus build.

Two reasons:

- **Fmax.** On some cores Quartus closes much faster clocks than nextpnr
  today. The i386 far-transfer change (#1143, record
  [2026-10-26-i386-far](../../sources/misteross/docs/validation/2026-10-26-i386-far.md)) measured Quartus 17.0.2 at
  98.34-104.47 MHz and nextpnr `bd7a1c0d` at 69.43-74.40 MHz on the same RTL.
  The [i386 Quartus oracle](../../sources/misteross/docs/validation/2026-10-25-i386-quartus-oracle.md)
  (#1112) put most of that gap in place and route. #264 records the same split
  for `fes.ramtest` at 130 MHz.
- **DE25-Nano.** Two Terasic DE25-Nano boards are on order for FES bring-up.
  The board has an Agilex 5 FPGA, which the OSS flow does not target. The
  board-associated Quartus Pro entitlement and its host/concurrency terms must
  be confirmed during setup. Quartus Pro 24.1 or later is the first release
  that supports Agilex 5.

## What this replaces

Before this design the tree said the opposite in several places. These were
updated together with the acceptance of the design:

| Where | Was | Now |
| --- | --- | --- |
| `docs/core-alignment.md`, standard 1 | The active producers use HIP/nextpnr; Quartus is an oracle where named. | The accepted policy permits labelled Quartus product variants. The current registry still selects only OSS; this status is explicit. |
| `docs/core-status.md` | The product path uses HIP/nextpnr; Quartus is an oracle, not a product path. | Quartus variants are allowed by policy; the current registered recipes and package selection still use HIP/nextpnr only. |
| `docs/core-alignment.md`, Rule | Quartus is described as an oracle or a separate mechanism. | A Quartus build can be a labelled toolchain variant of the same logical core; it is never an automatic fallback. |
| `README.md`, the HIP/nextpnr sentences | Quartus is a build-time oracle. | OSS is the current implementation; Quartus variants are accepted policy pending recipe and package-selection rollout. |
| `docs/core-packages.md`, `docs/core-development.md`, `docs/bootable-media.md`, `docs/component-boundaries.md`, `docs/development.md`, `docs/getting-started.md` | Some wording could imply that variant selection or `FES_OSS_ONLY` already exists. | State the accepted policy, the current HIP-only resolver, and the pending implementation steps. Never silently fall back. |
| `config/core-recipes.toml` `quartus_role` | Free text such as "bring-up/check oracle". | Stays for cores that remain oracle-only. A core with a Quartus product variant gets a declared toolchain entry (see below). |

What does not change: **a failed OSS route never falls back to Quartus.** A
Quartus variant is something a person or profile selects. It is never what
the build does after OSS fails.

## Policy

1. **Label every core build.** The toolchain is `oss` or `quartus`. It is part
   of the package identity, not a note beside it.
2. **Debian-style split.** `oss` is the "main" set: fully open toolchain.
   `quartus` is the "non-free" set: a proprietary toolchain. An image or
   catalog can be built with only the main set.
3. **OSS-only option.** The design calls for a future `FES_OSS_ONLY=1`
   option that builds and installs no `quartus` core. It is not implemented
   yet; see [OSS-only images](#oss-only-images).
4. **Both variants only when they differ significantly.** If Quartus and OSS
   results are close, ship the OSS build only. Where they differ a lot
   (i386 today), ship both under one core ID, labelled.
5. **OSS stays first-class.** The Quartus figure for a core is the reference
   target for nextpnr. The recipe keeps the OSS lane and its timing record
   current. A Quartus build never lets the OSS lane go stale.
6. **Quartus builds run on licensed, pinned hosts**, with their own timing and
   seal checks. Those checks are never relaxed (see
   [Timing and seals](#timing-and-seals)).

### What "significantly" means

Decided: a core ships a Quartus variant alongside its OSS build when, on the
same RTL and the same target clocks, either

- every Quartus sign-off seed closes a required clock that one or more OSS
  sign-off seeds miss, or
- the 15% test below is met for at least one required clock.

For the 15% test, evaluate each recipe clock separately. Each flow must first
pass every required clock on every declared sign-off seed. For a given clock,
use the highest Fmax among those complete passing seeds for that flow; record
the seed that produced it. The percentage is
`100 × (Quartus best Fmax − OSS best Fmax) / OSS best Fmax`. Quartus qualifies
when that value is at least 15% on any required clock. This defines both the
numerator, denominator and Quartus seed; an exploratory or failing seed cannot
be cherry-picked. This comparison rule does not waive the per-seed timing
checks. Otherwise the core ships OSS only. i386 is the example: about 70 MHz
OSS against about 100 MHz Quartus is about 40% more. A core where the gap is
3% ships OSS only. The result for each candidate goes in a dated note under
`docs/validation/`, with both flows' sign-off seed sets and per-clock figures,
like the i386 oracle note.

## Toolchain label

### Recipe

`config/core-recipes.toml` has one `[[recipes]]` row per `core_id`, with the
OSS `[recipes.pnr]` table. This design keeps one row per core and adds the
Quartus lane to it, because a variant is the same core made by a second
toolchain. Sketch (names to settle in the implementation PR):

```toml
[[recipes]]
core_id = "fes.example"
toolchains = ["oss", "quartus"]      # default ["oss"]
default_toolchain = "oss"            # what an unlabelled selection resolves to
identity_version = 2
producer_script = "scripts/build_fes_example_package.py"
producer_module = "scripts.build_fes_example_package"
authenticate = "_authenticate_tools"
lock_path = "cores/fes-example/toolchain.lock"
gpu_router = "HIP"
hip_architectures = "gfx1100;gfx1201"
selection_filename = "fes-example.package-selection.toml"
package_dir_env = "FES_EXAMPLE_PACKAGE_DIR"
package_selection_env = "FES_EXAMPLE_PACKAGE_SELECTION"
quartus_role = "no oracle required"

[recipes.pnr]
seeds = [1, 2, 3, 4]
timing_weights = [10]
search_weights = [10]
critexp = 2
clocks = [{ name = "clk_sys", mhz = 100.0 }]
freq = "100"
qsf = "cores/fes-example/constraints.qsf"
sdc = "cores/fes-example/clocks.sdc"
router = "gpu"
backend = "HIP"
gpu_device = "lease"
timeout_s = 1800
threads = 1
timing_allow_fail = false
argv_style = "search"
extra = []
env = {}
optional_env = {}
unset_env = []
compress_rbf = true
detailed_timing_report = true

[recipes.pnr.flags]
randomize_seed = false

[recipes.quartus]
lock_path = "cores/fes-example/quartus.lock"
edition = "lite"                     # "lite" (17.0.2, Cyclone V) or "pro"
device_family = "cyclonev"           # "agilex5" for DE25-Nano
qsf = "cores/fes-example/constraints.qsf"
sdc = "cores/fes-example/clocks.sdc"
seeds = [1, 2, 3, 4]
clocks = [{ name = "clk_sys", mhz = 100.0 }]   # same meaning as the oss clocks
```

The dual-lane sketch retains the current complete OSS recipe shape. A
Quartus-only row is a future schema shape: it will omit the OSS `lock_path`,
`gpu_router`, `hip_architectures` and `[recipes.pnr]` fields. The current
`scripts/recipes.py` loader rejects that row; it becomes valid only after
rollout step 2 changes the descriptor and parser.

`quartus_role` is kept for cores that have no Quartus variant and stay
oracle-only. It is not removed in the first step, so no existing recipe edit
is needed to land the label.

### Package manifest and identity

The sealed package manifest records `toolchain = "oss" | "quartus"` and, for
`quartus`, the Quartus version, edition and device family. Package identity
includes the toolchain; both variants share the logical core ID and version
but have distinct package IDs and payload digests.

The build-input JSON passed to `artifact_cache.functional_key` uses an
asymmetric rule to preserve existing OSS cache keys. An absent `toolchain`
field means `oss`. Keep the current OSS functional-input record unchanged and
omit that field from it; adding `"toolchain": "oss"` would change every
existing key because the cache hashes every field other than repository,
revision and source path. A Quartus functional-input record must include
`"toolchain": "quartus"`, so it has a different key from the OSS record.
Newly produced manifests still write the explicit toolchain label. Readers
interpret a missing label in a legacy manifest or cache record as `oss` and
must reject a cache hit whose declared lane conflicts with its recipe or
manifest. The `functional_key` hashing algorithm does not change. Tests for
the implementation must prove that a legacy/current OSS record retains its
key and that the Quartus record gets a distinct key.

## OSS-only images

This is proposed behavior for rollout step 3; `FES_OSS_ONLY` is not
implemented in the current tree. It should follow the shape of the existing
optional-core flags in `scripts/optional_cores.py`:

- It resolves each selected core to its `oss` lane. A core with only a
  `quartus` lane (the DE25-Nano shells, at first) is an error if the profile
  selected it by name, and is omitted with a recorded reason if the profile
  marks it optional. It is never silently built with Quartus.
- The omission is written once in `unavailable-cores.json`, the way an
  optional timing miss is today. This adds a reason beside `timing-not-met`,
  `build-failed` and `skipped`: `toolchain-excluded`.
- `FES_REQUIRE_ALL_CORES=1` with `FES_OSS_ONLY=1` is an error when any
  selected core has no OSS lane, the same conflict rule that guards
  `FES_SKIP_OPTIONAL_CORES` today.
- Verify reuses the recorded toolchain and omissions, ignores the flag, and
  rejects an OSS-only image that contains a `quartus` package.
- `FES_OSS_ONLY` is not `FES_SKIP_OPTIONAL_CORES`. The skip flags are a fast
  path for an unrelated change. The OSS-only flag is a policy: the result is
  an image a person can run with no proprietary tool involved in any core.

## Build hosts

Quartus Lite 17.0.2 (Cyclone V, DE10-Nano) is free and needs no license. The
Quartus Pro builds for Agilex 5 require an authorized license. The DE25-Nano
boards are expected to provide associated Pro entitlements, but the license
binding and concurrent-use terms remain open. Check Altera's applicable
no-cost and board-associated license terms before treating those as the only
authorized hosts or imposing a global job cap.

- Quartus runs only on named, pinned build hosts authorized by the applicable
  license terms. The development machine can run Lite; Pro hosts are
  determined by confirmed entitlements. Each host's Quartus install is
  recorded by version, edition and build number. A future `make doctor` report
  is rollout step 6; the current command does not check Quartus availability.
- The license file and any token stay on the host. Nothing licensed goes in
  the repository, a package, a cache key or CI logs. A package records the
  edition and version, never a seat or host ID.
- Quartus jobs are scheduled within the actual licenses and host
  authorizations available. Do not assume a two-job global cap until the
  license terms and binding are confirmed. They do not use the GPU lease that
  the HIP router uses.
- Quartus 17.0.2's SystemVerilog front end rejects some FES RTL (the i386
  CPU). Where that happens the existing route stays: Yosys elaborates the
  netlist and Quartus maps and fits it, as in
  `sources/misteross/scripts/quartus_fes_i386.py`.
  A Pro-edition front end may not need that. The recipe records which route
  built the package.

## Timing and seals

The checks for a `quartus` build are the checks an OSS build gets, with the
same meaning. Nothing is relaxed because the toolchain is different.

- **Timing.** The build must close every clock the recipe lists in `clocks`,
  on every seed it signs off, using the Quartus timing analyzer on the fitted
  design at the slow corner. A miss makes the lane fail the same way an OSS
  timing miss does: a required core fails the image, an optional core is
  omitted and recorded. No `timing_allow_fail`-style escape is added for
  Quartus.
- **Seal.** A Quartus package is sealed like an OSS package: source closure,
  constraint file digests (QSF and SDC), the Quartus version and edition, the
  seed and the RBF digest. Repeat the same Quartus build on the pinned host and
  compare the RBF digests. If the builds do not reproduce byte-for-byte, that
  is a finding to fix, not a reason to skip the repeat build.
- **No silent fallback.** A failed OSS route does not produce a Quartus
  package, and a failed Quartus lane does not produce an OSS one.
- **Acceptance.** Image inclusion is not hardware acceptance, same as today.
  A Quartus variant gets kit HIL against its own bytes before it is promoted.
  A kit baseline is never promoted silently.

## Pinning

Open. The OSS lanes pin by commit in `toolchain.lock` (`[tool.yosys]`,
`[tool.mistral]`, `[tool.nextpnr]`). Quartus is a binary install, so the pin is
a version string plus an installer or install-tree digest where one exists.
Proposal: a `quartus.lock` per core, or one shared `sources/misteross/quartus.lock`
with per-core overrides, mirroring the OSS split between the repository-level
lock and per-core locks. As with the OSS locks, a core's Quartus version is its
last known good, not a required pin: a newer Quartus that closes timing moves
the lock forward, and the seal and timing checks still apply.

## CI

CI cannot assume a licensed runner.

- Verilator simulation and lint stay independent of synthesis toolchain
  (`scripts/ci_verilator.sh`) and run for every core regardless of toolchain.
- Quartus jobs run only on a licensed, self-hosted runner. On a hosted runner
  they are skipped and reported as skipped, not passed.
- A pull request that changes the inputs of a Quartus lane (RTL, QSF, SDC, the
  Quartus lock) needs a Quartus run on a licensed host before merge, with the
  evidence in the pull request, the way HIL evidence is handled. The
  functional key makes this precise: an unchanged key reuses the cached sealed
  package and needs no run.
- Open: whether a cache of sealed packages is enough for unlicensed
  contributors, and whether the licensed runner is a third CI lane or a manual
  gate on the existing HIL step.

## Catalog, library and image selection

The current publisher and media selector accept one package per `core_id`:
`scripts/core_catalog.py` rejects duplicate catalog IDs and stores one
`package_id`, while `scripts/media.py` requires one package input for each
selected core ID. The rollout must extend selection without putting two
packages for one core into a single image.

- A catalog has one logical entry per `core_id` and a `variants` map keyed by
  toolchain. Each available variant names its distinct package ID, archive
  identity and label. The catalog publisher accepts
  more than one prepared package for a core and verifies each against its
  manifest; it does not emit duplicate core rows.
- The `toolchain` label is visible wherever package identity is presented:
  package manifest, catalog and library entry, `unavailable-cores.json`,
  `inputs.json` and package-selection record, kit core list and update
  manifest. Use the labels `oss` and `quartus` consistently.
- The library displays one logical core and its available variants. A user or
  profile may select a toolchain explicitly; when omitted, selection resolves
  to the recipe's `default_toolchain`, which defaults to `oss`. The chosen
  package ID and toolchain are retained with the selection and launch.
- Parent image selection resolves exactly one package variant for each
  selected core ID before it creates `inputs.json` or calls the media
  publisher. The image receipt records `core_id`, toolchain and `package_id`.
  `scripts/media.py` receives that single resolved package per core ID and
  verifies the receipt and package label. It must reject duplicate core IDs,
  an unselected variant or a package whose toolchain differs from the
  recorded selection. Two variants may be available in the catalog but are
  never both included for one core ID in the same image.
- The planned OSS-only option filters selection to `oss` and records whether
  the image was built OSS-only. Until that option lands, receipts must not
  claim OSS-only filtering.

## DE25-Nano bring-up

Expect Quartus first: the OSS flow has no Agilex 5 target, and nextpnr-mistral
is Cyclone V only. That means:

- a `boards/de25nano` shell beside `boards/de10nano` in `sources/misteross`,
  with its own constraints,
- new cores or shells that have only a `quartus` lane at first, so they are
  absent from an image selected with the planned OSS-only option,
- an HPS and image story that is separate from this design: the DE25-Nano HPS
  is a different CPU from the DE10-Nano's, so the kit software, target image
  and `libmister-runtime` need their own plan. This document only covers how
  a core is built and labelled.

## Rollout

Each step is one scope, one PR.

1. Docs: state the accepted policy and accurately describe current
   implementation status. Done with this design and its status-page updates.
2. Recipe, manifest and cache identity: add declared `toolchains` (existing
   recipes without this field resolve to `oss`) and
   `[recipes.quartus]` while keeping existing OSS recipe fields and functional
   records unchanged. Write the toolchain into new manifests. Preserve legacy
   OSS keys by omitting `toolchain` from OSS functional-input JSON; include
   `"toolchain": "quartus"` for Quartus. Interpret missing labels as OSS.
   Extend `Format2Recipe` and `scripts/recipes.py` to select an exact recipe
   field set by declared lane. An OSS lane keeps the current required
   `lock_path`, `gpu_router`, `hip_architectures` and complete `[recipes.pnr]`
   table. That table keeps its current exact keys (`seeds`, `timing_weights`,
   `search_weights`, `critexp`, `clocks`, `freq`, `qsf`, `sdc`, `router`,
   `backend`, `gpu_device`, `timeout_s`, `threads`, `timing_allow_fail`,
   `argv_style`, `extra`, `env`, `optional_env`, `unset_env`, `compress_rbf`,
   `detailed_timing_report` and `flags`), with `router = "gpu"` and
   `backend = "HIP"`. A Quartus-only row omits those OSS fields and provides
   its Quartus descriptor; do not populate a dummy HIP route to satisfy the
   current parser. Add tests for both lane shapes and key
   compatibility/separation.
3. Implement `FES_OSS_ONLY` and the `toolchain-excluded` reason in
   `scripts/optional_cores.py`, with tests like `tests/test_optional_cores.py`.
   Until this step lands, the variable is not supported.
4. Extend package preparation with an explicit `--toolchain` selector
   (defaulting to the recipe's `default_toolchain`) and `scripts/core_catalog.py`
   to publish one logical core entry with multiple toolchain variants, each
   with a distinct package ID. Extend library
   variant selection and parent profile/default resolution to choose exactly
   one variant per core ID. Keep the
   `scripts/media.py` input contract one package per selected core ID; have it
   validate the resolved toolchain/package identity and reject duplicates or
   mismatches. Test catalog grouping, explicit/default selection, image
   receipts and media rejection paths.
5. First dual-variant core: i386 once its OSS and Quartus results are recorded
   against the rule above. Verify both cache keys and end-to-end catalog/image
   selection.
6. Licensed build host and runner, Pro install and `make doctor` report.
   Schedule jobs against confirmed license entitlements and host permissions.
7. DE25-Nano board shell and first Quartus-only bring-up core, using the
   Quartus-only recipe shape only after step 2's schema and parser change lands.

## Open questions

1. **Version and lock pinning** for Quartus: per-core or shared lock, and what
   identifies an install (version, build number, installer digest).
2. **CI on unlicensed runners:** skip with a required licensed run, or cached
   sealed packages only.
3. **DE25-Nano:** Quartus Pro front end and whether the Yosys-elaborated
   netlist route is still needed, and where the HPS and image work is tracked.
4. **License binding:** how the board license ties to a host and what happens
   if a build host is replaced.
