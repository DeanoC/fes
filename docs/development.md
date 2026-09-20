# Development through FES

FES contains the host, target runtime, shared contracts and FPGA product sources
in one Git repository. Their ownership boundaries remain separate; their files
under `sources/` are tracked modules in the same FES commit. Start with
[AGENTS.md](../AGENTS.md), [the project map](project-map.md) and the owning module's
instructions. Read [getting started](getting-started.md) for machine setup and
[the agent workflow](agent-workflow.md) for assignments.

## Inspect source freshness

`make source-status` reports the checkout and selected commit without fetching,
changing refs or writing Git indexes:

```sh
make source-status
make source-status SOURCE_STATUS_ARGS='--offline'
make source-status SOURCE_STATUS_ARGS='--json --timeout 5'
```

Imported module rows identify the real FES commit, module path and subtree ID.
They share the FES `main` remote observation; they do not have independent branch
tips to reconcile. Dirty paths are scoped to each module. Staged or unstaged
module edits are visible, but build selection still refers to committed bytes.

Online observations include UTC timestamps. `equal`, `behind`, `ahead` and
`diverged` describe available Git history. `different-history-unavailable` means
the commits differ but ancestry cannot be established locally. Offline, failed
or timed-out observations remain unknown. A cached remote-tracking ref is never
presented as a fresh remote observation. Historical gitlink checkouts remain
readable by the status tool; those compatibility rows still distinguish indexed
and committed pins.

Status is informational: it does not establish compatibility, CI success, build
completion or deployed identity. Use `make check` for committed-source and
consumer consistency.

## Isolate component work

Create one FES worktree for a task, including tasks that span several modules:

```sh
mkdir -p out/dev
git worktree add -b feat/launch-status "$PWD/out/dev/launch-status" HEAD
cd out/dev/launch-status
```

Edit the owning files directly under `sources/FogCast`,
`sources/libmister-runtime`, `sources/mister-packages` or `sources/misteross` in
that worktree. Shared-contract changes and their consumers belong in the same
FES branch and PR. There is no internal pin-update PR chain. Independent tasks
use different FES worktrees; workers sharing one task agree disjoint file
ownership before concurrent edits. Do not create nested component worktrees.

For local iteration, run the owning module's tests or the
[affected software command](test-changed.md):

```sh
python3 scripts/test_changed.py --base origin/main --plan-only
python3 scripts/test_changed.py --base origin/main
make check-generated
```

After a shared definition or fixture changes, use `make generate`, review the
resulting consumer changes and run affected tests. Commit the reviewed FES
change when authorized, then run `make check` and the appropriate build. Parent
builds consume real committed FES snapshots under ignored `out/work/`; they do
not build arbitrary unstaged module edits. A snapshot retains the actual FES
repository and commit, with the module's path and tree identity. It is not a
synthetic standalone component commit. Do not edit builder-managed snapshots.

The import mapping in [config/source-imports.toml](../config/source-imports.toml)
records original repositories, prior gitlinks, imported commits and module trees.
The import commit retains original histories as parents. That local history
mapping does not assert that cutover has been published or hardware-qualified.
When moving from a submodule checkout, preserve old component branches,
worktrees and uncommitted work. Prefer a separate fresh checkout of the reviewed
import revision; do not delete old module directories or reset them to make the
new layout fit.

## Validate at the right scope

| Command from FES root | Purpose |
| --- | --- |
| `make test` | Parent/platform/image-recipe regression suites; prepare their prerequisites |
| `python3 scripts/test_changed.py --base REF` | Affected software tests, including dependent consumers |
| `make generate` / `make check-generated` | Regenerate mapped consumers / check without writing |
| `make check` | Committed module selection, FES policy and generated-definition consistency |
| `make doctor` | Selected profile and build prerequisites |
| `make host` | Compile selected linux/amd64 host outputs and `host.json` |
| `make dev` | Incremental diagnostic native image using a persistent base |
| `make build` | Build selected host and cold two-pass native image outputs |
| `make verify` | Verify published outputs against receipts and child checks |
| `make media` | Assemble a flashable disk image from verified cold outputs |
| `make verify-media` | Verify `media/current/fes.img` and its host-side evidence without deployment |

The default and only FES integration profile is `native-integration-dev`.
Build and verify do not deploy. The profile authenticates the pinned open-source
misteross HIP/nextpnr tools before selecting the ordered
`fes.pong`, `fes.zx81`, `fes.coleco` package set. See [described FPGA core
packages](core-packages.md) for first-checkout setup and the inspect/load/Stop
workflow. Host-only builds do not require those tools.
The clean, development, verification and media paths are package-only and all
reuse the same closed package set.
Systems whose nextpnr route is not implemented yet use an explicit Quartus
oracle/check documented by that system's recipe. Quartus is never an automatic
fallback and never creates a legacy image bundle.

During component development, use the narrow relevant component tests and
build only changed artifacts. For target diagnostics, follow the selected
FogCast development guide's fast loop using a disposable copy of a verified
image. Label those results diagnostic. Reserve expensive full image and
two-pass reproducibility checks for stabilized integration; full rebuilds
are also required when image configuration, packaging or locked inputs
change. One parent build runs per checkout, enforced by its existing lock.
Coordinate shared expensive runs rather than starting one per agent.

