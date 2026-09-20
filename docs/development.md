# Development through FES

New to the checkout? Begin with [getting started](getting-started.md). For
assignments and team handoffs, see [the agent workflow](agent-workflow.md).

Read [the parent instructions](../AGENTS.md) and
[component boundaries](component-boundaries.md) first. Choose the component
that owns the behavior, then read its instructions, README and current
architecture before editing. FES coordinates compatible revisions; each
component keeps its own implementation and focused development loop.

## Inspect source freshness

Run `make source-status` before selecting updates. It observes remote `main` for
FES and each component without fetching, changing refs, selecting pins or writing
Git indexes. Component `selected` is the index gitlink used by integration; a
staged selection is shown separately from the committed gitlink. Checkout HEAD,
branch, dirty paths and selection mismatches remain visible.

```sh
make source-status
make source-status SOURCE_STATUS_ARGS='--offline'
make source-status SOURCE_STATUS_ARGS='--json --timeout 5'
```

Online observations include UTC timestamps. `equal` means the selected SHA matches
the observed remote main. `behind`, `ahead` and `diverged` use available local
history; `different-history-unavailable` means the SHAs differ but the command
cannot safely determine ancestry without fetching. Offline, failed and timed-out
queries remain unknown. No cached remote-tracking ref is presented as a fresh
remote observation. Uninitialized components are reported and their configured
`.gitmodules` URL can still be queried; initialized checkouts use their `origin`.
All project repositories currently use `main`; this command deliberately compares
that integration branch, not the developer branch's configured upstream.

The JSON schema includes full SHAs and lossless Git porcelain change records.
The report exits zero when inspection succeeds, even for dirty/stale/unknown
sources; it is informational, not an integration gate. Invalid invocation or
local inspection failure exits 2. It does not establish compatibility, CI success,
build completion, hardware qualification or deployed state. Continue to use
`make check` for selected-source consistency.

## Isolate component work

From the FES root, create a worktree from the selected clean component HEAD.
For example, for a FogCast task named `launch-status`:

```sh
mkdir -p out/dev/launch-status
git -C sources/FogCast worktree add -b feat/launch-status \
  "$PWD/out/dev/launch-status/FogCast" HEAD
```

Use the corresponding directory under `sources/` for another component.
`out/` is ignored. Use a unique task/branch name for each concurrent task in
the same component repository. Leave the root `sources/` checkouts clean and
at their selected pins so other integration work can build reproducibly.
Run component commands inside the worktree; parent builds use selected
revisions, not an uncommitted component worktree.

When parallel work helps, assign disjoint scopes and share contract changes
early. The integrator selects reviewed component results, updates parent
gitlinks and affected profile/contract inputs, and runs integration checks.
Workers do not move root component checkouts or update gitlinks independently.
Committing and publishing still require user authorization.

For a reviewed component commit already available in its repository, the
integrator selects it explicitly, for example:

```sh
git -C sources/FogCast checkout --detach REVIEWED_FOGCAST_COMMIT
git -C sources/libmister-runtime checkout --detach MATCHING_RUNTIME_COMMIT
git add sources/FogCast sources/libmister-runtime
make check
make host
```

Replace the uppercase placeholders with full reviewed commit IDs. If a worker
used another clone, fetch its branch into the component repository first. FES
selects the runtime independently and generates the concrete assembly lock. Stage package or FPGA
gitlinks in the same way when they change. These staged gitlinks are what the
parent validates, so the candidate can be built before a parent commit or PR.
Before publishing the parent, ensure each selected commit is available from the
component remote; a local-only commit will break recursive clones elsewhere.

## Validate at the right scope

| Command from FES root | Purpose |
| --- | --- |
| `make test` | Parent regression tests |
| `make check` | Component pins, locks and generated-definition consistency |
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
receipt after structural validation. It uses the same pinned package set,
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
Makefile, FogCast native input policy except the runtime commit, and the parent
incremental runner. Changes to these inputs select a separate fresh volume.
Application-source changes and runtime commit changes retain the base. The
container cache identity uses input contents rather than checkout locations, so
identical compiler containers are shared across component worktrees. The package
selection and locked idle RBF are installed during finalization; a change to
that policy selects a new base. This intentionally conservative key can be
narrowed later with evidence.

The default parent integration path also opts every selected FES
package producer into the disposable shared compiler cache at
`out/cache/misteross-toolchains` through an explicit producer `--cache-root`.
That cache is workspace-local ignored state, not provenance. Published slots
seal `install/` and `evidence/` as 0555
directories with 0444 files and also keep writable `src/` and `build/`
trees, so a plain recursive removal cannot delete them. Restore owner write
and search permission, then remove that exact tree:

```sh
chmod -R u+rwX -- out/cache/misteross-toolchains
rm -rf -- out/cache/misteross-toolchains
```

Then retry. The shared-cache implementation and real OFF/HIP evidence live in
misteross PR #60; this parent slice does not claim a fresh cold build or
hardware validation.

`make dev` builds selected clean revisions, not arbitrary uncommitted worker
checkouts. Integrate reviewed component commits using the commands above before
running the parent build. Workers can still use their component's artifact-only
diagnostic loop.

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
- Parent pin or shared-contract effects and the next integration step.

Passing host tests or reusing historical hardware evidence does not establish
hardware acceptance of `native-integration-dev`. Record acceptance only for
the artifacts actually exercised. Name the selected component commits and
receipt hashes; an uncommitted worktree is not those artifacts. See
[artifact identities](artifacts.md). Whole-system image assembly is already
owned by FES `image/`; see [current refactor status](fes-structure.md).

## Contract generation and shared build caches

`make check-generated` verifies generated consumers and copied conformance data.
After repository import, `make generate` regenerates these outputs in one working
tree, including the known copied external-core pins. It refuses writes while
components remain gitlinks, preserving the old pinned integration inputs.
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
