# Native image assembly ownership

FES is the parent image command and the authoritative **recipe** for
Buildroot, rootfs layout and native image scripts. FogCast supplies the
agent, kit launcher and extra-core selector through `FOGCAST_DIR`. FES owns
external-artifact policy in `image/build/native-inputs.toml` and derives the
runtime revision from the selected real FES commit and runtime module path.

Do not invoke FogCast `make target-image-native` as a second builder.

## Operator path

From the FES root:

| Want | Command |
| --- | --- |
| Diagnostic rootfs | `make dev` |
| Cold two-pass rootfs | `make build` then `make verify` |
| Flashable disk | `make media` after verify |
| Versioned appliance | `make release` / `make appliance-media` |

Those commands build FogCast agent, kit and selector/verifier binaries, then run
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

- `cmd/mister-agent` and `cmd/fogcast-kit` (installed ARM binaries)
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
container package-lock SHA-256, and `SOURCE_DATE_EPOCH`. The cache receipt
records the archive SHA-256; both are checked before reuse. A missing or changed
archive triggers a cold toolchain build. `TOOLCHAIN_REBUILD=1 make build` forces
that rebuild and also bypasses the parent image receipt reuse.

Both image passes extract the checked archive into a fresh
`/target-image-output/external-toolchain/host`, run the SDK relocation script,
and select it as a custom external glibc/C++ toolchain. The path is the same in
each separate pass. Buildroot copies the cross compiler into each pass's own
`host/bin/arm-buildroot-linux-gnueabihf-*`, preserving the kernel, QEMU and
media tools' existing paths. The shared fragment pins ARM Cortex-A9 hard-float,
GCC 9.x, Linux 5.10 headers, glibc and C++; generated Buildroot `.config`
files are checked against those selections. `reproducibility.txt` and
`verification.json` bind the image to the toolchain key and archive digest.

This should save about 5.6 minutes on pass two, and about 5.6 minutes per pass
on a cache hit. A real cold image build must confirm the SDK's external-toolchain
validation and rootfs byte equality before claiming the saving or a release.

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
build uses the retained native image compiler under `work-2-native-dev/host`.
Kernel/U-Boot inputs and splash seals are unchanged; changed packaging still
requires a fresh committed-source image build and separate hardware acceptance.

Incremental native builds can retain the previous post-build rootfs. Preparation
verifies any retained RAM Tester package and installed selection record, then
checks its exact license and source notice before allowing that one `SOURCE.md`
through the generic payload scan. It installs the selected agent, kit and
tenfoot binaries before scanning the complete tree for secret assignments.
The later native post-build step refreshes packages and notices to the current
selection; other Markdown files remain forbidden.

Normal package recipes require explicit functional identity version 2. Older
artifact caches remain untouched but cannot supply legacy build evidence to
this route. Splash firmware evidence retains its distinct diagnostic schema.
