# FES - Fogger Expandable System

Fogger Expandable System is a OSS FPGA based on 'mister' (DE-10 Nano) that provides a different view on how a FPGA multiple system
should be. Compared to mister it currently supports only a few system but builds them with a fully OSS toolchain by default (Yosys + nextpnr) and provide a
closer analog to real expansion and cartridge buses that many system had.

DE25-Nano bring-up includes a [headless AArch64 image](docs/de25-linux.md),
[Quartus Pro clock/LED and HPS mailbox diagnostics](sources/misteross/boards/de25nano/README.md),
and [off-board runtime software checks](docs/de25-linux.md#aarch64-runtime-and-agent-checks-without-a-board).
These lanes build, simulate and retain diagnostic artifacts; DE25 physical
boot, FPGA programming, display and playable-core acceptance remain open.
The default appliance and playable packages target DE10-Nano.

Agents: read [AGENTS.md](AGENTS.md), then the
[development guide](docs/development.md) and the selected component's guidance.
[Component boundaries](docs/component-boundaries.md) describe ownership.

## Documentation

Start at the [documentation index](docs/README.md). Core ABI, interfaces and
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
Quartus-built core variants (accepted in #1163). Playable recipes use OSS packages; PC/XT selects a Quartus diagnostic package.
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

For compatible kit software updates, use `make kit-software` with a retained
verified base image; see [kit software releases](docs/kit-software-releases.md).

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

The normal package-only build uses the authenticated HIP/nextpnr producers and
their shared compiler cache beneath the primary FES checkout’s `out/cache`
(or the absolute `FES_CACHE_ROOT` override), reused across FES worktrees. A
matching package is reused only after its locked inputs, manifest, payload and
sealed selection are checked; a miss runs that package's registered producer.
The menu uses its per-core HIP lock. Both RAM Tester rates and the RAM Tester
timing diagnostic use `sources/misteross/cores/fes-ramtest/toolchain.lock`;
these per-core locks currently select the same compiler pins.
Quartus Lite 17.0.2 is available as an explicit bring-up/oracle check where a
recipe documents one. The [Quartus-built cores](docs/design/quartus-cores.md)
policy also allows a labelled Quartus variant of a core, built on a pinned host
authorized for its edition and device, with a license when required. The
current integrated recipe and package-selection path does not
yet select such variants; a failed HIP route never falls back to Quartus.
Downloads are checked against component locks; image compilation runs twice in
independent build roots with networking disabled.

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

The parent exposes one FES integration profile. Systems whose nextpnr route is
not implemented yet are checked explicitly with Quartus when their recipe
requires it; that check does not create an image package or change the default
package-only path. The accepted policy also allows labelled Quartus variants,
with PC/XT selecting a Quartus diagnostic package; see
[Quartus-built cores](docs/design/quartus-cores.md). Quartus is never an
automatic fallback.

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
software checks and core simulation cases in parallel with an always-reported
integration result. PRs, main-branch pushes and merge groups run the smoke
simulation tier; nightly and manual runs cover the complete suite in the full
tier. Feature-branch pushes do not duplicate PR runs. Routine runs also skip the full-device video and
real-media suites and use concurrency-focused race checks; those extended
modes return when their inputs change or on scheduled/manual runs. See
[selection and coverage](docs/test-changed.md#selection-and-coverage).
The normal repository token
is sufficient; first-party checkout needs no sibling-repository secret.
Quartus, full image builds and physical checks run on the development machine.

The normal profile installs the locked idle RBF and MENU display firmware.
Older multi-system validation records remain historical evidence and do not
change the package-only default.
