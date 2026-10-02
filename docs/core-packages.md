# Described FPGA core packages

Which packages exist, their ABI and their standing are
[core status](core-status.md). This page is how to build, inspect, install
and select one. For persistent Pong settings and best rally, see
[core persistence](core-persistence.md).

The locked idle core is the in-tree misteross seal
`sources/misteross/sealed/fes-splash.rbf`. It is installed twice: FAT
`/idle.rbf`, which U-Boot programs at boot (`core=idle.rbf`), and rootfs
`/usr/share/mister-runtime/idle.rbf`, which the runtime loads for Stop. Attract and rooms are not that
bitstream; see [Idle MENU → rooms](idle-menu-rooms.md).

The recipe registry supports the described `fes.pong`, `fes.zx81`,
`fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.apple2`, `fes.c64`, `fes.spectrum`, `fes.catch` and `fes.ramtest` HIP/nextpnr
producers. The default target-image selector installs the ordered closed
`fes.menu`, `fes.pong`, `fes.zx81`, `fes.coleco`, `fes.ramtest` package set.
SMS, SG-1000 and Spectrum passed timing at the selected identity but remain
registered for package-only use because the combined rootfs exceeds 128 MiB. `fes.apple2`
and `fes.catch` also remain package-only. `fes.c64` is registered, but has no
current timing-passing HIP seal.
Package IDs and payload digests are recorded in generated per-core selection
files. Package-only preparations can produce `fes-sms.package-selection.toml`,
`fes-sg1000.package-selection.toml` and `fes-spectrum.package-selection.toml`.
A successful package-only C64 preparation would use
`fes-c64.package-selection.toml`.
The selected FPGA sources are the tracked
`sources/misteross` module at the selected FES commit. Its repository-default
compiler lock serves factory Pong; the standard ZX81 socket uses
`toolchains/zx81-expansion.lock`; Coleco v2 uses `toolchains/coleco-sgm.lock`,
SG-1000 uses `toolchains/registered-memory.lock`, SMS uses `toolchains/fes-sms.lock`,
Apple II uses `toolchains/apple2.lock`, Commodore 64 uses `toolchains/c64.lock` (the same tool commits as Apple II), and ZX Spectrum uses `toolchains/spectrum.lock`. Inspect `config/core-recipes.toml` for each
registered producer's current lock and HIP settings. Freeze-scaffold
compose is documented in [FPGA cartridge expansion](fpga-expansion.md).
An older sealed SMS package does not accept a bitstream built from a later
tree. See [FES ZX81](fes-zx81.md) for the ZX81 machine contract.

The default `native-integration-dev` profile installs the locked idle RBF and
the ordered `fes.menu`, `fes.pong`, `fes.zx81`, `fes.coleco`, `fes.ramtest` package set.
The FES image route is package-only. Quartus is reserved for a documented bring-up or
oracle/check when a system is not yet supported by nextpnr; the package-only
route does not invoke it.
Each package can be installed on the host and given an explicit library entry;
the package route does not infer ROM or media inputs or replace the existing
catalog system.

## ROM linking transition

Format 4 adds two ordered, required ROM sources to the existing download-time
linker. The development Coleco MegaCart shell uses the household `firmware`
slot for an exact 8,192-byte BIOS and the title's named `coleco-cart` selection
for an exact 131,072-byte cartridge. The source-only `rom-link-v2.json`
archive carries both original objects and an optional exact-shell SGM archive;
the target verifies each digest, composes the expansion and patches the sealed
139,264-byte ROM map before programming. Status exposes both source identities
in `rom_links`. Format-3 `rom_link` and current factory Coleco mailbox paths
retain their existing behavior. This producer is imported explicitly for
development because `config/core-recipes.toml` has one row per core ID.

Format-3 packages seal one named ROM requirement and its CRAM map alongside the
base RBF. Select an exact-size binary for the title using the
[ROM selection API](../sources/FogCast/docs/core-package-library.md).
The host sends source inputs; the target agent's Go linker composes any selected
expansion and merges the ROM before the runtime downloads the resulting RBF.
This path needs no Python on the kit and requires runtime capability
`rom_linking: 1`. Status records the map, source ROM and programmed RBF digests;
restart adoption independently reconstructs the retained programmed bytes.

