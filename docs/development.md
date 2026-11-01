# Development through FES

FES contains the host, target runtime, shared contracts and FPGA product sources
in one Git repository. Their ownership boundaries remain separate; their files
under `sources/` are tracked modules in the same FES commit. Start with
[AGENTS.md](../AGENTS.md), [the project map](project-map.md) and the owning module's
instructions. Read [getting started](getting-started.md) for machine setup and
[the agent workflow](agent-workflow.md) for assignments.

## Inspect source freshness

Use `make status` for the combined source, build and validation summary;
`STATUS_ARGS='--offline --json'` produces a full local evidence report.
Supply CI, hardware and deployment receipts as described in [status](status.md).
Missing evidence stays unknown; a recent source commit is not hardware acceptance.

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

## Shared GPU leases

Run shared GPU work through `scripts/gpu-run`; inspect holders and waiters with
`scripts/gpu-status`. `GPU_LEASE_IDS` overrides host inventory detection and is
not a per-job pin. Each waiting ticket records its eligible physical ids and
vendor mask variable. Ticket order is preserved for overlapping GPU sets, while
a later ticket can backfill on a disjoint free GPU. On grant, `gpu-run` replaces
inherited visibility masks with the exact leased ids in `FES_GPU_IDS` and the
AMD `ROCR_VISIBLE_DEVICES` or NVIDIA `CUDA_VISIBLE_DEVICES` mask. Status shows
the waiter's id set and owner PID; tickets with a dead or reused local PID are
ignored after checking the recorded process start time.