## Incremental native image

Run `make dev` for the selected `native-integration-dev` revisions. It publishes
`out/native-integration-dev/development/linux.img` and a `development.json`
receipt after structural validation. It uses the same selected package set,
FES `image/` overlay and image recipes as the clean build. The native image
contains the locked idle RBF and the same closed `fes.pong`, `fes.zx81`,
`fes.coleco` package set. It does not deploy, run QEMU,
produce two-pass evidence, or replace the clean image and receipts.

The development Buildroot volume retains the compiler, libraries and package
outputs. The Go agent uses Go's compilation cache; the runtime package is cleaned
and rebuilt when its selected commit changes. An unchanged complete output is
reused after receipt/hash checks. Otherwise, full Buildroot finalization runs to
install the current agent, locked idle RBF, selected package set and build-input record. A package
selection change retains the compiler/base volume but forces final image
assembly. The development receipt binds the emitted package selection and the
exact external `manifest.toml` and `core.rbf` bytes.
A failed build leaves no development success receipt; the next invocation can
resume package compilation.

A compatible existing clean pass can seed a new development volume. FES checks
its source/profile/Go identities, receipt hashes, both recorded image hashes and
the actual pass-two image before copying it with symlinks and modes preserved.
The clean volume is mounted read-only. Without a compatible seed, the first run
builds the base once. Subsequent runs retain it. Internal Buildroot paths remain
identical because its generated host tools are not generally relocatable.

The cache key covers the FES `image/` Buildroot tree (configuration, overlays,
patches and package recipes), container inputs, source/package locks, scripts,
Makefile, FES native input policy except the generated runtime commit, and the
parent incremental runner. Changes to these inputs select a separate fresh volume.
Application-source changes and runtime commit changes retain the base. The
container cache identity uses input contents rather than checkout locations, so
identical compiler containers are shared across FES worktrees. The package
selection and locked idle RBF are installed during finalization; a change to
that policy selects a new base. This intentionally conservative key can be
narrowed later with evidence.

Compiler and functional-artifact caches are shared across FES worktrees. Their
default root is the primary Git checkout's `out/cache`, not each task worktree's
`out/cache`. Set `FES_CACHE_ROOT=/absolute/path` to select another stable location.
Producers receive its `misteross-toolchains` directory through `--cache-root`;
immutable core packages use its `core-packages` directory. Mutable build outputs
remain in each task's disposable snapshots. Cache contents are not independent
provenance or acceptance evidence.

Do not copy authenticated compiler installations to another path and assume
that their qualification survives relocation. Authenticate/build a new slot
when changing the cache location. Inspect a failing cache slot and the recorded
path before any targeted repair; never remove all of `out/`, which may contain
other worktrees and uncommitted work.

`make dev` builds committed module sources. Preserve local edits and commit the
reviewed change before assembly; use module tests for earlier iteration. The
artifact-only diagnostic loop remains available without a whole-image rebuild.

## Package-only development acceptance

Start with the [core developer workflow](core-development.md) to prepare one
authenticated HIP package and freeze optional media without an image build.
Its acceptance adapter reuses the isolated runner below.

For a sealed core package using an existing ABI, use the separate
[package-only acceptance workflow](package-acceptance.md). It imports one
archive into the host library and uses the existing compatibility, selection
and session APIs; no image selection files or rootfs rebuild are required.
The operator command is `make package-acceptance PACKAGE_ACCEPTANCE_ARGS='...'`.
Running it against a kit requires explicit operator opt-in and exclusive use;
ordinary `make test` exercises only host-side fixtures.

## Three-system target acceptance

The package-only build checks image structure and package identity without
mutating a target. After deploying the exact development image to the
designated kit, run the FES-owned acceptance lane:

```sh
make target-acceptance
```

It checks host and direct-target health, verifies that the selected Pong,
ZX81 and Coleco package IDs are installed and selected in the host library,
launches each entry through the persistent session API, attaches input,
sends a small core-appropriate press/release sequence through the launch-owned
input bridge, and stops back to idle.
The runner always stops an active session during cleanup. It does not infer
video correctness from a successful API response.

Multiple library titles may select the same core and package. Select the exact
game ID when that match is ambiguous, repeating the option for other cores:

```sh
make target-acceptance TARGET_ACCEPTANCE_ARGS='--entry fes.coleco=GAME_ID --entry fes.zx81=OTHER_GAME_ID'
```

Each supplied game ID must match the expected core and exact package from the
selection records. Without an option for a core, exactly one matching entry
must exist; the runner never picks the first of several titles.

For the full physical evidence lane, provide the exact media fixtures and an
HDMI capture directory explicitly:

```sh
make target-acceptance TARGET_ACCEPTANCE_ARGS='\\
  --media fes.zx81=/absolute/path/fes-zx81-load.p \\
  --media fes.coleco=/absolute/path/fes-coleco-diagnostic.rom \\
  --capture-dir out/native-integration-dev/target-acceptance'
```

