# FES development ease: investigation and migration proposal

Status: migration direction approved; repository layout not yet implemented.
Investigated 2026-09-20. Stages 1–3 and import/CI tooling are in local implementation; see
[development freshness instructions](development.md#inspect-source-freshness).
The user confirmed that independently releasing FogCast, libmister-runtime and
mister-packages is not an external requirement: optimize for FES development.
Current component instructions continue to apply until migration lands.

## Recommendation

Make FES the development repository for all first-party product sources:
FogCast, runtime, shared definitions, FPGA core implementations and their build
recipes. Preserve their architectural ownership as modules. Keep external
compiler forks and upstream dependencies independently pinned. Do not create a
repository per emulator core now.

A cross-component feature should have one worktree, one PR and one merge. A
merged FES commit should identify the complete first-party source selection.
Independent binaries, core packages and appliance images remain independently
buildable/versioned artifacts; they do not require independent Git repositories.

This is a staged migration, not a recommendation to copy everything into the
root tomorrow. In particular, fix FPGA input identity before broadening the
repository whose commit currently affects synthesized hardware.

## What the current system is making expensive

### Selection is repeated without improving compatibility

FES pins runtime through a gitlink. `scripts/inputs.py` also requires that SHA to
match FogCast's `build/native-runtime.inputs.lock.toml`. FogCast repeats the SHA
in architecture prose, `internal/buildinputs/compat.go` and a smoke fixture.
The expected-runtime function has no production callers; its test checks the
lock. Live compatibility uses protocol/API versions and operation capabilities.

FogCast #286 (`32996fe`) was four one-line SHA replacements. FES refreshed
components in `deade40`, FogCast landed that selection-only change, then FES #87
(`d66016d`) selected FogCast again. The runtime merge → FogCast selection merge →
FES selection merge chain exists even for compatible runtime changes.

These are mostly selection loops, not a cyclic executable dependency graph.
Historical runtime revisions in package oracle fixtures are evidence anchors,
not requirements that every component select the current runtime. Preserve
those historical references when removing redundant current-version pins.

[`scripts/consistency.py`](../scripts/consistency.py) provides valuable contract
enforcement through generated-consumer and copied-definition/fixture checks. The
problem is landing coordinated copies across repositories, not checking them.
Atomic regeneration in one PR retains that protection.

### An unrelated commit changes FPGA identity

In misteross, `scripts/build_fes_coleco_oss.py:create_build_record` includes the
repository revision. `scripts/export_core_package.py:build_identity` hashes the
complete canonical record. The producer injects this identity into `BUILD_ID`
before synthesis. FES `scripts/bundle.py` authenticates a candidate against the
selected producer's canonical record, including revision and recipe identity.

Thus a new repository commit can require a new FPGA artifact even when that
core's functional inputs are unchanged. This is more than a cache inconvenience:
changed identity bits enter synthesis and placement. The
[Coleco integration record](coleco-stream-32k.md) records distinct pre-squash and
selected-revision packages, a failed first selected-revision placement and a
second exact-package hardware diagnostic. Reusing an old package by relabeling
its provenance would be incorrect.

A monorepo without correcting this would amplify the problem: a UI or document
change could alter every core's build identity.

### misteross mixes responsibilities, but is not a large source checkout

At selected revision `744906e6`, `git ls-tree -r -l HEAD` reports 831 tracked
blobs totaling 4,988,826 bytes (about 4.76 MiB). This excludes Git history,
ignored compiler installations and generated build outputs. The latter can be
large regardless of repository boundaries.

There is real organizational coupling: production cores, shared board helpers,
compiler recipes, laboratory experiments and extensive validation documentation
share the project. Several core producers import helpers from `build_fes_pong`.
SMS and SG-1000 directly compile Coleco PLL, RAM, CPU and VDP-related sources.
Splitting those cores now would create additional shared-source pins and update
coordination before there is a clean reusable core SDK boundary.

Toolchain variation is legitimate. The selected root nextpnr lock uses
`d672fade`, while Coleco/SMS/SG-1000 use `0fad53a7`. Latest merged source does not
mean every core must immediately adopt the newest compiler. Preserve qualified
per-recipe compiler selections and migrate them deliberately.

### Green integration does not answer “is everyone's work included?”

Current FES CI checks selected submodules and builds the host. It does not
provide a remote-head freshness report or a component-merge integration queue.
A clean, consistent selection can still omit recently merged component work.
Git submodules intentionally select a recorded commit; fetching does not select
new source automatically ([Git documentation](https://git-scm.com/docs/gitsubmodules)).

On 2026-09-20, read-only `git ls-remote origin refs/heads/main` queries found:

| Repository | Selected and observed remote main |
| --- | --- |
| FES | `3a58ef102e0dc8836fc9cfe970a8373dbf01028b` |
| FogCast | `31a426b15e49a794cb6e90692823dfa2ece7dc01` |
| libmister-runtime | `079b4548d5ab84c094686e00df4fbbe658f4f613` |
| misteross | `744906e6c901419030990907ffa722d1b3f9453a` |
| mister-packages | `41f4d9406955bed7abf318e1a14fde44e500dc92` |

All selections matched at observation time. This is source freshness evidence,
not evidence that the resulting image has booted or passed hardware acceptance.

## Options considered

| Option | Development effect | Decision |
| --- | --- | --- |
| Existing repos plus update bots | Less typing; same multiple merges, credentials and transient selections | Do not build a permanent coordinator around unnecessary boundaries |
| Multiple repos, FES alone selects dependencies | Removes the redundant FogCast pin chain; cross-contract features still span PRs | Useful migration bridge and fallback |
| Consolidate software/contracts; keep all FPGA product source separate | Makes host/runtime changes atomic; core ABI changes still cross repos | Reasonable intermediate stage |
| Consolidate first-party product development; pin external tools/IP | Atomic features and one source truth, while preserving artifact boundaries | Recommended target |
| Separate repository for every core | Adds shared RTL, toolchain and contract update choreography | Defer until an actual independent owner/lifecycle requires it |

## Target module and dependency structure

Illustrative layout; exact path renaming is not a prerequisite for the import:

```
apps/fogcast/          host, UI and network-facing target agent
runtime/              physical lifecycle, programming, input/media delivery
contracts/            schema, emitter, shared ABI and conformance fixtures
fpga/cores/           first-party games, emulators and demos
fpga/shared/          board support, common RTL, reusable CPU/video/audio IP
fpga/tools/           producer SDK and compiler recipes/locks
fpga/experiments/     explicitly opt-in research and bring-up
image/                appliance assembly and release selection
scripts/              development entry points and integration checks
```

Contracts feed runtime, host and FPGA consumers; shared RTL feeds appropriate
cores; the image selects build artifacts. Network/session coordination stays in
FogCast, physical transitions in runtime. Preserve existing Go modules and C++
targets initially, using explicit workspace/local references where needed.
Do not combine languages or processes merely because their sources share Git.

Extract shared producer helpers from the Pong implementation and common RTL
from Coleco ownership into explicit modules. Retain upstream origin, licenses
and update metadata for vendored IP. External compiler forks remain external;
keep one authoritative lock per genuinely different qualified compiler recipe,
not duplicated copies of the same selection in each consuming module.

CODEOWNERS/review routing, module instructions and dependency tests express team
ownership. Shared contracts have consumer-wide checks. Experiments do not enter
the factory profile or hardware acceptance lane just because they live here.

## Build identities and cache design

Introduce a versioned functional-input identity distinct from provenance. Its
closure must include all RTL and includes, generated definitions, shared helper
code, recipe code, constraints, compiler identities, relevant options, seed
policy and environment inputs affecting results. A handwritten top-level file
list alone is insufficient; detect undeclared input reads or conservatively
include entire relevant modules. Never replace the current commit anchor with
an incomplete hash and call it reproducible.

Keep full source commit, source snapshot digest, dirty/development classification
and producer provenance in the build receipt. Keep package content digests and
exact RBF digests authoritative for artifact selection. A later commit with an
identical functional closure may reuse the exact authenticated RBF with a new
selection receipt referencing its original provenance. It must not rewrite old
manifests or claim new hardware evidence. Software integration evidence still
needs reassessment when host/runtime behavior changes.

The embedded identity, package readers, producer and FES canonical-record checks
need a coordinated versioned migration. Continue reading existing package formats;
do not reinterpret their identity semantics. Demonstrate that unrelated docs/UI
changes preserve core identity, and that each relevant input change invalidates
it. Test shared-helper changes, compiler changes, deleted inputs and path escapes.

Use stable authenticated compiler caches outside task worktrees. Current native
development output volumes include the absolute checkout path, and toolchain
locations/authentication complicate relocation. Share immutable installations
and verified outputs; isolate or lease mutable build directories. Do not bypass
authentication with symlinks or allow concurrent writers into one build tree.
Provide supported cache location/resume options instead of ad hoc Python drivers.

Allow a clearly marked local development snapshot for fast uncommitted iteration.
Release/promotion remains tied to a clean committed selection and exact artifact
receipts. A local development build must never silently satisfy a release gate.

## The contributor workflow to build

Proposed behavior, not commands that exist today:

1. One worktree/branch for a feature, including all affected modules.
2. One regenerate/check command for contracts and checked-in generated files.
3. One focused test command computes changed modules plus dependent consumers.
4. Core build commands reuse authenticated artifacts for unchanged input closures.
5. One PR runs affected tests and an always-reported aggregate integration check.
6. Merge tests against the actual current base; publish source/artifact receipts.
7. Hardware qualification promotes exact artifacts under the existing kit lease.

Do not use workflow-level path filtering that leaves required checks pending.
Have an always-running planner/aggregate job with explicit skipped module results.
If the repository's plan and branch settings support GitHub merge queues, use
that existing facility and the `merge_group` trigger. Otherwise use one serialized
integrator that rebases/retests the candidate against current main. Availability
and branch protections have not been inspected; this is not a claim that a queue
is enabled. See [GitHub required-check guidance](https://docs.github.com/en/enterprise-cloud%40latest/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks).

A single status command should report separate facts:

| State | Evidence |
| --- | --- |
| Working tree | Branch, dirty paths, local divergence; never overwrite these |
| Latest merged | Remote commit observed, query timestamp, offline/unknown state |
| Selected candidate | Exact source snapshot and qualified external locks |
| CI verified | Checks and tested source identity, including integration base |
| Built | Artifact digests, input closure and producer receipt |
| Hardware qualified | Exact artifacts, test scope, kit and result |
| Deployed | Observed device image/agent/runtime identities |

Before consolidation, show selected versus remote SHA for each child and a
combined candidate diff. After consolidation, internal freshness is one main
commit. Keep compiler updates explicit and never equate “latest” with “qualified”.

## Migration plan and acceptance gates

Each stage is a bounded reviewable change. Keep the current workflow operational
until its replacement has passed the corresponding gate.

| Stage | Deliverable | Acceptance and rollback |
| --- | --- | --- |
| 1. Make state visible | Read-only status/freshness report with exact selections and remote observation time | Test equal/ahead/diverged/unavailable remotes and dirty trees; no checkout mutation. Revert command if needed |
| 2. Remove redundant selection | FES alone chooses runtime and assembly inputs; Remove the FogCast lock unless a real standalone development workflow needs an optional default; no equality constraint | One runtime selection change passes host/runtime compatibility checks without a FogCast SHA-only PR. Revert coordinated changes to restore old checking |
| 3. Correct build identity | Versioned input closure and provenance receipt; authenticated cache reuse | Negative invalidation tests plus one representative core build and exact-artifact diagnostic. Old readers/packages remain supported; feature stays opt-in until qualified |
| 4. Import software/contracts | FES, FogCast, runtime and definitions in one development tree | Existing focused tests, generated consistency, host and image checks pass; atomic ABI-change rehearsal needs one PR. Keep former repositories usable at cutover tags |
| 5. Import and organize FPGA product modules | Core/shared/producer modules; external toolchains still locked | Same selected source inputs and compiler selections accounted for; simulation and representative build/package/hardware acceptance pass. Roll back import commit before publishing new cutover artifacts if gate fails |
| 6. Retire transition machinery | New CI planner, stable caches, ownership docs and one contributor workflow | Two independent team changes merge without manual internal pin PRs; status identifies merged/built/qualified separately. Archive old development entry points only after this rehearsal |

Start stage 1 next. It supplies useful evidence immediately and remains useful
through migration. Stage 2 removes real daily friction while identity work is
validated. Do not spend weeks building pin-update bots that consolidation removes.

For imports, prefer preserving component history under prefixes with an explicit
cutover mapping, rehearsed on a disposable branch. Compare that against a snapshot
import if preserving history creates unacceptable size/tooling costs. Record old
repository URL, commit, license and PR references either way; retain old repositories
for historical links. No force-push or destruction of existing release history.
Reconcile active team branches at a short announced cutover, not a prolonged
bidirectional mirror with two authoritative main branches.

Repository-root assumptions, hardcoded origin checks, generated relative paths,
Go replacement paths and whole-repository cleanliness checks all need auditing
in the import rehearsal. Preserve strict release cleanliness, while scoped local
module iteration remains possible. Native image identities may change after
migration even with equivalent behavior: new artifacts require their own evidence.

## Success criteria

- A host/runtime/contract feature lands in one PR with zero internal pin-only PRs.
- A docs-only or unrelated UI merge causes zero FPGA synthesis/placement runs.
- Shared RTL/contract changes automatically test their full consumer closure.
- A developer sees selected versus latest merged source in one read-only command.
- A fresh worktree can reuse immutable toolchains without copying installations.
- Build and hardware receipts never imply qualification of untested artifacts.
- Multiple teams can work concurrently without editing a shared integration checkout.

Measure PR/merge count, end-to-end change lead time, cache hits and avoidable FPGA
builds before and after the rehearsal. Avoid inventing duration or cost savings
before measurement.

## Investigation and stage 1 handoff

Base: FES `3a58ef102e0dc8836fc9cfe970a8373dbf01028b`.
Result: uncommitted proposal and stage 1 implementation on `docs/development-ease`,
isolated under `out/dev/development-ease/fes`. `make source-status` reports index
selections, committed selections, local checkout state and bounded remote-main
observations; offline and JSON modes are available. No fetch or checkout occurs.

Validation: 11 new status tests and six existing input tests passed. Tests cover
ancestry, unavailable history, dirty-file/index/ref preservation, staged pins,
uninitialized components, remote failure/timeouts, offline behavior and JSON.
Independent review identified implicit lazy-fetch and ancestry-error handling;
both were corrected and covered. Live observations at 2026-09-20 07:15 UTC
matched the table above. `make check` in the canonical integration checkout
passed (20 generated consumers, 20 fixture copies and four copied source pins).
That checks the selected components, not an imported or restructured source tree.
Documentation links and `git diff --check` passed.

No component pins, shared contracts or hardware changed. No image build or new
hardware acceptance is claimed. Next integration step: land this FES-only slice,
then stage 2 removes redundant runtime selection across FES and FogCast while
preserving demonstrated standalone development workflows. Remaining migration
stages retain the explicit acceptance gates above.


## Implementation progress

See [functional build/reuse evidence](validation/2026-09-20-functional-core-identity.md).
Stage 1 has source freshness reporting. Stage 2 moves external native-artifact
policy into FES and derives the runtime revision from the parent selection;
FogCast checkpoint `e69e92d` removes duplicate selection. Stage 3 producer
checkpoint `5be2940` has versioned functional records and a passing real Coleco
build plus documentation-only exact-artifact reuse. Hardware qualification is
still pending; recipe defaults remain version 1 until that gate.

History-preserving import tooling, real-commit module snapshots, full-repository
container mounts, affected-module CI and a single contract-generation command
have focused tests. They are preparatory code, not evidence that the actual
repository cutover or its image has been validated. Keep the full migration
objective open through import, component tests, build/hardware gates, contributor
workflow rehearsal and final integration.
