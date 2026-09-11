# Native image assembly ownership

FES is the parent image command. The selected FogCast gitlink is still the
authoritative **recipe** for Buildroot, rootfs layout and native image
scripts. Do not copy those scripts into FES and leave two builders.

This is the freeze for the assembly migration: name the recipe, keep one
operator path, move the files once later.

## Operator path

From the FES root:

| Want | Command |
| --- | --- |
| Diagnostic rootfs | `make dev` |
| Cold two-pass rootfs | `make build` then `make verify` |
| Flashable disk | `make media` after verify |
| Versioned appliance | `make release` / `make appliance-media` |

Do not run FogCast `make target-image-native` as the parent integration path.
That target remains the recipe implementation FES invokes on a staged FogCast
clone.

## Recipe identity (FogCast pin)

These paths in the selected FogCast revision are the native image recipe.
Changing any of them is an image-recipe change and requires a parent pin:

- `Makefile` (`target-image-native` and related targets)
- `scripts/target-image-container.sh`
- `scripts/fetch-target-image-sources.sh`
- `scripts/fetch-native-runtime-inputs.sh`
- `scripts/build-target-image.sh`
- `scripts/verify-target-image.sh`
- `scripts/verify-target-image-source-cache.sh`
- `scripts/qemu-smoke-target-image.sh`
- `scripts/native-extra-cores.sh`
- `build/target-image.sources.lock.toml`
- `build/native-runtime.inputs.lock.toml`
- `buildroot/`
- `containers/target-image/`
- `cmd/target-image-lock/`

Agent and runtime **binaries** are component outputs consumed by that recipe.
FPGA cores enter as sealed bundles / packages, not as a second image builder.

## After the migration

FES will own Buildroot configuration and SD/media assembly. FogCast will keep
the agent, host apps and recipe *inputs* (locks, extra-core selection). The
move is one cut: no lingering FogCast `target-image-native` as a second
authority.
