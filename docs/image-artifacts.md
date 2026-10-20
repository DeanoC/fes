# Bootstrap OS assembly inputs

`make image` assembles the OS from an explicit set of prebuilt MENU firmware,
agent, runtime and UI binaries. It never builds those components on a cache miss.
A missing or changed input stops assembly with an error. The artifact set contains
no playable cores; install those through the existing core catalog.

Start by exporting from a retained, receipt-checked image on Linux (`debugfs` from
`e2fsprogs` is required). An older multi-core image is also suitable: the export
selects only its MENU firmware and bootstrap software.

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

The manifest records each ARM executable's digest and its original software
provenance, plus the existing MENU package selection and member digests. Assembly
copies binaries into its private source snapshot, checks them again, and includes
the manifest in the image fingerprint. Runtime source does not need to match the
OS checkout and is not mounted or compiled in this path. The existing rootfs
checks validate the binaries against the new base's ELF libraries before sealing
the factory software bundle. This is interface validation, not kit acceptance.

The six bootstrap binaries are `mister-agent`, `mister-runtime`, `fogcast-kit`,
`fogcast-tenfoot`, `fes-kit-data` and `fes-kit-software`. The last two implement
base storage and software activation. Assembly still builds its host-side
validators, the OS libraries/toolchain when needed, and checks the locked idle
firmware. It does not claim a compiler-free OS build.

`make build` and `make rebuild` remain explicit source integration operations:
they can run MENU, Go and runtime producers and provide a fresh image from which
to export artifacts. `make dev` remains the incremental source integration path.
Use `make kit-software` for routine agent/runtime/UI engine updates and the room
installer for Lua/art changes; neither requires this OS assembly step.

Image pass/reproducibility policy is unchanged in this slice. A real bootstrap
boot, core install/play, software update and failed-install recovery remain
separate integration evidence.
