# FES - Fogger Expandable System

Fogger Expandable System is a OSS FPGA based on 'mister' (DE-10 Nano) that provides a different view on how a FPGA multiple system
should be. Compared to mister it currently supports only a few system but builds them with a fully OSS toolchain by default (Yosys + nextpnr) and provide a
closer analog to real expansion and cartridge buses that many system had.

The default appliance and playable packages target DE10-Nano. DE25-Nano is a
standard Quartus Pro 26.3 development kit (Kit D25 `de25-j8g`, serial
`TRWU8J8G`; see [DE25 kit HIL](docs/de25-kit-hil.md) and
[DE25 Linux platform](docs/design/de25-linux-platform.md)) with eight registered
Quartus Pro recipes (`fes.menu`, `fes.zx81`, `fes.spectrum`, `fes.atari-st`,
`fes.c64`, `fes.pcxt-dos`, `fes.ramtest`, `fes.pc386`), a [DE25-Nano Linux guide](docs/de25-linux.md),
[board shells and diagnostics](sources/misteross/boards/de25nano/README.md),
and [image export commands](docs/image-artifacts.md#de25-image-and-export-commands).
Partial-persona loading remains unsupported and default-off pending
[#1568](https://github.com/DeanoC/fes/issues/1568), and full normal-kit
acceptance remains open. The separate
[Linux-free TamaGo experiment](platform/embedded/README.md) remains an
explicitly opted-in path through `FES_EXPERIMENTAL_DE25_EMBEDDED=1`, with
dedicated CI host/QEMU regressions and ongoing hardware qualification under
[#1867](https://github.com/DeanoC/fes/issues/1867); Linux build and image
defaults are unchanged.

Use `make de25-embedded-image` and `make verify-de25-embedded-image` for
the native Tenfoot SD card. The [launcher guide](platform/embedded/nano/launcher.md#3-build-and-verify-the-sd-image)
lists the matched boot, SDK, recovery and staging inputs.

Agents: read [AGENTS.md](AGENTS.md), then the
[development guide](docs/development.md) and the selected component's guidance.
[Component boundaries](docs/component-boundaries.md) describe ownership.

## Documentation

Start at the [documentation index](docs/README.md) and the single roadmap in
the [forward plan](docs/plans/forward-plan.md). Core ABI, interfaces and
standing are the [core status](docs/core-status.md) matrix. Boards, toolchains
and support tiers are the [core support matrix](docs/support-matrix.md).

- [DeepWiki](https://deepwiki.com/DeanoC/fes): Devin-generated guide to the FES codebase.
- [Getting started](docs/getting-started.md): setup, build choices, running the host and common failures.
- [Bootable media](docs/bootable-media.md): build, provision and verify a flashable native image.
- [Development](docs/development.md): worktrees, incremental builds and handoffs.

## Components

| Component | Responsibility |
| --- | --- |
| FES | Compatible revisions, integration checks, final system assembly and evidence |
| [FogCast](sources/FogCast) | Browser/tenfoot UI, game library, host services and network-facing target agent |
| [libmister-runtime](sources/libmister-runtime) | Local daemon/library controlling FPGA, media, input and hardware lifecycle |
| [misteross](sources/misteross) | FPGA development, compilation and exported FES package artifacts |
| [mister-packages](sources/mister-packages) | Shared hardware/system definitions and generated C++/Go |

Main_MiSTer is an original implementation and test reference, not a production
dependency. The target agent talks to the runtime's local socket; only the
runtime controls hardware. The normal FES package route uses misteross's
authenticated HIP/nextpnr producer. FES policy also allows labelled
Quartus-built core variants (accepted in #1163), and Quartus is used wherever
nextpnr timing closure is hard. On DE10-Nano, playable recipes use OSS packages
except the ZX81 family (ZX81, ZX80, TS1000, TS1500), which selects its Quartus
Lite producer with nextpnr as an advisory lane, and PC/XT (`fes.pcxt-dos`) and
`fes.pc386`, which select their Quartus Lite producers (Yosys front end). On
DE25-Nano, registered recipes select Quartus Pro 26.3.
The general variant selector and OSS-only image option are rollout
work described in the design. See
[Quartus-built cores](docs/design/quartus-cores.md).

## Start

Use Linux amd64 with Git, GNU Make, Python 3.11+, Go and Docker. Go selects the
version in the selected FogCast `go.mod` (currently 1.26.5). The affected-test
command also needs Node.js 18+ and ripgrep for the host UI lane. First-party sources
are included here; external build dependencies retain their own locks.

```sh
git clone git@github.com:DeanoC/fes.git
cd fes
make check
make host
```

`make check` verifies clean selected modules, package YAML, every generated
consumer and every shared fixture copy, and prints their counts. It needs Go,
not Docker or Quartus. `make host` builds the Linux CLI and browser API server. Run `make doctor`
when preparing for container/image builds. See [getting started](docs/getting-started.md)
for Git authentication and a minimal host configuration.
Use [focused tests](docs/test-changed.md) during development. The full `make test`
suite provisions its pinned platform/media containers and needs Docker plus
network access on first use; host compilation does not need them.

## Build the integrated native system

For routine native development, run `make dev`. It keeps the compiler and base
packages and publishes a structurally checked diagnostic image under
`out/native-integration-dev/development/`. A compatible existing clean build can
seed that cache; otherwise the first run builds the base once. See
[the incremental workflow](docs/development.md#incremental-native-image).

DE25 uses the same source workflow with an explicit managed AArch64 SDK/base
and matching owner kernel/compiler: `make dev BOARD=de25-nano
DE25_IMAGE=/absolute/managed-base IMAGE_KERNEL=/absolute/kernel
IMAGE_SDK=/absolute/owner-sdk.tar.gz`. MENU must first be admitted through its
Quartus recipe. The source path builds the selected Go/C++ components, reuses
the existing factory compiler/base cache and publishes a verified immutable
generation under `out/de25-nano/development/`. Missing MENU is refused before
target software or OS compilation. See the
[DE25 source-build requirements](docs/development.md#de25-source-development).

For compatible kit software updates, use `make kit-software` with a retained
verified base image; see [kit software releases](docs/kit-software-releases.md).
DE25 selects the compiler and software target with `BOARD=de25-nano
DE25_IMAGE=/absolute/verified-image`; `make kit-sdk` authenticates and caches
that image's AArch64 SDK without rebuilding Linux. A software update still
requires a managed factory image with its software manifest and MENU contract.

For clean integration and release verification:

```sh
make build
make verify
```

The default `native-integration-dev` builds a bootstrap image with the locked
idle RBF and a closed package set containing only `fes.menu`, the display firmware
needed by the kit UI. It does not build playable cores. Install `fes.pong`,
`fes.zx81`, `fes.coleco` and other systems through the core catalog when wanted;
see [modular updates](docs/modular-updates.md) and [core packages](docs/core-packages.md).
Software bundles update the agent, runtime and UI without rebuilding this OS.
The image has no legacy bundle lane and still verifies the packages it actually selects. Core timing, video
parts and firmware remain requirements of the individual core installation.

The Coleco factory recipe selects its native 256×192 indexed-pixel shell and
builds matching Direct and Scanlines parts, which own frame capture and 720p
output. A separate sealed `core-video-parts/`
tree retains the archives and exact-shell inventory; `fes-core-video-parts.json`
binds that inventory in development and cold image receipts. The normal core
catalog Install operation admits the published companions into the host library,
so the household video preference applies to the next Play. Native shells
require a matching video part; older raster shells retain their built-in
Direct fallback.

The normal package-only build uses the authenticated HIP/nextpnr producers, and
the Quartus Lite producers for the ZX81 family, PC/XT (`fes.pcxt-dos`) and
`fes.pc386` (with Quartus Pro 26.3 for DE25 recipes), with the HIP/nextpnr
shared compiler cache beneath the primary FES checkout’s `out/cache`
(or the absolute `FES_CACHE_ROOT` override), reused across FES worktrees. A
matching package is reused only after its locked inputs, manifest, payload and
sealed selection are checked; a miss runs that package's registered producer.
Each registered OSS core selects its per-core lock (`cores/fes-menu/toolchain.lock`
for MENU, `cores/fes-ramtest/toolchain.lock` for RAM Tester's 100 MHz OSS build,
and so on; locks share Yosys and Mistral pins except documented exclusions while
each pins its own last-known-good nextpnr commit).
Quartus Lite 17.0.2 (`QUARTUS_ROOTDIR` naming the install that the core's
`quartus.lock` pins) builds the registered DE10 ZX81-family, PC/XT and pc386
packages; the ZX81 family also reads the pinned Mistral ROM database from the
shared cache or `make toolchain-fes-zx81`. Elsewhere it is an explicit
bring-up/oracle check where a recipe documents one. The
[Quartus-built cores](docs/design/quartus-cores.md) policy allows a labelled
Quartus variant of a core, built on a pinned host authorized for its edition
and device, with a license when required, under the shared
[Quartus lease](docs/design/quartus-hosts.md). The integrated path selects
exactly one producer per core and board, and does not yet select between OSS
and Quartus variants of one core; a failed HIP route never falls back to
Quartus. Downloads are checked against component locks; image compilation runs
with networking disabled (one pass by default, or two independent build roots
with `IMAGE_PASSES=2`).

```text
out/native-integration-dev/
  fogcast-api                 Linux amd64 server with browser UI
  fogcast                     Linux amd64 CLI
  linux.img                   ARMv7 target root filesystem
  idle.rbf                     locked MiSTer idle RBF
  fes-*.package-selection.toml one selection record per package
  core-packages/<package-id>/  manifest, RBF and optional sealed ROM map
  inputs.json                 selected sources, profile, Go and parent recipe
  host.json / image.json      input fingerprints, OS/arch (host), and output hashes
  reproducibility.txt         independent image hashes
  manifest.tsv                installed files
  library-report.tsv          target library closure
  verification.json           explicit verification results
  qemu-smoke.log              packaging smoke evidence
  media/current/fes.img       flashable raw DE10-Nano disk image
  media/current/fes-media.toml closed external media manifest
  media/current/media.json    media receipt and host-check statuses
```

Run the host with an explicit local configuration:

```sh
out/native-integration-dev/fogcast-api --config /absolute/path/config.toml --listen 127.0.0.1:8787
```

The native image starts the UI with no playable cores installed. Core installation
uses the package/library lifecycle. `linux.img` is the target root filesystem;
run `make media` after a verified build to publish flashable media. Build and
verification do not deploy or contact a kit. QEMU checks packaging; physical
boot and play checks remain separate. See [bootable media](docs/bootable-media.md).

## Commands and profiles

| Command | Result |
| --- | --- |
| `make test` | Parent, platform and image tests; requires their container prerequisites |
| `make test-changed TEST_CHANGED_ARGS='--base origin/main'` | Affected software checks and dependent consumers |
| `make dev-snapshot` | Freeze local edits in a separate diagnostic-only checkout |
| `make source-status` | Read-only selected revisions, local changes and observed remote main freshness |
| `make status` | Separate source, CI, build, hardware and deployment evidence ([guide](docs/status.md)) |
| `make check` | Current component and shared-definition consistency |
| `make doctor` | Selected pins/lock, Linux architecture, Go and container availability |
| `make host` | Linux API server and CLI |
| `make dev` | Incremental diagnostic native image with retained compiler/base |
| `make package-acceptance PACKAGE_ACCEPTANCE_ARGS='...'` | Explicit opt-in single-package lifecycle diagnostic; no image rebuild ([guide](docs/package-acceptance.md)) |
| `make package-acceptance-isolated PACKAGE_ACCEPTANCE_ISOLATED_ARGS='...'` | Private-host package lifecycle and restart diagnostic; live library is not mounted ([guide](docs/package-acceptance.md)) |
| `python3 scripts/kit_software_acceptance.py --help` | Explicit leased software install/queue/cancel/activation diagnostic; successful selections remain installed ([guide](docs/kit-software-acceptance.md)) |
| `make build` | Host and one-pass native image, reusing matching checked outputs; `IMAGE_PASSES=2` opts into reproducibility |
| `make image IMAGE_ARTIFACTS=/absolute/artifacts` | Assemble OS from prebuilt bootstrap inputs ([guide](docs/image-artifacts.md)) |
| `make verify` | Require host/image receipts, verify image hashes and QEMU packaging; compare both hashes when `IMAGE_PASSES=2` |
| `make media` | Publish a verified flashable disk image, auto-embedding the local target agent config |
| `make verify-media` | Reverify the published disk image and embedded root filesystem; no device writes |
| `make rebuild` | Force host and the selected package-only image passes |

| Profile | Selected source combination |
| --- | --- |
| `native-integration-dev` (default) | Current FES commit, locked idle RBF and MENU display firmware; install playable cores separately |
| `de25-nano` (explicit `BOARD=de25-nano`) | Committed AArch64 sources, authenticated managed SDK/base and matching owner kernel/compiler; requires an admitted Quartus Pro MENU package ([commands](docs/development.md#de25-source-development)) |

The parent defaults to the DE10 integration profile. Systems whose nextpnr route is
not implemented yet are checked explicitly with Quartus when their recipe
requires it; that check does not create an image package or change the default
package-only path. The accepted policy also allows labelled Quartus variants:
on DE10, the ZX81 family, PC/XT (`fes.pcxt-dos`) and `fes.pc386` select their
registered Quartus Lite producers, and on DE25, registered recipes select
Quartus Pro 26.3; see [Quartus-built cores](docs/design/quartus-cores.md).
Quartus is never an automatic fallback.

## Development and integration

Edit modules under `sources/` in an isolated FES worktree. Committed integration
builds use disposable full-repository snapshots under `out/work/`, retaining the
actual FES commit and module path. Builds in one
checkout are serialized and each profile uses a distinct Docker output volume.
Ambient Go and image-selection overrides are normalized.

The integrator reviews one feature diff, runs `make check`, and
records the tested commit. See [development and handoffs](docs/development.md)
for exact commands and how several agents can work independently.

CI computes affected modules and dependent consumers, then runs independent
software checks, core simulation cases and the TamaGo bare-metal/QEMU lane
(`platform/embedded/`) in parallel with an always-reported integration result.
PRs, main-branch pushes and merge groups run the smoke simulation tier; nightly
and manual runs cover the complete suite in the full tier. Feature-branch
pushes do not duplicate PR runs. Routine runs also skip the full-device video
and real-media suites and use concurrency-focused race checks; those extended
modes return when their inputs change or on scheduled/manual runs. See
[selection and coverage](docs/test-changed.md#selection-and-coverage).
The normal repository token
is sufficient; first-party checkout needs no sibling-repository secret.
Quartus, full image builds and physical checks run on the development machine.

The normal profile installs the locked idle RBF and MENU display firmware.
Older multi-system validation records remain historical evidence and do not
change the package-only default.
