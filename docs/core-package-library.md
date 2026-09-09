# Installed FPGA packages in the library

FogCast keeps installed `.fcore` archives on the host, independently of the
kit's temporary activation staging and immutable appliance image. Installation
does not program the FPGA. A library entry selects an exact installed package
ID; importing another version leaves that selection unchanged.

This path supports ROM-less cores. It does not infer cartridge transport from
a package's optional descriptive system field. Original catalog Pong and
standalone FES Pong remain separate products.

## Operator commands

Run the host normally, then use its existing API origin:

```sh
fogcast --api http://127.0.0.1:8787 --json core-install /absolute/path/core.fcore
fogcast --api http://127.0.0.1:8787 --json core-list
fogcast --api http://127.0.0.1:8787 --json core-check PACKAGE_ID
fogcast --api http://127.0.0.1:8787 --json core-entry 'Standalone FES Pong' PACKAGE_ID
```

`core-entry` returns the stable `game_id`. The entry appears under **FPGA cores**
in the normal library, where the existing launcher can launch it. The standard
session launch API takes that game ID; controller input and held Select+Start
use the existing package session lifecycle.

To select another installed version or return to a retained version:

```sh
fogcast --api http://127.0.0.1:8787 --json core-select GAME_ID CURRENT_PACKAGE_ID NEXT_PACKAGE_ID
```

This is a checked update: the expected current ID must still match, and the
replacement must belong to the same core and pass target compatibility
inspection. It changes the next launch, not the currently running FPGA.
The game ID, favorites and play history remain stable. There is one entry per
core ID; creating the same entry again returns a conflict.

`core-inspect PATH` remains a local inspection command; `core-load PATH` remains
the explicit development loader. Neither creates a library entry.

## API for clients

All operations use the ordinary host API access controls. Target inspection
uses the paired target credential internally. JSON requests reject unknown
fields. No API response exposes the archive's private filesystem location.

| Request | Body / result |
| --- | --- |
| `POST /api/v1/core-packages` | Bounded `application/octet-stream` archive; returns `{package_id,descriptor}` (201 new, 200 already installed) |
| `GET /api/v1/core-packages` | `{packages:[{package_id,descriptor,entries,compatibility:"unknown"}]}` |
| `GET /api/v1/core-packages/{id}` | Validated `{package_id,descriptor}` |
| `POST /api/v1/core-packages/{id}/compatibility` | Empty body; `{package_id,descriptor,compatible,compatibility_error,target,target_id,state}` |
| `POST /api/v1/library/core-entries` | `{"title":"Standalone FES Pong","package_id":"..."}`; returns entry |
| `GET /api/v1/library/core-entries` | `{entries:[...]}` |
| `GET /api/v1/library/core-entries/{game_id}` | `{game_id,title,core_id,package_id}` |
| `PUT /api/v1/library/core-entries/{game_id}` | `{"package_id":"...","expected_package_id":"..."}`; returns entry |
| `POST /api/v1/session/launch` | Existing `{"game_id":"..."}` request |
| `POST /api/v1/session/stop` | Existing Stop operation |

Inventory is local and works offline. Its compatibility is deliberately
`unknown`: use explicit inspection to obtain a current target-specific result.
A valid incompatible inspection returns HTTP 200 with `compatible:false`,
`state:"incompatible"` and a structured compatibility error. An unreachable
or busy target is an observation error, not a compatible result. Entry creation
and selection require a successful compatible observation. Actual launch
revalidates admission because the runtime may change afterward.

Selection conflicts return HTTP 409. Missing packages/entries return 404.
Malformed input and unsupported selections use the existing canonical error
envelope. Package admission errors preserve the prior active input/media
session; a failure after hardware mutation follows the existing runtime
recovery and truthful session status rules.

## Storage and recovery

The default archive root is `~/.local/share/fogcast/core-packages/`. Embedded
host compositions can set `fogcast.Paths.CorePackages` to an isolated absolute
root. Archives are content-addressed, validated before atomic publication and
revalidated from the same bytes sent for activation. Partial imports never
become inventory entries. An installed ID is never silently overwritten.

Selections live in the existing catalog database. Reserved package libraries
are excluded from ROM-library retirement and scans. Back up both the catalog
and the package directory. Appliance image updates do not change these host
files. This milestone retains all installed versions; removal and automatic
update policies are separate work.

A running package's status retains the package ID and generation actually
loaded even if its library selection changes. After a host restart, an already
running package remains visible and stoppable, but its originating library
entry is unknown until a new explicit library launch. A matching package ID
alone cannot prove whether it was started through the library or development
API.
