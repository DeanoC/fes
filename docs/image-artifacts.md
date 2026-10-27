# Bootstrap OS assembly inputs

`make image` assembles the OS from an explicit set of prebuilt MENU firmware,
agent, runtime and UI binaries. It never builds those components on a cache miss.
A missing or changed input stops assembly with an error. The artifact set contains
no playable cores; install those through the existing core catalog.

Start by exporting from a retained, receipt-checked image on Linux (`debugfs` from
`e2fsprogs` is required). An older multi-core image is also suitable if its
runtime accepts its MENU: the export selects only its MENU firmware and bootstrap
software.

Every set must pass the menu check that image assembly applies
([image compatibility](kit-software-releases.md#image-compatibility)). `export`,
`pack`, `check` and `make image` read the interfaces the runtime declares in its
`.fes_menu_interfaces` section (`readelf` from binutils is required) and refuse a
set whose runtime does not accept every interface its MENU declares. A runtime
built before that section existed declares none, so an image that carries one
cannot seed a set. Exporting from an image that was itself assembled from a set
selects the same binaries again; to follow current sources, export from a fresh
`make build` output or pack newly built binaries.

```sh
python3 scripts/image_artifacts.py export \
  --image-output /absolute/retained/native-integration-dev \
  --output /absolute/artifacts/bootstrap-v1
python3 scripts/image_artifacts.py check /absolute/artifacts/bootstrap-v1
make image IMAGE_ARTIFACTS=/absolute/artifacts/bootstrap-v1
make verify IMAGE_ARTIFACTS=/absolute/artifacts/bootstrap-v1
```

`verify` still requires the separately built host receipt (`make host`). Build
and verification do not deploy. The exported directory is portable; keep it
with the image inputs or copy it to another build host. Do not edit a published
set in place. Export a new directory when selecting a new integration build.

You can also select independently produced artifacts without a prior image:

```sh
python3 scripts/image_artifacts.py pack \
  --bin-dir /absolute/selected-binaries \
  --menu-package /absolute/menu-package \
  --menu-selection /absolute/fes-menu.package-selection.toml \
  --fogcast-revision FULL_SOURCE_COMMIT --runtime-revision FULL_SOURCE_COMMIT \
  --output /absolute/artifacts/bootstrap-v2
```

The binary directory contains the six names listed below, without platform
suffixes. To try a different binary, prepare a new input directory with that
binary and the other selected files, then pack a new artifact set. Packing runs
no producer. The two base helpers must be static ARM EABI5 executables; other
package ELF library compatibility is checked again during image assembly.

The manifest records each ARM executable's digest and its original software
provenance, plus the existing MENU package selection and member digests. Assembly
copies binaries into its private source snapshot, checks them again, and includes
the manifest in the image fingerprint. Its cache key binds the OS recipe,
profile, selected FogCast bundle validator and artifact manifest; changing runtime
source alone does not invalidate an otherwise identical artifact assembly.
Runtime source does not need to match the OS checkout and is not mounted or
compiled in this path. The existing rootfs checks validate software-bundle
binaries against the new base's ELF libraries before sealing the factory bundle.
This is interface validation, not kit acceptance.

The six bootstrap binaries are `mister-agent`, `mister-runtime`, `fogcast-kit`,
`fogcast-tenfoot`, `fes-kit-data` and `fes-kit-software`. The last two implement
base storage and software activation. Assembly still builds its host-side
validators, the OS libraries/toolchain when needed, and checks the locked idle
firmware. It does not claim a compiler-free OS build.

`make build` and `make rebuild` remain explicit source integration operations:
they can run MENU, Go and runtime producers and provide a fresh image from which
to export artifacts. `make dev` remains the incremental source integration path.
Use `make kit-software` for routine agent/runtime/UI engine updates and the room
installer for Lua/art changes; neither requires this OS assembly step. When the
toolchain cache has no SDK for the current image key, run `make kit-sdk` first.

OS assembly defaults to one pass, including CI. Use `IMAGE_PASSES=2` for an
explicit two-build reproducibility comparison and the same setting for verification
and export. Single-pass output retains structural, hash and QEMU checks and supports
normal release/bootstrap/media assembly. A real bootstrap
boot, core install/play, software update and failed-install recovery remain
separate integration evidence.
