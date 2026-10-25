# Native image assembly ownership

FES is the parent image command and the authoritative **recipe** for
Buildroot, rootfs layout and native image scripts. FogCast supplies the
agent, kit launcher and extra-core selector through `FOGCAST_DIR`. FES owns
external-artifact policy in `image/build/native-inputs.toml` and derives the
runtime revision from the selected source or explicit artifact provenance.

Do not invoke FogCast `make target-image-native` as a second builder.

## Operator path

From the FES root:

| Want | Command |
| --- | --- |
| Diagnostic rootfs | `make dev` |
| Clean one-pass rootfs | `make build` then `make verify` |
| Reproducibility check | `IMAGE_PASSES=2 make build` then `IMAGE_PASSES=2 make verify` |
| Flashable disk | `make media` after verify |
| Versioned appliance | `make release` / `make appliance-media` |

Image builds default to one pass locally and in CI. Verification retains image
hashes, structural checks and QEMU packaging, and records reproducibility as
`not-run-single-pass`. Single-pass images support ordinary release export,
bootstrap and media assembly. No special version name or scratch marker is needed.
Set `IMAGE_PASSES=2` consistently for build, verification and export to request two
independent rootfs builds and matching hashes. Receipts and release evidence bind
the actual pass count; a one-pass image cannot claim two-pass verification.

`make image IMAGE_ARTIFACTS=/absolute/artifacts` consumes named prebuilt
MENU and userspace inputs; see [bootstrap artifacts](image-artifacts.md).
It builds only assembly helpers and OS dependencies. Source integration commands
build FogCast agent, kit and selector/verifier binaries, then run
`make -C image FOGCAST_DIR=<snapshot>/sources/FogCast`. Do not run FogCast
`make target-image-native` as the parent integration path.

## Recipe identity (FES `image/`)

These paths in the FES tree are the native image recipe. Changing any of
them is an image-recipe change:

- `image/Makefile` (`target-image-native` and related targets)
- `image/scripts/target-image-container.sh`
- `image/scripts/fetch-target-image-sources.sh`
- `image/scripts/fetch-native-runtime-inputs.sh`
- `image/scripts/build-target-image.sh`
- `image/scripts/toolchain_cache.py`
- `image/scripts/verify-target-image.sh`
- `image/scripts/verify-target-image-source-cache.sh`
- `image/scripts/qemu-smoke-target-image.sh`
- `image/scripts/native-extra-cores.sh`
- `image/build/target-image.sources.lock.toml`
- `image/build/native-inputs.toml`
- `image/buildroot/`
- `image/containers/target-image/`

The container mounts FES `image/` as `/work`. For imported modules, it mounts
the complete committed source snapshot read-only at `/fogcast`, with the FogCast
module at `/fogcast/sources/FogCast`. This retains the real root Git metadata
inside the container. Historical standalone FogCast checkouts use the supported
legacy `/fogcast` layout.

## FogCast inputs

The tracked `sources/FogCast` module owns:

- `cmd/mister-agent`, `cmd/fogcast-tenfoot` and the `cmd/fogcast-kit` stub (installed ARM binaries)
- `cmd/target-image-lock` (extra-core selector / lock verifier)
- public `appliance` schema/store module consumed by FES `platform/`

FES `platform/` owns `fes-boot`. Appliance bootstrap assembly builds that
module against the selected FogCast `appliance` module through a temporary
Go workspace; it does not build FogCast `cmd/fes-boot`.

FPGA cores enter as sealed bundles / packages, not as a second image builder.

FES writes a concrete `build/native-runtime.inputs.lock.toml` inside its disposable
FogCast module in the assembly snapshot. This generated overlay contains the selected runtime
revision and FES external-artifact policy; it is not a FogCast source input.
The staging path restores/removes only this declared overlay before reuse and
continues to reject unrelated changes. Explicit hardware diagnostics receive the
concrete generated lock explicitly through `NATIVE_RUNTIME_INPUT_LOCK`.


## Source and cache boundaries

### Buildroot cross toolchain

The cold image path builds the pinned Buildroot cross toolchain once in the
target-image container, exports its relocatable SDK with `make sdk`, and stores
a deterministic `host.tar` under
`image/build/cache/target-image/toolchains/<key>/`. The key hashes the pinned
Buildroot commit, the shared toolchain fragment bytes, the target-image
container package-lock SHA-256, `SOURCE_DATE_EPOCH`, and the builder context:
the lock's `[container]` image, platform and digest, plus the
`containers/target-image/Dockerfile` and `create-builder-user.sh` bytes. The cache receipt
records the archive SHA-256; both are checked before reuse. A missing or changed
archive triggers a cold toolchain build. `TOOLCHAIN_REBUILD=1 make build` forces
that rebuild and also bypasses the parent image receipt reuse.

`make kit-sdk` publishes that same archive without an image build. It fetches
the pinned Buildroot checkout and Linux kernel Git source, plus the downloads
selected by the toolchain configuration, then runs `publish_toolchain` in
`image/scripts/build-target-image.sh` inside the target-image container. The
key, fragment, builder and `toolchain_cache.py` package step are the ones
`build-target-image.sh --ensure-toolchain native-dev` uses, so the `host.tar`
bytes match. Core
packages, idle and splash RBFs, and the native runtime input lock are not
inputs. The image path still checks those before it calls the same publisher.
With `FES_TARGET_IMAGE_SHARED_CACHE` set, the archive lands in
`toolchains/<key>/` under that directory. `TOOLCHAIN_REBUILD=1 make kit-sdk`
rebuilds it.

