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

Installed core packages have exact directory mode0555 and member mode0444.
Assembly clears inherited directory setgid before checking the seal, including
when its destination is on a shared build disk.

The same selector has an explicit DE25 lane:

```sh
python3 scripts/image_artifacts.py pack --board de25-nano \
  --bin-dir /absolute/selected-aarch64-binaries \
  --menu-package /absolute/selected-de25-menu \
  --menu-selection /absolute/fes-menu.package-selection.toml \
  --fogcast-revision FULL_SOURCE_COMMIT --runtime-revision FULL_SOURCE_COMMIT \
  --output /absolute/artifacts/de25-bootstrap
python3 scripts/image_artifacts.py check --board de25-nano /absolute/artifacts/de25-bootstrap
```

DE25 requires matching board/architecture fields, little-endian AArch64 ELF
inputs, static base helpers and a MENU manifest targeting `de25_nano`. The
operator chooses the board; uploaded metadata cannot select it. The default
remains DE10. `stage(..., board='de25-nano')` copies into the existing AArch64
binary names and preserves the selected source provenance.

The shared factory post-build scripts also accept `FES_TARGET_BOARD=de25-nano`.
They require the exact image board file, select AArch64 helpers, pin idle to
that image's selected MENU and use the built-in owner instead of `fes-window`.
The normal package-selection and interface/ELF-closure checks still run.
DE25 SSH keys live on the bootstrap's checked persistent FAT mount. Shared DHCP
retains its client for lease renewal and uses the configured hostname.

The existing DE25 builder can assemble an explicitly selected factory candidate:

```sh
python3 image/de25-nano/build.py managed-factory \
  --kernel /absolute/matching-owner-kernel \
  --sdk /absolute/pinned-owner-toolchain.tar.gz \
  --artifacts /absolute/artifacts/de25-bootstrap \
  --output /absolute/new-de25-factory
python3 image/de25-nano/build.py verify-factory --output /absolute/new-de25-factory
```

The compiler input is the archive pinned by the owner-kernel lock. Artifact
selection supplies the six target binaries and sealed DE25 MENU. Assembly
builds the shared host validators and Bluetooth helper, runs the ordinary
package selector before the OS build, and uses the shared native factory
post-build and software-bundle packer. It installs the selected runtime through
the existing hash-before-replacement Buildroot recipe. The factory step installs
its current startup script after successful bundle assembly, including when the
preceding managed base deliberately omitted that script.

Outputs include `rootfs.ext4`, `rootfs.tar`, the SDK, matching kernel files,
selected artifacts and `factory.json`. Verification checks the bundle/base/MENU
contracts, executable aliases, source hashes and critical tar/ext4 payloads.
The output is an uninstalled candidate with closed normal admission. See the
[factory action validation](validation/2026-11-02-de25-managed-factory/README.md)
for the initial wiring checks. The later
[Terasic-based factory proof](validation/2026-11-03-de25-terasic-managed-factory/README.md)
completes actual source compilation, factory verification and kernel/rootfs
virtual boot with an admitted MENU package.

The [ordinary image commands](#de25-image-and-export-commands) now route an
explicit DE25 selection through this same factory producer. The
[shared appliance commands](appliance-releases.md#de25-factory-selection)
accept explicit DE25 factory selection for release, AArch64 bootstrap and
regular-file card construction/verification. They require the actual factory,
its bound structural report and successful image boot evidence; the real
managed base is correctly refused. Actual release, bootstrap and two-pass card
assembly now pass; native managed-kit acceptance remains required.
Earlier shared-script fixtures retain their
[factory-wiring validation scope](validation/2026-11-01-de25-factory-inputs/README.md).

## DE25 image and export commands

Select the board independently of the supplied profile, JSON or payloads:

```sh
make image BOARD=de25-nano \
  IMAGE_ARTIFACTS=/absolute/artifacts/de25-bootstrap \
  IMAGE_KERNEL=/absolute/matching-owner-kernel \
  IMAGE_SDK=/absolute/pinned-owner-compiler.tar.gz
make verify BOARD=de25-nano
python3 scripts/image_artifacts.py export --board de25-nano \
  --image-output /absolute/fes/out/de25-nano/factory \
  --output /absolute/new-artifacts/de25-bootstrap
```

The default output is `out/de25-nano/factory`; set `IMAGE_OUTPUT` for both
image and verification when choosing another directory. The Python builder
exposes `--board de25-nano --kernel --sdk --artifacts --output`. The compiler
archive is the owner-kernel lock's original pinned input, distinct from the
managed SDK used for normal C++ runtime builds. MENU still needs its ordinary
sealed-package admission before the OS build starts. The factory producer
retains its cache/output locks and one real image-build pass; `IMAGE_PASSES=2`
is refused until independent two-pass factory production exists.

Verification always rechecks the actual factory and its bound image boot
report/log. It reads recorded inputs by default; optional `IMAGE_ARTIFACTS`,
`IMAGE_KERNEL` and `IMAGE_SDK` must match the factory's selected programs,
package, kernel and owner compiler. The explicit board must agree with the
profile. Omitting the board cannot select DE25 through `PROFILE` or metadata.
The default DE10 commands retain their existing image and host-receipt checks.
DE25 `build`, `rebuild` and `dev` use the
[committed-source workflow](development.md#de25-source-development), with explicit
managed SDK/base and owner kernel/compiler inputs. `make host BOARD=de25-nano`
and `make doctor BOARD=de25-nano` use the shared host/profile checks.

Export obtains fresh factory verification, then uses read-only `debugfs` to
extract the six actual executables. It compares the four bundle programs with
their selected hashes and the two rebuilt base helpers with the factory's
bound producer-input hashes. It does not simply copy the original input
directory. The existing packer rechecks AArch64 ELF, MENU interfaces and closed
package members. The exported manifest binds the source image, factory receipt
and original FES revision, and the source receipt is rechecked before atomic
publication. Existing outputs and factory/output overlap are refused.

Both image operations and export use the existing media/build lease. They
never write a physical device. The [entrypoint validation](validation/2026-11-02-de25-image-entrypoints/README.md)
separates real ext4 fixtures and actual intermediate-image refusals from full
factory/package admission. The later actual factory/card proof covers ordinary
source, release, bootstrap and media production. Export's payload extraction
checks and native kit acceptance retain their own scopes.

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
