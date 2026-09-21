# Getting started with FES

Start at FES when you want to build a known combination of FogCast, the native
runtime and FPGA artifacts. Create a FES task worktree when you want to change
one or several of those modules. The [project map](project-map.md) explains the distinction.

## 1. Prepare the checkout

Parent builds currently run on Linux amd64. Install Git, GNU Make, Python 3.11+
and Go; image builds also require a running Docker-compatible container engine.
Go selects the version required by the chosen FogCast `go.mod`. The normal FES
FPGA package route uses the authenticated HIP/nextpnr producers. Quartus Lite
17.0.2 is only an explicit bring-up/oracle check for a system whose nextpnr
route is not implemented; it is not required by the default package-only path.

The first-party modules are tracked in FES, so a checkout of the reviewed import
revision needs access to this repository rather than four separate component
remotes. Use the branch or commit supplied for the imported layout; do not assume
that a remote default branch has already published this cutover.

```sh
git clone git@github.com:DeanoC/fes.git
cd fes
# Select the reviewed import branch/commit if it is not the default checkout.
git status
make check
```

When coming from the earlier submodule layout, preserve existing component
branches, worktrees and uncommitted changes. Use a separate fresh checkout of the
reviewed import revision, and port the intended changes into its tracked module
paths. Do not remove old module directories or reset existing work to force the
new layout into place. Original URLs, commits and trees are recorded in
[config/source-imports.toml](../config/source-imports.toml); the import retains
original histories.

`make check` validates committed module sources, FES artifact policy and generated
consumers. It needs Go but does not build an image or use the kit. For everyday
software iteration, use the [affected tests](test-changed.md):

```sh
python3 scripts/test_changed.py --base origin/main --plan-only
python3 scripts/test_changed.py --base origin/main
make check-generated
```

`make generate` refreshes mapped definitions and fixtures after authoritative
inputs change; review those edits together with their consumers. The full
`make test` suite additionally covers platform and image-recipe tests and
provisions its pinned platform/media containers. Docker must be available, with
network access for the first provisioning. For host compilation, use `make host`; Docker is
not required for that path. `make doctor` also checks the container engine, and
does not check Quartus availability. Build commands consume committed snapshots;
local module tests can exercise uncommitted work.

## 2. Choose what to build

| Your task | Command | Outputs |
| --- | --- | --- |
| Check Linux/Go/container prerequisites | `make doctor` | Prerequisite and selected-revision report |
| Build the host CLI and browser API | `make host` | `out/native-integration-dev/fogcast` and `fogcast-api` |
| Build an everyday diagnostic target image | `make dev` | `out/native-integration-dev/development/linux.img` |
| Build only the clean two-pass image | `make image` | `out/native-integration-dev/linux.img` and image evidence |
| Build a stabilized host/image combination | `make build` | Host binaries and clean two-pass image under `out/native-integration-dev/` |
| Verify that clean combination | `make verify` | Receipt checks, structural checks and QEMU packaging smoke |
| Publish flashable native media | `make media` | `out/native-integration-dev/media/current/fes.img` and receipts |
| Reverify published native media | `make verify-media` | Media, payload, embedded-rootfs and QEMU checks |

For the default package-only image, the first producer run authenticates or
reuses the selected HIP/nextpnr toolchain automatically. A system whose nextpnr
route is not implemented yet has a separate, recipe-defined Quartus oracle/check;
it is never an automatic fallback and never creates an image bundle.

