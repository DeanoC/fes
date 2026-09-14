# Described FPGA core packages

For persistent Pong settings and best rally, see [core persistence](core-persistence.md).

The recipe registry supports the described `fes.pong`, `fes.zx81` and
`fes.coleco` packages. The default target-image selector installs the ordered
closed package set, while focused profiles may select a smaller package set.
See [FES ZX81](fes-zx81.md) and the Coleco validation records for bring-up
notes.

The default `native-integration-dev` profile installs the locked idle RBF and
the ordered `fes.pong`, `fes.zx81` and `fes.coleco` format-2 package set.
Format-1 catalog cores are not built or installed by this FES production path;
the historical format-1 lanes remain separate from the package-only contract.
Quartus is reserved for those explicit historical format-1 profiles or a
documented bring-up/oracle check; the package-only route does not invoke it.
Each package can be installed on the host and given an explicit library entry;
the package route does not infer ROM or media inputs or replace the existing
catalog system.

## Build and inspect

FES resolves every selected package from its misteross recipe before an image
or development-cache reuse decision. It authenticates the clean misteross
source, the pinned OSS tools and each canonical build-input record. A cached
package is reused only when its matching build-input bytes, manifest, payload,
build identity and provenance all validate. A miss runs that recipe once and
the result goes through the same checks.

The default FES package producers use the authenticated HIP/nextpnr route.
The parent default path opts all selected FES package producers into the
workspace-local shared compiler cache at `out/cache/misteross-toolchains`.
Shared mode verifies the selected misteross checkout and lock pins, then
authenticates Yosys, nextpnr-mistral and Mistral from the matching published
cache slot rather than from `build/toolchain` in that checkout. An old package
record still cannot prove which tools are selected now. A new checkout can
prepare and check the shared slot with:

```sh
make dev
# If this reports missing authenticated FES package build inputs:
revision=$(git rev-parse :sources/misteross)
make -C "out/work/misteross-$revision" \
  toolchain-fes CACHE_ROOT="$PWD/out/cache/misteross-toolchains"
make -C "out/work/misteross-$revision" \
  doctor-strict CACHE_ROOT="$PWD/out/cache/misteross-toolchains"
make dev
```

`make toolchain` may compile the pinned tools into that shared slot and is
intentionally separate from ordinary parent tests. `make host` remains
independent of package and FPGA tool authentication. `make check` validates
the shared ABI and programming definitions, their three real generated
consumers, and all shared fixture copies without running synthesis.

The parent publishes and receipts these external image inputs:

```text
fes-pong.package-selection.toml
fes-zx81.package-selection.toml
fes-coleco.package-selection.toml
core-packages/<package-id>/manifest.toml
core-packages/<package-id>/core.rbf
```

The installed directory is
`/usr/share/mister-runtime/core-packages/<package-id>/`. The child image builder
revalidates the package during fetch, install and image verification and records
the selection in its installed build inputs. Cold builds compare the selection
from both independent passes. Development and cold receipts include the exact
selection, manifest and payload hashes; a metadata-only manifest change
invalidates image reuse even when the RBF bytes do not change.

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
Format-1 catalog bundles and `NATIVE_RUNTIME_SYSTEMS` remain available only
through the explicitly selected historical image path.

## Install and select a library package

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

The host retains all versions under `~/.local/share/fogcast/core-packages/`;
back up that directory together with its catalog database. Appliance image
recovery remains independent of these host files. See the selected
[FogCast operator/API guide](../sources/FogCast/docs/core-package-library.md)
for response contracts, persistence and recovery behavior.

## Package and ABI compatibility

`mister-packages` owns the format-2 schema, FES GP ABI and DE10-Nano programming
profiles. `misteross` owns package construction and build provenance. The
package ID hashes the exact canonical manifest and payload, so changing either
creates a different immutable identity. Package `version` follows semantic
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
| Host session, transfer and CLI | `sources/FogCast/internal/corepackage`, `internal/misterruntime`, and `internal/fogcastcli` |
| Selection, receipts and assembly | `scripts/bundle.py`, `scripts/build.py`, and `scripts/native_dev.py` |

## Integration evidence

See [core package library validation](validation/2026-09-09-core-package-library.md)
for selected revisions, software checks and the distinction between diagnostic
and exact-image hardware acceptance.
