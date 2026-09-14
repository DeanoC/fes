# Getting started with FES

Start at FES when you want to build a known combination of FogCast, the native
runtime and FPGA artifacts. Start a component worktree when you want to change
one of those parts. The [project map](project-map.md) explains the distinction.

## 1. Prepare the checkout

Parent builds currently run on Linux amd64. Install Git, GNU Make, Python 3.11+
and Go; image builds also require a running Docker-compatible container engine.
Go selects the version required by the chosen FogCast `go.mod`. The normal FES
FPGA package route uses the authenticated HIP/nextpnr producers. Quartus Lite
17.0.2 is needed only for an explicit historical format-1 profile or a recipe's
bring-up/oracle check; it is not required by the default package-only path.

You need Git access to FES and all four component repositories. The clone
example below uses SSH for FES, but `.gitmodules` currently uses HTTPS for the
components. Configure your Git HTTPS credential helper, or an existing Git URL
rewrite to your authenticated SSH setup; parent SSH access alone is insufficient.

```sh
git clone --recurse-submodules git@github.com:DeanoC/fes.git
cd fes
make test
make check
```

For an existing checkout, inspect `git status` and `git submodule status` before
running `git submodule update --init --recursive`. Preserve any component work
before changing its checkout. The parent expects each `sources/` directory to
be clean and at its indexed revision.

`make test` checks the parent without external services. `make check` validates
the selected component revisions, the runtime lock and generated definitions.
It needs Go and the components, but does not build an image or use the kit.
For host-only work, continue with `make host`; Docker is not required for this
path. `make doctor` checks the container engine as well, and does not check
Quartus availability. Avoid `git submodule update --remote`: integration uses
pinned commits, not whichever branch tips are newest.

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
reuses the selected HIP/nextpnr toolchain automatically. A historical
format-1 source build needs the installed Quartus location:

```sh
QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0 \
  make build PROFILE=native-source-dev
```

The first development build can copy a compatible existing clean base or build
it once. Later builds retain unchanged compiler/base packages. Completely
unchanged outputs are reused after hash checks. See
[incremental native builds](development.md#incremental-native-image) for cache
invalidation and output details. `make dev` builds the target image only; run
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
work is intended. A historical format-1 profile may additionally invoke
Quartus.

The default profile is `native-integration-dev`. Historical profiles are for
reproducing earlier combinations, not the normal development starting point:

```sh
make doctor PROFILE=native-dev
make doctor PROFILE=native-source-dev
```

`make verify` requires both host and clean-image receipts, so run `make host`
as well if you previously built only `make image`. The host receipt names
linux/amd64; a Darwin sofa binary is a separate FogCast-native product.

`make media` expects the private host FogCast configuration described in step 3
so it can embed the selected target agent automatically. Set up that file before
publishing media, or set `FES_UNPROVISIONED=1` when an image without an agent
configuration is deliberately required.

`make dev` supports only the current integration profile. The word `dev` in a
historical profile name does not imply that it supports incremental builds.

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
RBF and the ordered `fes.pong`, `fes.zx81`, `fes.coleco` format-2 package set;
use the media command above for the complete flashable layout. Format-1 catalog
cores remain outside this production path.
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
| Missing submodule | Confirm Git access, then initialize submodules as above |
| `HEAD differs from parent pin` | Inspect the component and staged gitlink; let the integrator reconcile them |
| `source checkout is dirty` | Preserve edits in the component task worktree; see [development](development.md#isolate-component-work) |
| Runtime lock differs from parent pin | Select the FogCast/runtime pair together; changing only the gitlink is insufficient |
| Generated consumer or copied source-pin drift | Reconcile the authoritative package definition and affected consumers; `make check` reports drift but does not repair it |
| Missing Quartus | Only relevant to an explicit historical format-1 profile or documented oracle check; the default package-only path uses HIP/nextpnr |
| Build already running | Coordinate with its operator; one parent build owns this checkout at a time |
| Verify reports stale outputs | Run the matching `make build`; a development receipt cannot replace a clean image receipt |
| Private-component CI checkout fails | Configure repository secret `FES_COMPONENTS_TOKEN` with access to the parent and four components |

Next: [change a component](development.md#isolate-component-work) or
[assign a bounded agent task](agent-workflow.md).