The first development build can copy a compatible existing clean base or build
it once. Later builds retain unchanged compiler/base packages. Completely
unchanged outputs are reused after hash checks. See
[incremental native builds](development.md#incremental-native-image) for cache
invalidation and output details. Compiler and functional-package caches default
to the primary FES checkout's `out/cache`, shared across its worktrees; an absolute
`FES_CACHE_ROOT` selects another stable location. Do not relocate authenticated
compiler installations by copying their directories. `make dev` builds the target image only; run
`make host` separately when you need host binaries.

For clean integration verification:

```sh
make build
make verify
make media
make verify-media
```

`make build` also reuses matching complete outputs. When it must assemble a new
image, it runs two independent clean passes. `make rebuild` forces the host,
selected package production and both image passes; use it when that expensive
work is intended. The only parent profile is `native-integration-dev`.

`make verify` requires both host and clean-image receipts, so run `make host`
as well if you previously built only `make image`. The host receipt names
linux/amd64; a Darwin sofa binary is a separate FogCast-native product.

`make media` expects the private host FogCast configuration described in step 3
so it can embed the selected target agent automatically. Set up that file before
publishing media, or set `FES_UNPROVISIONED=1` when an image without an agent
configuration is deliberately required.

`make dev` supports the current integration profile and its package-only image
contract.

## 3. Run the host

Host compilation does not configure your game library or target. Keep an
existing working configuration if you have one. For a new setup, save this as
a private file such as `~/.config/fogcast/config.toml` with mode `0600`:

```toml
selected_target = "local"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[[targets]]
name = "local"
enabled = false
```

This minimal configuration is accepted by the selected FogCast loader. It has
no game-library roots and no enabled hardware target. To add a mounted library,
append a table using an existing absolute directory:

```toml
[[libraries]]
id = "megadrive-main"
system = "megadrive"
root = "/absolute/path/to/your/megadrive-games"
```

To connect a configured target, change the target table to `enabled = true` and
supply both `address = "http://YOUR_TARGET:8182"` and
`agent = "YOUR_TARGET_AGENT_TOKEN"`. Replace those placeholders with the actual
settings. The `agent` field is the API credential, not the target's SSH password.
Follow the component's [development guide](../sources/FogCast/docs/DEVELOPMENT.md)
for the designated kit and its existing setup. The
[configuration loader](../sources/FogCast/fogcast/config.go) is authoritative for
accepted fields. Keep local paths and credentials out of the parent repository.

With your configuration available:

```sh
out/native-integration-dev/fogcast --help
out/native-integration-dev/fogcast-api --config /absolute/path/config.toml --listen 127.0.0.1:8787
```

Open `http://127.0.0.1:8787` in your browser. The server stays in the foreground;
stop it with Ctrl-C. If another host instance uses that port, choose a different
port rather than stopping an existing session.

Restart the host after editing its configuration file. After configuring a
library, scan it from another terminal at the FES root:

```sh
out/native-integration-dev/fogcast --config /absolute/path/config.toml scan
out/native-integration-dev/fogcast --config /absolute/path/config.toml games
```

These commands update/read the host's local library index. The SDL tenfoot
launcher has its
own [component build guide](../sources/FogCast/docs/native-tenfoot-launcher/README.md).

## 4. Build bootable media

`linux.img` is an ARMv7 root filesystem, not a flashable SD-card image. After a
successful cold `make build` and `make verify`, run `make media` to publish the
flashable raw disk image at `out/native-integration-dev/media/current/fes.img`.
When the private host configuration exists, this command automatically embeds
the target agent file, so the card can be inserted and booted without creating
files by hand. `make verify-media` rechecks the media without writing a block
device. The default host file is `~/.config/fogcast/config.toml`; override it
with `FES_HOST_CONFIG=/absolute/path/config.toml`.

```sh
make media
```

The generated target config contains only the token and fixed MiSTer paths; its
contents are never logged or committed. Use `make media FES_UNPROVISIONED=1`
only when an unprovisioned image is intentional (CI sets `CI=true` automatically
to prevent host credentials from being picked up). Read [bootable media](bootable-media.md)
for explicit custom configurations, immutable generation rollback and the
separately authorized physical-card acceptance procedure.

## 5. Understand the target output

`linux.img` is an ARMv7 root filesystem for the MiSTer target, not a complete
bootable SD-card image. The default contains the native runtime, locked idle
RBF and the ordered `fes.pong`, `fes.zx81`, `fes.coleco` package set; use the
media command above for the complete flashable layout. The image has no legacy
bundle lane.
The `make build`, `make dev`, `make verify` and `make media` paths use the
same closed package set.

Build commands do not deploy. Use the exact kit and deployment instructions in
the selected FogCast [working policy](../sources/FogCast/AGENTS.md) and
[development guide](../sources/FogCast/docs/DEVELOPMENT.md). A development image
is suitable for bounded diagnostics. QEMU checks packaging; actual FPGA, video
and input behavior require separate hardware checks of the exact image.

## If a check fails

| Message or symptom | Next step |
| --- | --- |
| Imported modules are absent | Check that this checkout contains the reviewed import revision; preserve the earlier checkout and its component work |
| `source checkout is dirty` | Run focused tests during iteration, then commit the reviewed module changes before a selected-source build |
| Generated consumer or copied source-pin drift | Update the authoritative definition, run `make generate`, review all consumer edits, then `make check-generated` |
| Generated runtime lock is stale | Regenerate through the FES build path; artifact policy belongs to `image/build/native-inputs.toml` |
| Private GitHub pin fetch failed / requires `GITHUB_TOKEN` | Splash/idle are in-tree and do not need a misteross token. For other private GitHub pins, export `GITHUB_TOKEN` or `GH_TOKEN` with `contents:read`, then retry. |
| Missing Quartus | Only relevant to a documented oracle/check for a system not yet supported by nextpnr; the default package-only path uses HIP/nextpnr |
| Build already running | Coordinate with its operator; one parent build owns this checkout at a time |
| Verify reports stale outputs | Run the matching `make build`; a development receipt cannot replace a clean image receipt |
| Checkout uses old recursive-component CI instructions | Confirm which FES revision and workflow is running; imported modules are tracked files, while old commits retain their historical checkout requirements |

Next: [change a component](development.md#isolate-component-work) or
[assign a bounded agent task](agent-workflow.md).