The ZX81, SMS, SG-1000, Apple II, Commodore 64 and ZX Spectrum producers export format 3 when sealed.
Other core producers retain format 2 and their current media/firmware paths
until explicitly converted. SMS requires an exact 32 KiB `cartridge-rom`;
SG-1000 requires an exact 16 KiB `cartridge-rom`. Pad a shorter fixed-map
cartridge with `0xff` before import. Apple II requires an exact 16 KiB
`apple2-firmware` image covering `$C000–$FFFF`; its slot cards and floppy
media are separate inputs (see the
[Apple II pathfinder design](superpowers/specs/2026-09-26-apple2-pathfinder-design.md)).
Commodore 64 requires an exact 16 KiB `c64-firmware` image (8 KiB BASIC window,
then 8 KiB KERNAL window). Its disk is `fes.media.c64-disk` 1.0, an exact
174,848-byte `.d64` on media unit 0, read only. Its two cartridge sockets are
the optional `fes.expansion.c64-bus` 1.0.
ZX Spectrum requires an exact 16 KiB `spectrum-firmware` image covering
`$0000–$3FFF`. Its edge cards and `.tap` cassette are separate inputs (see the
[ZX Spectrum pathfinder design](superpowers/specs/2026-09-28-spectrum-pathfinder-design.md)).
Hardware evidence is tied to the exact tested package and software; rebuilding
a package does not inherit earlier acceptance. Cartridge ROM packages must remove redundant reset-held application
blob/stream and firmware mailboxes; firmware ROM packages may retain separate
tape/disk input.

## Build and inspect

For a single core without image assembly, use the
[core developer workflow](core-development.md). Producer descriptors live in
`config/core-recipes.toml`; package capabilities remain in sealed manifests.

FES resolves every selected package from its misteross recipe before an image
or development-cache reuse decision. It authenticates the clean misteross
source, the pinned OSS tools and each canonical build-input record. A cached
package is reused only when its matching build-input bytes, manifest, payload,
build identity and provenance all validate. A miss runs that recipe once and
the result goes through the same checks.

The default FES package producers use the authenticated HIP/nextpnr route.
Functional builds also require `/usr/bin/strace` with `--kill-on-exit` support
for compiler input checks. The tracer and its libraries are fingerprinted;
changing them changes the execution identity. Non-executable Markdown remains
documentation: using it as a compiler or ordinary Python helper input rejects
the build instead of sealing a package with an incomplete identity.
The parent default path opts all selected FES package producers into the
shared compiler cache at the primary FES checkout's
`out/cache/misteross-toolchains`, or under the configured `FES_CACHE_ROOT`.
Shared mode verifies the selected misteross checkout and lock pins, then
authenticates Yosys, nextpnr-mistral and Mistral from the matching published
cache slot rather than from `build/toolchain` in that checkout. An old package
record still cannot prove which tools are selected now. A new checkout can
prepare and check the shared slot with:

```sh
make dev
# If this reports missing authenticated FES package build inputs:
# Run from the FES root with clean selected module sources.
work="$PWD/sources/misteross"
cache=$(python3 -c 'import sys; sys.path.insert(0, "scripts"); from recipes import TOOLCHAIN_CACHE_ROOT; print(TOOLCHAIN_CACHE_ROOT)')
FES_TOOLCHAIN_CACHE_ROOT="$cache" \
  make -C "$work" toolchain-fes
FES_TOOLCHAIN_CACHE_ROOT="$cache" \
  FES_TOOLCHAIN_GPU_ROUTER=HIP \
  FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' \
  make -C "$work" doctor-strict
# Seed the standard ZX81 socket slot when the ZX81 package is selected:
FES_TOOLCHAIN_CACHE_ROOT="$cache" \
  make -C "$work" toolchain-fes-zx81
FES_TOOLCHAIN_CACHE_ROOT="$cache" \
  FES_TOOLCHAIN_LOCKFILE=toolchains/zx81-expansion.lock \
  FES_TOOLCHAIN_GPU_ROUTER=HIP \
  FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' \
  make -C "$work" doctor-strict
# Seed the Coleco slot when Coleco packages are selected:
FES_TOOLCHAIN_CACHE_ROOT="$cache" \
  make -C "$work" toolchain-fes-coleco
FES_TOOLCHAIN_CACHE_ROOT="$cache" \
  FES_TOOLCHAIN_LOCKFILE=toolchains/registered-memory.lock \
  FES_TOOLCHAIN_GPU_ROUTER=HIP \
  FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201' \
  make -C "$work" doctor-strict
make dev
```

`FES_TOOLCHAIN_CACHE_ROOT` selects the parent shared-cache root; `CACHE_ROOT`
is not a substitute for it. The `toolchain-fes`, `toolchain-fes-zx81` and
`toolchain-fes-coleco` targets compile the authenticated HIP/nextpnr tools into
their respective slots. `make toolchain` may compile the pinned tools into
that shared slot and is intentionally separate from ordinary parent tests.
`make host` remains independent of package and FPGA tool authentication.
`make check` validates
the shared ABI and programming definitions, their three real generated
consumers, and all shared fixture copies without running synthesis.

The parent publishes and receipts these external image inputs:

```text
fes-menu.package-selection.toml
fes-pong.package-selection.toml
fes-zx81.package-selection.toml
fes-coleco.package-selection.toml
core-packages/<package-id>/manifest.toml
core-packages/<package-id>/core.rbf
core-packages/<package-id>/rom-map.json  # format 3 only
```