Each capture is written as a JPEG and `acceptance.json` records the exact
package, input count, media digest and capture digest. Capture digests prove
which bytes were recorded; visual interpretation remains a human HDMI review.
The lane is intentionally not part of ordinary CI because it requires the
designated physical kit and `/dev/video0`.

The `--media` option remains an explicit development upload after library launch;
it can replace library-selected media for that diagnostic session. Its receipt
records the uploaded file digest, not proof of immutable library-media selection.
This lane does not establish selected-media acceptance. For persistent library
selection, use the [data-driven media commands](core-packages.md#library-media).
Fresh Coleco entries receive no implicit controller diagnostic.

Use `make build` and `make verify` for stabilized integration and release checks.
A warm development image is diagnostic evidence and cannot satisfy those
commands' release receipts. `make rebuild` still forces the full cold build.
Native development does not build the separate QEMU test kernel. Unused
development volumes may consume several GB each; their exact names are
recorded in the development `inputs.json`.

After those cold receipts pass, `make media` can assemble
`out/native-integration-dev/media/current/fes.img`. On a local checkout it
automatically embeds a target agent configuration derived from the private host
config, so the generated card is ready to boot. It reads the verified cold
rootfs and does not rebuild it; `make verify-media` revalidates the current
immutable media generation, including the embedded rootfs and QEMU packaging
check. Both commands operate on files under `out/` and never write a block
device. See [bootable media](bootable-media.md) for the unprovisioned override,
rollback and the physical acceptance boundary.

## Hardware and handoff

The exact disposable kit and its operating instructions are in the selected
FogCast [development guide](../sources/FogCast/docs/DEVELOPMENT.md), alongside
its [working policy](../sources/FogCast/AGENTS.md). Use that designation;
an arbitrary reachable device is not authorized by a successful build.
One lease holder owns the kit for the duration of a test. The lease is sufficient
for ordinary diagnostics; coordinate disruptive deployment, service replacement
or reboot separately.

Report component work with this short handoff:

- Scope and changed behavior.
- Base commit and result commit, or the location of the uncommitted diff.
- Commands run and results, including failures or untested paths.
- Hardware status: host-only, diagnostic, or acceptance with exact artifact
  identities and dated evidence.
- Shared-contract, consumer or FES artifact-policy effects and the next integration step.

Passing host tests or reusing historical hardware evidence does not establish
hardware acceptance of `native-integration-dev`. Record acceptance only for
the artifacts actually exercised. Name the selected FES commit, module paths and
receipt hashes; an uncommitted worktree is not those artifacts. See
[artifact identities](artifacts.md). Whole-system image assembly is already
owned by FES `image/`; see [current refactor status](fes-structure.md).

## Contract generation and shared build caches

`make check-generated` verifies generated consumers and copied conformance data.
`make generate` regenerates these outputs in the FES working tree, including the
known copied external-core pins. Its legacy-layout guard refuses to rewrite
historical gitlink checkouts.
Canonical package definitions and runtime fixtures remain owner-maintained inputs.

Compiler caches and functional core artifacts use the primary Git checkout's
`out/cache/`, shared across its worktrees. `FES_CACHE_ROOT=/absolute/path` selects
another stable cache explicitly. Relocating an authenticated compiler installation
is not supported by merely copying its directory; build/authenticate a new slot.
Mutable source builds stay in individual snapshots. Cached packages retain their
original manifests and records; a separate `.provenance.json` selection receipt
identifies selected versus original source and the exact payload digest.

Functional record version 2 is opt-in per recipe until its representative build
and hardware gate pass. Version 1 retains exact-record selection. A changed commit
with the same verified functional inputs can reuse the original version-2 artifact;
unavailable historical evidence fails closed. Core-local source closure remains
conservative, so edits within an owning core directory can invalidate that core.

## Diagnose local edits without committing them

Run `make dev-snapshot` from the feature worktree to freeze current tracked and
untracked, nonignored files into a separate checkout under
`out/development-snapshots/`. Staged and unstaged changes are combined using the
current working file contents; deletions and executable modes are preserved.
The command leaves your index, branches and working files unchanged.

The printed JSON gives the snapshot directory and actual Git commit. Run
`make host` or `make dev` in that directory for diagnostic integration builds;
`make doctor` is also available. The snapshot retains the original repository
origin and parent commit, and commits a development-only marker. It is not a
reviewed source selection: cold builds, verify, image, media and release checks
reject it, including when a profile selects its module revision. Removing the
working marker does not change that classification. Commit reviewed edits in
the feature worktree before producing release or hardware-acceptance evidence.
Snapshots do not update the feature branch or qualify hardware automatically.

Host builds publish `out/<profile>/host-inputs.json` on both successful builds
and cache reuse. Its canonical JSON digest equals `host.json.inputs`; snapshot
builds include explicit `development-only` provenance there. Cold host readers
validate this sidecar when present and reject diagnostic output. Historical
ordinary receipts without the sidecar remain readable.
