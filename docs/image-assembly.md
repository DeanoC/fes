# Native image assembly ownership

FES is the parent image command and the authoritative **recipe** for
Buildroot, rootfs layout and native image scripts. FogCast supplies the
agent, kit launcher, extra-core selector and native-runtime lock as
inputs through `FOGCAST_DIR`.

Do not invoke FogCast `make target-image-native` as a second builder.

## Operator path

From the FES root:

| Want | Command |
| --- | --- |
| Diagnostic rootfs | `make dev` |
| Cold two-pass rootfs | `make build` then `make verify` |
| Flashable disk | `make media` after verify |
| Versioned appliance | `make release` / `make appliance-media` |

Those commands build FogCast agent/kit/lock binaries, then run
`make -C image FOGCAST_DIR=<FogCast checkout>`. Do not run FogCast
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
- `image/buildroot/`
- `image/containers/target-image/`

The container mounts FES `image/` as `/work` and FogCast as `/fogcast:ro`.

## FogCast inputs

The selected FogCast gitlink still owns:

- `cmd/mister-agent` and `cmd/fogcast-kit` (installed ARM binaries)
- `cmd/target-image-lock` (extra-core selector / lock verifier)
- `build/native-runtime.inputs.lock.toml` (runtime/idle/Mega Drive policy)

FPGA cores enter as sealed bundles / packages, not as a second image builder.