The installed directory is
`/usr/share/mister-runtime/core-packages/<package-id>/`. The child image builder
revalidates the package during fetch, install and image verification and records
the selection in its installed build inputs. Cold builds compare the selection
from both independent passes. Development and cold receipts include the exact
selection, manifest, payload and optional ROM-map hashes; a metadata-only manifest change
invalidates image reuse even when the RBF bytes do not change.

`fes.menu` is an image service package, not a playable library core. The native
image installs its sealed selection at
`/usr/share/mister-runtime/selections/fes-menu.package.toml`. Runtime startup
uses that exact package and the kit UI enables native HDMI presentation. The
boot splash remains the fallback if menu activation fails.

On a host with a running `fogcast-api`, inspect and explicitly load a package:

```sh
out/native-integration-dev/fogcast core-inspect /absolute/path/core.fcore
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 \
  core-load /absolute/path/core.fcore
out/native-integration-dev/fogcast status
out/native-integration-dev/fogcast stop
```

`core-inspect` is local and read-only. `core-load` uses the running host-owned
session; its API origin precedence is `--api`, `FOGCAST_API`, then
`http://127.0.0.1:8787`. A successful load retains the package, reports its
declared and observed identities and assigns a generation. `stop` retires input,
returns the runtime to idle and releases the session. A package admission error
does not fall back to interpreting the input as a raw RBF.

Existing raw development-RBF loading remains available through its existing
MiSTer-compatible development route. It retains the earlier conservative
behavior: no inferred game/media launch and no fabricated custom ABI or input
capability. The separate `development-contained-v1` profile is an explicit raw
diagnostic selection with contained bridges and SDRAM. Its live identity is
unverified, so it infers no ABI and exposes no controller or video service.
The package-only image does not use catalog bundle inputs or legacy runtime
system-selection variables; it installs only the selected sealed packages.

## Install and select a library package

For a single sealed package on an already-compatible platform, the
[package-only acceptance runner](package-acceptance.md) automates explicit host
import, compatibility, selection and launch/Stop without rebuilding the image.
The image's closed package set is not the host library's admission allowlist.

The producer writes an installable archive at
`out/work/misteross-<selected-revision>/build/packages/<package-id>.fcore`.
Use the package ID in `fes-pong.package-selection.toml` to select the matching
archive; the ZX81 and Coleco records follow the same per-core naming pattern.
The image directory and the host archive store have separate roles:
installation on the host retains the archive used for future library launches.

With the running host API:

```sh
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-install /absolute/path/core.fcore
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-list
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-check PACKAGE_ID
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-entry 'Standalone FES Pong' PACKAGE_ID
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-entry 'ZX81' PACKAGE_ID
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 core-select GAME_ID CURRENT_PACKAGE_ID NEXT_PACKAGE_ID
```

The selected packages are image inputs as well as host library packages. Host
`core-install` / `core-entry` plus `POST /api/v1/session/launch` with the
returned `game_id` is the library path. The ZX81 package remains a volatile
`fes.simple-computer` package (`fes.keyboard`, no gamepad), and `core-load`
remains development-only without creating a library entry.

Import works offline and never activates hardware. Creating an entry or changing
its selected version requires current target compatibility. Selection is an
explicit checked update for the next launch; the currently running package keeps
its actual identity. Select a retained older package to roll back. Neither
import nor selection rebuilds compilers or FPGA payloads.

### Library media

Multiple titles can use one core/package; only duplicate core/title pairs
conflict. Library entries
select immutable media by SHA-256, independently of the installed package:

```sh
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 --json core-media-install /absolute/path/controller.rom
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 --json core-media-capabilities PACKAGE_ID
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 --json core-entry 'Coleco controls' PACKAGE_ID blob MEDIA_ID
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 --json core-media-select GAME_ID PACKAGE_ID none MEDIA_ID
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 --json core-media-select GAME_ID PACKAGE_ID OLD_MEDIA_ID NEW_MEDIA_ID
out/native-integration-dev/fogcast --api http://127.0.0.1:8787 --json core-media-select GAME_ID PACKAGE_ID OLD_MEDIA_ID none
```

Use the returned `media_id` and `game_id`; the create and select examples are
alternative workflows. `none` is the CLI spelling for an empty selection.
Selection checks both the expected package and media IDs and affects the next
launch. Changing the original file does not change imported bytes; import the
new bytes and explicitly select their new digest. Package changes retain media.

Host storage accepts 1 byte through 32 MiB in bounded-memory streams and 64 KiB catalog
chunks. That limit is separate from the selected core's media capacity.
`core-media-capabilities` reports the offline supported declaration, including
role, format, minimum/maximum size and transport, with target compatibility
explicitly unknown. Unknown interface versions do not inherit larger capacity.

