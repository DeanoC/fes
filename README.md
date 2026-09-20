# FES

Fogger Entertainment System contains the first-party source for FogCast and
the native MiSTer system. One FES commit selects host, runtime, shared contracts
and FPGA sources. Develop a feature across modules in one worktree and one PR.

Agents: read [AGENTS.md](AGENTS.md), then the
[development guide](docs/development.md) and the selected component's guidance.
[Component boundaries](docs/component-boundaries.md) describe ownership.

## Documentation

- [Getting started](docs/getting-started.md): setup, build choices, running the host and common failures.
- [Bootable media](docs/bootable-media.md): build, provision and verify a flashable native image.
- [Appliance releases](docs/appliance-releases.md): versioned images, prepared cards, network updates and automatic fallback.
- [Described FPGA core packages](docs/core-packages.md): build, inspect, load and stop the FES package set.
- [Project map](docs/project-map.md): what runs where, component responsibilities and directory layout.
- [Artifact identities](docs/artifacts.md): named host/target/FPGA/OS outputs and what may differ.
- [Image assembly](docs/image-assembly.md): FES `image/` recipe vs FogCast inputs.
- [Agent workflow](docs/agent-workflow.md): assignments, worktrees, integration and handoffs.
- [Documentation index](docs/README.md): current guides, validation records and historical plans.

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
authenticated HIP/nextpnr producer. Quartus is a build-time oracle/check for
systems not yet supported by nextpnr, never a launch dependency or hidden
fallback.

## Start

Use Linux amd64 with Git, GNU Make, Python 3.11+, Go and Docker. Go selects the
version in the selected FogCast `go.mod` (currently 1.26.5). First-party sources
are included here; external build dependencies retain their own locks.

```sh
git clone git@github.com:DeanoC/fes.git
cd fes
make check
make host
```

For an older submodule checkout, preserve component work and use a fresh clone
for the migration; do not overwrite nested working repositories.
`make check` verifies clean selected modules, package YAML,
twenty generated consumers, twenty shared fixture copies and copied source
pins. It needs Go,
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

For clean integration and release verification:

```sh
make build
make verify
```

The default `native-integration-dev` selects component revisions through the
FES commit, retains the locked idle RBF, and installs the ordered,
closed format-2 package set `fes.pong`, `fes.zx81`, `fes.coleco`. Each selected
package is independently resolved, cached, installed and recorded; this is the
closed package set for the default image, and package-only verification rejects
missing, extra or misidentified packages. The FES image has no legacy bundle
lane.

The normal package-only build uses the authenticated HIP/nextpnr producers and
their shared compiler cache beneath the primary FES checkout’s `out/cache`
(or the absolute `FES_CACHE_ROOT` override), reused across FES worktrees. A
matching package is reused only after its locked inputs, manifest, payload and
sealed selection are checked; a miss runs that package's format-2 producer.
Quartus Lite 17.0.2 remains available only as an explicit bring-up/oracle check
where a recipe documents one. It is not run by the default FES path, and a
failed HIP route never falls back to Quartus.
Downloads are checked against component locks; image compilation runs twice in
independent build roots with networking disabled.

```text
out/native-integration-dev/
  fogcast-api                 Linux amd64 server with browser UI
  fogcast                     Linux amd64 CLI
  linux.img                   ARMv7 target root filesystem
  idle.rbf                     locked MiSTer idle RBF
  fes-*.package-selection.toml one selection record per package
  core-packages/<package-id>/  exact manifest.toml and core.rbf
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

The native image supports the ordered FES Pong, ZX81 and Coleco package set
through the package/library lifecycle. It does not promise generalized/custom
RBF ABIs or useful video/input from arbitrary development cores. The SDL tenfoot client
remains a component build, not a parent output. `linux.img` is the target root
filesystem; run `make media` after a verified cold build to publish the
flashable disk image. Build, media assembly and verification do not deploy or
contact the kit. QEMU checks packaging, not FPGA behavior; exact-image hardware
acceptance is separate; see [bootable media](docs/bootable-media.md). Current gitlink diagnostic evidence is in the
[dual-PLL native diagnostic](docs/dual-pll-native-diagnostic.md). Earlier clean
two-pass evidence is in [integration validation](docs/integration-validation.md).

## Commands and profiles

| Command | Result |
| --- | --- |
| `make test` | Parent, platform and image tests; requires their container prerequisites |
| `make test-changed TEST_CHANGED_ARGS='--base origin/main'` | Affected software checks and dependent consumers |
| `make dev-snapshot` | Freeze local edits in a separate diagnostic-only checkout |
| `make source-status` | Read-only selected revisions, local changes and observed remote main freshness |
| `make check` | Current component and shared-definition consistency |
| `make doctor` | Selected pins/lock, Linux architecture, Go and container availability |
| `make host` | Linux API server and CLI |
| `make dev` | Incremental diagnostic native image with retained compiler/base |
| `make package-acceptance PACKAGE_ACCEPTANCE_ARGS='...'` | Explicit opt-in single-package lifecycle diagnostic; no image rebuild ([guide](docs/package-acceptance.md)) |
| `make package-acceptance-isolated PACKAGE_ACCEPTANCE_ISOLATED_ARGS='...'` | Private-host package lifecycle and restart diagnostic; live library is not mounted ([guide](docs/package-acceptance.md)) |
| `make build` | Host and two-pass native image, reusing matching checked outputs |
| `make image` | Image only with structural checks |
| `make verify` | Require host/image receipts, verify image, two-pass hashes and QEMU packaging |
| `make media` | Publish a verified flashable disk image, auto-embedding the local target agent config |
| `make verify-media` | Reverify the published disk image and embedded root filesystem; no device writes |
| `make rebuild` | Force host and both package-only image passes |

| Profile | Selected source combination |
| --- | --- |
| `native-integration-dev` (default) | Current FES commit, locked idle RBF and the ordered `fes.pong`, `fes.zx81`, `fes.coleco` package set |

The parent exposes one FES integration profile. Systems whose nextpnr route is
not implemented yet are checked explicitly with Quartus when their recipe
requires it; that check does not create an image package or change the default
package-only path.

## Development and integration

Edit modules under `sources/` in an isolated FES worktree. Committed integration
builds use disposable full-repository snapshots under `out/work/`, retaining the
actual FES commit and module path. Builds in one
checkout are serialized and each profile uses a distinct Docker output volume.
Ambient Go and image-selection overrides are normalized.

The integrator reviews one feature diff, runs `make check`, and
records the tested commit. See [development and handoffs](docs/development.md)
for exact commands and how several agents can work independently.

CI computes affected modules and dependent consumers, then runs their software
checks with an always-reported integration result. The normal repository token
is sufficient; first-party checkout needs no sibling-repository secret.
Quartus, full image builds and physical checks run on the development machine.

The normal profile installs the locked idle RBF and the ordered FES package set.
Older multi-system validation records remain historical evidence and do not
change the package-only default.