On an Apple silicon Mac the vendor is `apple` and the inventory is its one
Metal GPU, id 0, found through ioreg. Metal has no visibility mask: a grant sets
`FES_GPU_IDS` and `GPU_RUN_DEVICE` to the physical id, which nextpnr's Metal
backend uses as its device index. With no device nodes to check, a GPU route
must show a Metal user client that nextpnr opened on the leased GPU, or log
`backend metal:<registry id>:... ready` for it; CPU fallback output fails it.
Process start times and ancestry come from libproc and the lease directory's
filesystem from `statfs(2)`. Install the scripts as the
[Shared GPUs](../AGENTS.md#shared-gpus) section describes, including its Mac
`PATH` step.

`gpu-run --phase-lease` holds no GPU up front. After it takes one of the
owner's job slots (`GPU_LEASE_MAX_JOBS_PER_OWNER`, default 4), the command
asks for a GPU on `GPU_LEASE_FD` at each GPU phase and releases it afterwards;
`gpu-run --help` gives the protocol. The owner cap then counts active GPU
phases. Phase requests from owner `lander` wait in a separate queue that every
runnable main-queue ticket goes ahead of whenever a GPU frees. `gpu-status`
also lists phase-lease jobs running in or waiting for a job slot. Producers use
this mode automatically when their nextpnr binary supports `GPU_LEASE_FD`.

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
change, then run `make check` and the appropriate build. Parent
builds consume real committed FES snapshots under ignored `out/work/`; they do
not build arbitrary unstaged module edits. A snapshot retains the actual FES
repository and commit, with the module's path and tree identity. It is not a
synthetic standalone component commit. Do not edit builder-managed snapshots.

FogCast's local Go replacement `../misteross/expansion` resolves inside that
same full snapshot. Both host and ARM target builds therefore consume the
expansion linker committed with the selected FogCast revision; uncommitted
sibling edits do not enter a parent build. Image containers mount the complete
snapshot Git root, preserving this layout. A separately selected misteross FPGA
revision does not replace the host's Go dependency. No compiler or FPGA build
is needed to build or test this software linker.

The import mapping in [config/source-imports.toml](../config/source-imports.toml)
records original repositories, prior gitlinks, imported commits and module trees.
The import commit retains original histories as parents. That local history
mapping does not assert that cutover has been published or hardware-qualified.

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
| `make build` | Build selected host and cold one-pass native image outputs; `IMAGE_PASSES=2` adds the reproducibility pass |
| `make verify` | Verify published outputs against receipts and child checks |
| `make media` | Assemble a flashable disk image from verified cold outputs |
| `make verify-media` | Verify `media/current/fes.img` and its host-side evidence without deployment |

The default FES integration profile is `native-integration-dev`. It selects only
`fes.menu` display firmware and the locked idle RBF; playable core builds do not
run as part of this image. The clean, development, verification and media paths
reuse the same closed package set. Install `fes.pong`, `fes.zx81`, `fes.coleco`
and other systems independently through the [core catalog](core-packages.md).
Individual packages currently use HIP/nextpnr; Quartus is an explicit oracle
where specified. The accepted policy also permits labelled Quartus variants,
but the current recipe and package resolver do not select them yet; see
[Quartus-built cores](design/quartus-cores.md). Host builds and [kit software
updates](kit-software-releases.md) do not build FPGA packages. Build and verify
do not deploy.

FES selects placement seed 5 for both Atari ST video parts. The selected seed
is part of their canonical cache identity and archive verification; it does not
change the sealed shell, clocks or socket containment requirements. Standalone
misteross builds can still select other bounded seeds explicitly.
Systems whose nextpnr route is not implemented yet use an explicit Quartus
oracle/check documented by that system's recipe. A future labelled Quartus
variant follows the accepted
[Quartus-built core design](design/quartus-cores.md) once recipe and package
selection support lands. Quartus is never an automatic fallback and never
creates a legacy image bundle.

During component development, use the narrow relevant component tests and
build only changed artifacts. For target diagnostics, follow the selected
FogCast development guide's fast loop using a disposable copy of a verified
image. Label those results diagnostic. Reserve expensive full image and
two-pass reproducibility checks for stabilized integration; full rebuilds
are also required when image configuration, packaging or locked inputs
change. One parent build runs per checkout, enforced by its existing lock.
Coordinate shared expensive runs rather than starting one per agent.

Image builds use one pass by default, including CI. `make image
IMAGE_ARTIFACTS=/absolute/artifacts` assembles explicit prebuilt inputs without
rebuilding their sources. `make verify` still checks the image hashes, structure,
and QEMU packaging. Receipts and release evidence record `image_passes: 1` and
`two_pass_reproducibility: not-run-single-pass`; they do not claim reproducibility.
Single-pass images can be exported, bootstrapped and provisioned normally, without
a special version name or scratch marker.

For an explicit reproducibility check, use `IMAGE_PASSES=2 make image` (with the
same artifact selection), then `IMAGE_PASSES=2 make verify` and
`IMAGE_PASSES=2 make release RELEASE_VERSION=<version>`. Both independent rootfs
builds must match. Cached outputs and export requests must match the selected
pass count. `make media` follows the verified image's pass count. A two-pass
release of the same image and version gets its own default release directory
beside the one-pass release. Image integrity, target compatibility and boot
acceptance are the same for both modes. Bootstrap and card assembly retain
their separate, inexpensive deterministic packaging comparisons; these do not
rebuild the OS.

The update manifest keeps its existing field set; build-pass evidence lives in
`evidence.json`, bound to the verified image receipts. Source revisions identify
provenance, not a requirement for matching host software.

An optional core, currently Atari ST, can miss timing without failing
`make build` or `make dev`. The build prints a `WARNING:` block
naming the core, the reason (`timing-not-met`, `build-failed`, or `skipped`),
and any seeds and clock figures, then continues. `FES_SKIP_OPTIONAL_CORES=1`
or `FES_SKIP_CORES=fes.atari-st` skips that producer before place-and-route,
which is the fast path for an image of a pull request that does not change ST.
`FES_REQUIRE_ALL_CORES=1` makes any optional miss fatal. Set it for a release
or factory image that must contain every selected core, and on `make verify`
when that image must be rejected if an optional core was left out. Verify
ignores skip flags and does not rewrite `unavailable-cores.json`. It reuses
the omission recorded in `inputs.json` and does not place-and-route that core
again. The two skip forms conflict with `FES_REQUIRE_ALL_CORES=1`.

CI simulations use Verilator 5.032 at source commit
`8ff77e9d47351b0a59114929880687839a51840b`, selected by
[`scripts/ci_verilator.sh`](../scripts/ci_verilator.sh). This simulation baseline
is independent of the synthesis toolchain locks; neither Ubuntu's older default
Verilator nor a development snapshot is substituted automatically. CI caches the
installation by host architecture and bootstrap-script digest. One preparation
job restores or builds it and shares the installation with the selected
simulation jobs, so a cold cache does not compile Verilator separately for every
core. Coleco's full simulation tier runs 24 jobs: seven unit/board scenarios in
both default and OSS lanes, the CPU expansion probe, registered diagnostic
through the socket, video-parts check and seven SGM cases. Its smoke tier runs
5 jobs: unit, unit-OSS, board-graphics, diagnostic and integrated SGM. The
[affected-check guide](test-changed.md#selection-and-coverage)
describes CI job selection and the complete set of Coleco simulation cases. To reproduce it
locally, install Git, a C++ compiler, Make, Autoconf, Bison, Flex, help2man and the
Flex/zlib development libraries, then run:

```sh
bash scripts/ci_verilator.sh "$PWD/out/tools/ci-verilator" "$PWD/out/work/ci-verilator"
make -C sources/misteross VERILATOR="$PWD/out/tools/ci-verilator/bin/verilator" sim-fes-demo
```

## Incremental native image

Run `make dev` for the selected `native-integration-dev` revisions. It publishes
`out/native-integration-dev/development/linux.img` and a `development.json`
receipt after structural validation. It uses the same selected package set,
FES `image/` overlay and image recipes as the clean build. The native image
contains the locked idle RBF and the same closed package set from
[the default profile](../profiles/native-integration-dev.toml). It does not deploy, run QEMU,
produce two-pass evidence, or replace the clean image and receipts.

The development Buildroot volume retains the compiler, libraries and package
outputs. The Go agent uses Go's compilation cache; the runtime package is cleaned
and rebuilt when its selected commit changes. An unchanged complete output is
reused after receipt/hash checks. Otherwise, full Buildroot finalization runs to
install the current agent, locked idle RBF, selected package set and build-input record. A package
selection change retains the compiler/base volume but forces final image
assembly. The development receipt binds the emitted package selection and the
exact external `manifest.toml` and `core.rbf` bytes, plus `rom-map.json` when
the selected package is format 3 or 4.
A failed build leaves no development success receipt; the next invocation can
resume package compilation.

A compatible existing clean build can seed a new development volume. FES checks
its source/profile/Go identities, receipt hashes and recorded pass hashes. It then
copies only the clean tree for the selected pass (`work-1-native-dev` by default,
`work-2-native-dev` with `IMAGE_PASSES=2`), after checking that tree's rootfs
hash, with symlinks and modes preserved. The clean volume is mounted read-only.
Without a compatible seed, including a clean build that lacks the selected
pass's tree, the first run builds the base once. Subsequent runs retain it.
Internal Buildroot paths remain identical because its generated host tools are
not generally relocatable.

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

## Target acceptance

The package-only build checks image structure and package identity without
mutating a target. After booting the development image on the designated kit,
run the FES-owned acceptance lane:

```sh
make target-acceptance
```

The runner reads the selected package identities from the development image's
`inputs.json`. For the default MENU-only image, it checks that the recorded
`fes.menu` identity matches its published selection file, host and direct-target
health are ready with matching host/agent revisions, and the host session is
idle. It reports bootstrap readiness; this does not claim a playable-core
launch or visual MENU check.

For an image that selects supported playable packages, it checks every selected
package's exact identity, verifies that it is installed and selected by exactly
one matching host-library entry, launches each entry through the persistent
session API, sends its core-specific input sequence, and stops back to idle.
The full expanded package set retains all seven current Pong, ZX81, Coleco,
SMS, SG-1000, C64 and Spectrum lanes. Missing or unexpected selection files
and selections that disagree with `inputs.json` are errors. `inputs.json` is
required; older selection directories without it are rejected. The runner
always stops an active session during cleanup. It does not infer video
correctness from a successful API response.

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

Capture defaults to raw YUYV input (`--capture-input-format yuyv422`).
Use `--capture-input-format mjpeg` only for a device whose compressed input
has been checked. Capture waits three seconds before opening the device and
keeps frame 120 to allow HDMI and capture buffers to settle. Override with
`--capture-settle-seconds` and `--capture-frames` for another device. The input
format, settling delay and frame count are recorded with each capture.
Each capture is written as a JPEG and `acceptance.json` records the exact
package, input count, media digest and capture digest. Capture digests prove
which bytes were recorded; visual interpretation remains a human HDMI review.
The lane is intentionally not part of ordinary CI because it requires the
designated physical kit and the ShadowCast node
`/dev/v4l/by-id/usb-GENKI_ShadowCast_3_KT044001-video-index0`.
That is the default `--video-device`. Powerboat also has a second capture
card, so do not substitute `/dev/videoN`; the stable paths are in the FogCast
[dedicated fixture](../sources/FogCast/docs/DEVELOPMENT.md#dedicated-fixture).

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
owned by FES `image/`; see [image assembly](image-assembly.md).

## Contract generation and shared build caches

Cold native images cache the pinned Buildroot SDK under
`image/build/cache/target-image/toolchains/`. Each image pass uses a fresh
extraction of the same checked archive. Set `TOOLCHAIN_REBUILD=1` on `make build`
to force a new SDK and bypass image receipt reuse; see
[image assembly](image-assembly.md#buildroot-cross-toolchain).

For cross-worktree image reuse, set
`FES_TARGET_IMAGE_SHARED_CACHE=/absolute/host/path` on `make build`, `make image`,
`make dev`, `make kit-sdk` or `make kit-software`. The external directory holds
`dl/`, `toolchains/<key>/` and `ccache/`; it does not change the shared compiler
and package cache below. `make kit-sdk` fills `toolchains/<key>/` for the current
image key without building an image.
The toolchain-only build never uses ccache. With `IMAGE_PASSES=2`, cold pass 2
disables it so the two rootfs hashes still compare independent compiles. Stop
builds before removing one of these cache directories; see
[image assembly](image-assembly.md#buildroot-cross-toolchain).

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

Registered OSS recipes select functional record version 2. Version 1 retains
exact-record selection. A changed commit
with the same verified functional inputs can reuse the original version-2 artifact;
unavailable historical evidence fails closed. The versioned Markdown policy
excludes non-executable documentation while retaining code and data throughout
the owning modules. Functional builds require strace for compiler read checks;
ordinary Python source reads are also guarded. Older records keep their original
closure rules. Each new artifact still needs its own qualification.

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
For `make dev`, set `FES_CACHE_ROOT` to the primary FES checkout's absolute
`out/cache` if the snapshot's own `out/cache` has no authenticated toolchain.

Host builds publish `out/<profile>/host-inputs.json` on both successful builds
and cache reuse. Its canonical JSON digest equals `host.json.inputs`; snapshot
builds include explicit `development-only` provenance there. Cold host readers
validate this sidecar when present and reject diagnostic output. Historical
ordinary receipts without the sidecar remain readable.

For a missing authenticated compiler slot, use the recipe-specific command
printed by FES, then rerun the original build. It includes the selected shared
cache, lock and HIP settings. See [compiler provisioning](core-packages.md)
for clean-module provisioning commands. A plain `make toolchain` without those
settings can populate a different slot. Manual provisioning inherits shell
compiler overrides that FES normally removes for producer execution.

## Compatible kit software updates

Use `make kit-sdk` when the toolchain cache has no SDK for the current image
key. It builds and publishes only that Buildroot SDK. Then use
`make kit-software KIT_SOFTWARE_BASE=/absolute/linux.img
KIT_SOFTWARE_BASE_SHA256=<reviewed-image-sha256> KIT_SOFTWARE_VERSION=<version>`
to build the four kit executables and selected assets from committed HEAD using
the retained base's pinned SDK. This publishes an immutable bundle plus receipt;
it does not rebuild the OS or cores, update host software, or contact the kit.
The existing image and previous output directories stay intact. See
[kit software releases](kit-software-releases.md#build-an-update-without-rebuilding-the-os)
for cache prerequisites, receipt details, installation and activation.
