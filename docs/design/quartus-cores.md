# Quartus-built cores

Status: proposed design for issue #1163. Deano approved the policy on
27 October 2026. No build, recipe, manifest or UI behavior in this tree changes
until the steps under [Rollout](#rollout) land. Until then
[core alignment](../core-alignment.md) and the current tree stay the standard.

## Decision

FES allows cores built with Quartus alongside cores built with the OSS flow
(Yosys + nextpnr-mistral). Both ship. A core build says which toolchain made
it, and an image can exclude every Quartus build.

Two reasons:

- **Fmax.** On some cores Quartus closes much faster clocks than nextpnr
  today. The i386 far-transfer change (#1143, record
  [2026-10-26-i386-far](../../sources/misteross/docs/validation/2026-10-26-i386-far.md)) measured Quartus 17.0.2 at
  98.34-104.47 MHz and nextpnr `bd7a1c0d` at 69.43-74.40 MHz on the same RTL.
  The [i386 Quartus oracle](../../sources/misteross/docs/validation/2026-10-25-i386-quartus-oracle.md)
  (#1112) put most of that gap in place and route. #264 records the same split
  for `fes.ramtest` at 130 MHz.
- **DE25-Nano.** Two Terasic DE25-Nano boards are on order for FES bring-up.
  The board has an Agilex 5 FPGA, which the OSS flow does not target. Each
  board ships with its own Quartus Pro license, so two machines can be
  authorized as build hosts when the boards are set up. Quartus Pro 24.1 or
  later is the first release that supports Agilex 5.

## What this replaces

Today the tree says the opposite in several places. When this design is
accepted, these change in the same commit as the first implementation step:

| Where | Today | After |
| --- | --- | --- |
| `docs/core-alignment.md`, standard 1 | "Quartus is an oracle only where the recipe says so... Quartus never programs a kit." | Quartus builds may be sealed packages and may program a kit. Standard 1 names the two toolchains and the labelling rule below. |
| `docs/core-alignment.md`, Rule | Quartus is listed as "an oracle", not a second product path. | Quartus is a second product toolchain with its own label. The rule against a second *mechanism for the same job* still applies to each variant's own build path. |
| `README.md` ("No Quartus!"), the HIP/nextpnr sentences | Quartus is a build-time oracle. | OSS is the default and first-class; Quartus is an opt-in labelled toolchain. |
| `docs/core-packages.md`, `docs/core-development.md`, `docs/bootable-media.md`, `docs/component-boundaries.md`, `docs/development.md` | "package-only"; "never silently falls back to Quartus". | Keep "never silently falls back". Describe the explicit variant selection. |
| `config/core-recipes.toml` `quartus_role` | Free text such as "bring-up/check oracle". | Stays for cores that remain oracle-only. A core with a Quartus variant gets a real toolchain entry (see below). |

What does not change: **a failed OSS route never falls back to Quartus.** A
Quartus variant is something a person or profile selects. It is never what
the build does after OSS fails.

## Policy

1. **Label every core build.** The toolchain is `oss` or `quartus`. It is part
   of the package identity, not a note beside it.
2. **Debian-style split.** `oss` is the "main" set: fully open toolchain.
   `quartus` is the "non-free" set: a proprietary toolchain. An image or
   catalog can be built with only the main set.
3. **OSS-only option.** `FES_OSS_ONLY=1` builds and installs no `quartus`
   core. See [OSS-only images](#oss-only-images).
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

Proposed rule, to confirm in review: a core gets a Quartus variant alongside
its OSS build when, on the same RTL and the same target clocks, either

- the OSS lane misses a clock in the recipe's `clocks` list that Quartus
  closes, or
- Quartus closes the limiting clock at 15% or more above the OSS best seed,
  and a user-visible feature needs that margin.

i386 is the example: about 70 MHz OSS against about 100 MHz Quartus is 40%
more. A core where the gap is 3% ships OSS only. The result for each
candidate goes in a dated note under `docs/validation/`, with both flows' seeds
and clock figures, like the i386 oracle note.

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
# existing lock_path / [recipes.pnr] stay the oss lane

[recipes.quartus]
lock_path = "cores/fes-example/quartus.lock"
edition = "lite"                     # "lite" (17.0.2, Cyclone V) or "pro"
device_family = "cyclonev"           # "agilex5" for DE25-Nano
qsf = "cores/fes-example/constraints.qsf"
sdc = "cores/fes-example/clocks.sdc"
seeds = [1, 2, 3, 4]
clocks = [{ name = "clk_sys", mhz = 100.0 }]   # same meaning as the oss clocks
```

`quartus_role` is kept for cores that have no Quartus variant and stay
oracle-only. It is not removed in the first step, so no existing recipe edit
is needed to land the label.

### Package manifest and identity

The sealed package manifest records `toolchain = "oss" | "quartus"` and, for
`quartus`, the Quartus version, edition and device family. The functional key
(`artifact_cache.functional_key`, see [core-key narrowing](../core-key-narrowing.md))
includes the toolchain, so an OSS package can never satisfy a Quartus lookup
or the reverse. A cache hit is still only a package built from the same
sources by the same toolchain.

Both variants share the core ID and version. They differ in the package ID and
payload digest. An older package with no `toolchain` field reads as `oss`, so
existing sealed packages stay valid.

## OSS-only images

`FES_OSS_ONLY=1` follows the shape of the existing optional-core flags in
`scripts/optional_cores.py`:

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
Quartus Pro builds for Agilex 5 need a license, and Altera ties it to the host
it is acquired on. Each DE25-Nano gives one, so the first two Pro builds are
two hosts. Confirm exactly how the license binds (host ID, no-cost flow or
file) when the first board arrives.

- Quartus runs only on named, pinned build hosts: today the development
  machine for Lite, and the two machines authorized with the board
  licenses for Pro. Each host's Quartus install is recorded by version,
  edition and build number, and `make doctor` reports it.
- The license file and any token stay on the host. Nothing licensed goes in
  the repository, a package, a cache key or CI logs. A package records the
  edition and version, never a seat or host ID.
- Two seats means two concurrent Pro jobs at most. Quartus jobs queue on the
  build host. They do not use the GPU lease that the HIP router uses.
- Quartus 17.0.2's SystemVerilog front end rejects some FES RTL (the i386
  CPU). Where that happens the existing route stays: Yosys elaborates the
  netlist and Quartus maps and fits it, as in `scripts/quartus_fes_i386.py`.
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
  seed and the RBF digest. The two-pass reproducibility check applies. If a
  Quartus lane cannot reproduce byte-for-byte on a pinned host, that is a
  finding to fix, not a reason to skip the second pass.
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

## Manifest, library and UI

- The toolchain label shows wherever a core's identity shows: the catalog and
  library entry, `unavailable-cores.json`, the kit's core list and the
  update manifest. Proposed text is `OSS` or `Quartus`.
- When a core has both variants the library lists one core and offers the
  variant. Open: which one a kit launches by default. Proposal: the one the
  recipe names in `default_toolchain`, and a kit built with `FES_OSS_ONLY=1`
  only ever sees `oss`.
- A person who wants no proprietary-built core can tell from the image
  receipts. The factory image says whether it was built OSS-only.

## DE25-Nano bring-up

Expect Quartus first: the OSS flow has no Agilex 5 target, and nextpnr-mistral
is Cyclone V only. That means:

- a `boards/de25nano` shell beside `boards/de10nano` in `sources/misteross`,
  with its own constraints,
- new cores or shells that have only a `quartus` lane at first, so they are
  absent from any `FES_OSS_ONLY` image,
- an HPS and image story that is separate from this design: the DE25-Nano HPS
  is a different CPU from the DE10-Nano's, so the kit software, target image
  and `libmister-runtime` need their own plan. This document only covers how
  a core is built and labelled.

## Rollout

Each step is one scope, one PR.

1. Docs: update `core-alignment.md`, `README.md` and the `package-only` text
   to match the accepted policy (the table above).
2. Recipe and manifest: `toolchains`, `[recipes.quartus]`, `toolchain` in the
   manifest and the functional key. OSS packages read as `oss`; no existing
   key changes.
3. `FES_OSS_ONLY` and the `toolchain-excluded` reason in `scripts/optional_cores.py`,
   with tests like `tests/test_optional_cores.py`.
4. First dual-variant core: i386 once its OSS and Quartus results are recorded
   against the rule above.
5. Licensed build host and runner, Pro install, `make doctor` report.
6. DE25-Nano board shell and first Quartus-only bring-up core.

## Open questions

1. **Version and lock pinning** for Quartus: per-core or shared lock, and what
   identifies an install (version, build number, installer digest).
2. **CI on unlicensed runners:** skip with a required licensed run, or cached
   sealed packages only.
3. **Manifest and UI labelling:** wording, and the default variant for a core
   that ships both.
4. **The "significantly" threshold:** 15% and a closed-versus-missed clock is
   a starting point.
5. **DE25-Nano:** Quartus Pro front end and whether the Yosys-elaborated
   netlist route is still needed, and where the HPS and image work is tracked.
6. **License binding:** how the board license ties to a host and what happens
   if a build host is replaced.