The legacy `fes.media.blob` 1.0 target transport accepts 1 through 16,384 bytes
(16 KiB), with either `fes.simple-computer` 1.0 or `fes.application` 1.0.
That limit is unchanged. The
implemented `fes.media.blob-stream` 1.0 transport is a distinct interface, not
a widening of legacy blob 1.0. Stream-enabled SMS supports 1 through 32,768
bytes (32 KiB) on its fixed `0x0000–0x7fff` map. The selected Coleco application
package also supports streamed media up to 32 KiB, mapped at `0x8000–0xffff`.
Coleco images of at most 16 KiB retain the existing mirroring; larger images
return `0xff` beyond the committed length. Both transports use the library
media role `blob`; the role alone does not identify the transport or capacity.
Stream delivery requires verified active stream support and checks the endpoint's
observed capacity separately from the offline declaration and host storage limit.
Neither transport implies arbitrary cartridge or mapper support.

New Coleco entries require explicit media to run a diagnostic;
package-only entries upload nothing. Schema 7 migrates existing Coleco entries
once to the historical diagnostic as ordinary selected media. Clearing that
selection is preserved across restart.

For example, importing a 512 KiB ROM succeeds, but selecting it for blob 1.0
fails before hardware activation and leaves the previous selection unchanged.
See [media capacity and transport](core-media-evolution.md) for the current
storage boundary and implemented versioned stream path. A kit result for an
older sealed package does not accept a later bitstream; those notes are under
[validation/](validation/).

Media bytes and selections live in the host catalog, so back it up alongside
the package store. Where a core supports persistence, settings/progress remain
core-scoped: different titles using that core do not gain separate save slots.
The raw `core-media` development upload remains available, and the parent
target-acceptance runner's `--media` option exercises that diagnostic path,
not selected-media acceptance.

The host retains all versions under `~/.local/share/fogcast/core-packages/`;
back up that directory together with its catalog database. Appliance image
recovery remains independent of these host files. See the selected
[FogCast operator/API guide](../sources/FogCast/docs/core-package-library.md)
for response contracts, persistence and recovery behavior.

## Package and ABI compatibility

`mister-packages` owns the format-2/3 schemas, FES GP ABI and DE10-Nano programming
profiles. `misteross` owns package construction and build provenance. The
package ID hashes the exact manifest and payload bytes, plus the sealed ROM map
for format 3, so changing any member creates a different immutable identity.
Package `version` follows semantic
version syntax and describes the packaged core release; it does not relax ABI
checks.

Inspection accepts an unknown but well-formed ABI so tools can describe it.
Loading requires a runtime-advertised programming profile and ABI major, the
declared required interfaces, and profile-specific observed identity. Minor
versions and optional interfaces are negotiated from the runtime registry.
FogCast consumes that advertised registry generically; it does not carry a
second generated Go allowlist. The runtime C++ and misteross Verilog consumers
are regenerated from the shared FES GP definition.

Installed packages can appear under **FPGA cores** in the ordinary library.
The existing session launch, controller input and Stop paths handle these entries. Parent tests and component tests cover package admission, lifecycle,
input retirement, fixed HDMI setup and image placement. Physical acceptance of
the newly assembled image remains a separate exact-artifact kit operation.

## Component entrypoints

| Concern | Entry point |
| --- | --- |
| Shared schema, ABI and profiles | `sources/mister-packages/packages/` and `cmd/mister-packages` |
| FPGA recipe and package export | `sources/misteross/scripts/build_fes_pong.py` and `scripts/export_core_package.py` |
| Hardware admission and lifecycle | `sources/libmister-runtime/src/native/` and the runtime daemon protocol |
| Host session, transfer and CLI | `sources/FogCast/corepackage`, `sources/FogCast/hostclient`, `internal/misterruntime`, and `internal/fogcastcli` |
| Selection, receipts and assembly | `scripts/bundle.py`, `scripts/build.py`, and `scripts/native_dev.py` |

## Integration evidence

See [core package library validation](validation/2026-09-09-core-package-library.md)
for selected revisions, software checks and the distinction between diagnostic
and exact-image hardware acceptance.

## Operator publication

`config/core-library.toml` curates labels, systems and standing for registered
recipes. `make core-catalog` publishes only supplied verified core-dev receipts;
metadata alone cannot make a core installable. Its version-1 `catalog.json`
contains a stable source namespace and a SHA-256 of the compact sorted-key UTF-8
JSON body excluding `catalog_sha256`. Artifacts carry separate canonical package
and archive identities. Unproduced rows deliberately omit artifact fields.

FogCast optionally reads this local publication for browser installation and
manifest-derived guided ROM setup. See its `docs/core-package-library.md` for
configuration. The factory image's package set is independent of this catalog.
Publication bundles no private ROMs and performs no compiler or kit operation.
