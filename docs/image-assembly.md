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
continues to reject unrelated changes. Legacy smoke diagnostics receive the
concrete generated lock explicitly through `NATIVE_RUNTIME_INPUT_LOCK`.


## Source and cache boundaries

Parent builds materialize real committed FES snapshots under ignored `out/work`;
module paths and subtree identities accompany the root commit. They do not invent
child commits or consume uncommitted task changes. The generated runtime lock is
a declared assembly overlay, never an input to commit into FogCast.

Splash and idle copy the in-tree seal
`sources/misteross/sealed/fes-splash.rbf`. They do not wget standalone
`DeanoC/misteross`. Other private GitHub pins still use
`image/scripts/fetch-native-runtime-inputs.sh` with `GITHUB_TOKEN` or
`GH_TOKEN` (`contents:read`). The image fetch container forwards that
token; the offline build (`run`) container does not. Public GitHub pins
use unauthenticated `raw.githubusercontent.com` when no token is set.

The compiler and immutable core-package caches default to the primary FES
checkout's `out/cache`, shared by its worktrees. `FES_CACHE_ROOT` can select another
absolute stable location. Cached FPGA packages keep their original manifest,
payload and build record. Functional-identity reuse, where enabled by a recipe,
records current selection separately from original provenance; reuse does not
qualify a new image on hardware. External compilers remain locked dependencies.

The source import mapping in
[config/source-imports.toml](../config/source-imports.toml) preserves the old
component identities. The imported layout requires its own assembly and
exact-artifact validation; neither that mapping nor a successful host test claims
published cutover, reproducible image completion or hardware acceptance.