Both image passes extract the checked archive into a fresh
`/target-image-output/external-toolchain/host`, run the SDK relocation script,
and select it as a custom external glibc/C++ toolchain. The path is the same in
each separate pass. Buildroot copies the cross compiler into each pass's own
`host/bin/arm-buildroot-linux-gnueabihf-*`, preserving the kernel, QEMU and
media tools' existing paths. The shared fragment pins ARM Cortex-A9 hard-float,
GCC 9.x, Linux 5.10 headers, glibc and C++; generated Buildroot `.config`
files are checked against those selections. `reproducibility.txt` and
`verification.json` bind the image to the toolchain key and archive digest.
The native post-build extracts one more, unrelocated copy to build the
fes-window kernel module. Relocation rewrites GCC's plugin `configargs.h`, and
cc1 then refuses the kernel's GCC plugins. The module build maps its absolute
paths out of the debug information that its build ID hashes, so both passes
build the same module.

This should save about 5.6 minutes on pass two, and about 5.6 minutes per pass
on a cache hit. A real cold image build must confirm the SDK's external-toolchain
validation and rootfs byte equality before claiming the saving or a release.

Set `FES_TARGET_IMAGE_SHARED_CACHE=/absolute/host/path` to share Buildroot caches
across FES worktrees. The directory must be outside the worktree. It contains
`dl/` for locked downloads, `toolchains/<key>/` for the checked SDK archive and
receipt, and `ccache/` for compiled target and host objects. The container mounts
`dl/` at Buildroot's existing download path, so source lock verification still
runs before every use. The toolchain-only build leaves ccache disabled, preserving
its key and SDK bytes. Image passes and `make dev` enable a 20 GiB ccache.

For cold two-pass builds, pass 1 may use ccache and pass 2 sets
`CCACHE_DISABLE=1`. The second rootfs is therefore compiled independently; a
bad cached result must fail the existing hash comparison. Shared-cache evidence
records this policy in `reproducibility.txt` and `verification.json`. A
single-pass image may use ccache and does not claim reproducibility. With the
variable unset, configs, container arguments and evidence retain their original
form. To clear the cache, stop image builds using it, then remove the specific
`dl/`, `toolchains/` or `ccache/` directory under the configured root; the next
build repopulates it.

Parent builds materialize real committed FES snapshots under ignored `out/work`;
module paths and subtree identities accompany the root commit. They do not invent
child commits or consume uncommitted task changes. The generated runtime lock is
a declared assembly overlay, never an input to commit into FogCast.

Splash and idle copy the in-tree seal
`sources/misteross/sealed/fes-splash.rbf`, one core installed as FAT
`/idle.rbf` and rootfs `/usr/share/mister-runtime/idle.rbf`. They do not wget standalone
`DeanoC/misteross`. Other private GitHub pins still use
`image/scripts/fetch-native-runtime-inputs.sh` with `GITHUB_TOKEN` or
`GH_TOKEN` (`contents:read`). The image fetch container forwards that
token; the offline build (`run`) container does not. Public GitHub pins
use unauthenticated `raw.githubusercontent.com` when no token is set.

The compiler and immutable core-package caches default to the primary FES
checkout's `out/cache`, shared by its worktrees. `FES_CACHE_ROOT` can select another
absolute stable location. Cached FPGA packages keep their original manifest,
payload and build record. Functional-identity-2 reuse
records current selection separately from original provenance; reuse does not
qualify a new image on hardware. External compilers remain locked dependencies.

The source import mapping in
[config/source-imports.toml](../config/source-imports.toml) preserves the old
component identities. The imported layout requires its own assembly and
exact-artifact validation; neither that mapping nor a successful host test claims
published cutover, reproducible image completion or hardware acceptance.

## Supported image recipe

`native-dev` is the only image variant. Conventional `prod`, `dev`, and
`--fast-dev` entrypoints have been retired. The native overlay directly owns
per-card Ethernet MAC ([bootable media](bootable-media.md#per-card-ethernet-mac)),
network, SSH, supervisor, mount-smoke, agent and runtime services. The kernel
build uses the selected pass's retained native image compiler:
`work-1-native-dev/host` by default, `work-2-native-dev/host` with `IMAGE_PASSES=2`.
Kernel/U-Boot inputs and splash seals are unchanged; changed packaging still
requires a fresh committed-source image build and separate hardware acceptance.

Incremental native builds can retain the previous post-build rootfs. Preparation
requires a retained RAM Tester notice to match the current template exactly;
stale or changed source text fails closed and is not rewritten in place. It also
verifies the exact license and installed selection record before allowing that
one `SOURCE.md` through the generic payload scan. The normal fresh package install
generates notices from the selected package's sealed manifest. Preparation installs
the selected agent, kit and tenfoot binaries before scanning the complete tree for
secret assignments. The later native post-build step refreshes packages and
notices to the current selection; other Markdown files remain forbidden.

Normal package recipes require explicit functional identity version 2. Older
artifact caches remain untouched but cannot supply legacy build evidence to
this route. Splash firmware evidence retains its distinct diagnostic schema.
