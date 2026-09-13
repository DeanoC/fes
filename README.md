# FES

Fogger Entertainment System is the starting point for developing and building
FogCast and the native MiSTer system. Start here, select a component to work on,
then return here to verify the assembled combination.

Agents: read [AGENTS.md](AGENTS.md), then the
[development guide](docs/development.md) and the selected component's guidance.
[Component boundaries](docs/component-boundaries.md) describe ownership.

## Documentation

- [Getting started](docs/getting-started.md): setup, build choices, running the host and common failures.
- [Bootable media](docs/bootable-media.md): build, provision and verify a flashable native image.
- [Appliance releases](docs/appliance-releases.md): versioned images, prepared cards, network updates and automatic fallback.
- [Described FPGA core packages](docs/core-packages.md): build, inspect, load and stop the standalone FES Pong package.
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
| [misteross](sources/misteross) | FPGA development, compilation and exported RBF bundles |
| [mister-packages](sources/mister-packages) | Shared hardware/system definitions and generated C++/Go |

Main_MiSTer is an original implementation and test reference, not a production
dependency. The target agent talks to the runtime's local socket; only the
runtime controls hardware. Quartus runs on the build machine, not during launch.

## Start

Use Linux amd64 with Git, GNU Make, Python 3.11+, Go and Docker. Go selects the
version in the selected FogCast `go.mod` (currently 1.26.5). All component
repositories must be accessible with your Git credentials.

```sh
git clone --recurse-submodules git@github.com:DeanoC/fes.git
cd fes
make test
make check
make host
```

For an existing checkout, inspect local changes before running
`git submodule update --init --recursive`; preserve component work first.
`make check` verifies clean pinned sources, the runtime lock, package YAML,
fourteen generated consumers, eleven shared fixture copies and copied Mega Drive/SNES/NES source pins. It needs Go,
not Docker or Quartus. `make host` builds the Linux CLI and browser API server. Run `make doctor`
when preparing for container/image builds. See [getting started](docs/getting-started.md)
for Git authentication and a minimal host configuration.

## Build the integrated native system

For routine native development, run `make dev`. It keeps the compiler and base
packages and publishes a structurally checked diagnostic image under
`out/native-integration-dev/development/`. A compatible existing clean build can
seed that cache; otherwise the first run builds the base once. See
[the incremental workflow](docs/development.md#incremental-native-image).

For clean integration and release verification:

```sh
QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0 make build
make verify
```

The default `native-integration-dev` selects component revisions through the
submodule gitlinks, packages source-built Mega Drive, Pong, SNES and NES cores,
and installs the described standalone `fes.pong` development package. Each
format-1 core has its own validated bundle and installed selection record;
the selected NES image has exact video and session-lifecycle acceptance recorded
in [the dated FES validation](docs/validation/2026-09-08-native-nes-wire-acceptance.md).
Historical profiles retain their Mega Drive-only inputs.

A fresh FPGA build requires Quartus Lite 17.0.2. Validated Mega Drive, SNES and
NES bundles may be reused from the workspace-local cache after the current
recipe and bundle manifest still accept them, including across unrelated
`misteross` commits. Pong reuse still requires the exact selected `misteross`
revision. Distinct validated artifacts fail closed; recover by running
`chmod -R u+rwX -- out/cache/fpga-bundles/<system>` then
`rm -rf -- out/cache/fpga-bundles/<system>` and retrying. `make rebuild` bypasses that
cache and rebuilds. Downloads are checked
against component locks; image compilation runs twice in independent build
roots with networking disabled. Allow several GB for tools and outputs.

```text
out/native-integration-dev/
  fogcast-api                 Linux amd64 server with browser UI
  fogcast                     Linux amd64 CLI
  linux.img                   ARMv7 target root filesystem
  {megadrive,pong,snes,nes}.rbf selected source-built cores
  <core>-rbf.toml              FPGA build provenance for each core
  <core>.selection.toml        installed-core selection for each core
  fes-pong.package-selection.toml described-package selection
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

The native profiles support Mega Drive, ROM-less Pong, basic SNES and the existing MiSTer-compatible
development-RBF lifecycle. It does not promise generalized/custom RBF ABIs or
useful video/input from arbitrary development cores. The SDL tenfoot client
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
| `make test` | Parent regression tests; no components or external services required |
| `make check` | Current component and shared-definition consistency |
| `make doctor` | Selected pins/lock, Linux architecture, Go and container availability |
| `make host` | Linux API server and CLI |
| `make dev` | Incremental diagnostic native image with retained compiler/base |
| `make build` | Host and two-pass native image, reusing matching checked outputs |
| `make image` | Image only with structural checks |
| `make verify` | Require host/image receipts, verify image, two-pass hashes and QEMU packaging |
| `make media` | Publish a verified flashable disk image, auto-embedding the local target agent config |
| `make verify-media` | Reverify the published disk image and embedded root filesystem; no device writes |
| `make rebuild` | Force host, Quartus and both image passes |

| Profile | Selected source combination |
| --- | --- |
| `native-integration-dev` (default) | Current gitlinks, four source-built catalog cores and described FES Pong package |
| `native-dev` | Original FogCast `cd85971` / runtime `443b603`, upstream core |
| `native-source-dev` | Same original pair, source-built core and historical lock overlay |

Use `PROFILE=native-dev` or `PROFILE=native-source-dev` to rebuild a historical
combination. Their exact source overrides live in the profile files; those
commits must remain available in component history. Historical profiles lack the
new native development loader. They retain independent output directories and
Docker volumes. Historical hash comparison is informational; see
[original validation and provenance](docs/historical-parent-validation.md) and
[source-profile hardware evidence](docs/source-build-validation.md).

## Development and integration

Keep `sources/` clean at the indexed gitlinks. Components compile from disposable
standalone clones under `out/work/`, which preserve each child's Git identity.
Use separate worktrees under `out/dev/` for component changes. Builds in one
checkout are serialized and each profile uses a distinct Docker output volume.
Ambient Go and image-selection overrides are normalized.

The integrator updates component gitlinks together, runs `make check`, and
records the tested combination. See [development and handoffs](docs/development.md)
for exact commands and how several agents can work independently.

CI runs parent tests, consistency and host compilation. Configure the repository
secret `FES_COMPONENTS_TOKEN` with read access to FES and all four private
components; the default Actions token cannot read sibling private repositories.
CI deliberately fails with an actionable message when this credential is absent.
Quartus, full image builds and physical checks run on the development machine.

The normal profile includes [Pong and basic SNES](docs/multi-system-development.md)
alongside Mega Drive. Native [SNES cartridge saves](docs/snes-saves.md) retain
battery RAM through clean Stop and relaunch. SNES enhancement chips remain outside
this implementation.
