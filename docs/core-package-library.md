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
fogcast --api http://127.0.0.1:8787 --json core-entry 'ZX81' PACKAGE_ID
```

`core-entry` returns the stable `game_id`. The entry appears under **FPGA cores**
in the normal library, where the existing launcher can launch it. The standard
session launch API takes that game ID. Held Select+Start uses the existing
package session lifecycle.

Standalone FES ZX81 is a ROM-less `fes.simple-computer` 1.0 package
(`fes.zx81` 1.0.0). Required interfaces are `fes.keyboard`,
`fes.media.blob` and `fes.video.fixed-720p60`. It has no `fes.gamepad` and
no persistence layout, so library launches are volatile and there is no
paddle-speed settings API. Remote input attaches on `fes.keyboard`; the
target agent posts the 40-bit matrix through runtime `set_keyboard`. A
library launch starts BASIC (empty `LOAD ""` reports `0/0`). A `.p` blob is
still delivered with runtime `load_media`, not the host session API.
`core-load` remains the development loader and does not create this entry.

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

## Persistent settings and progress

Library launches carry an explicit library context through the authenticated
agent to `load_library_core`. The runtime resolves the core namespace and layout
from the admitted package. Development `core-load` and raw RBF loads remain
volatile, including when a development package declares the same core ID.
`core_package.persistence_mode` reports `persistent` or `volatile`; the package
ID and generation continue to identify the actual running artifact.

Standalone FES Pong's `fes.persistence.words` 1.0 and `fes.pong.progress` 1.0
interfaces store a paddle-speed enum and a best rally. Speed is numeric: 0 slow
(2 pixels/frame), 1 normal (4), 2 fast (6). Best rally counts player paddle
returns and saturates at 65535. Defaults are speed 1 and best rally 0. This is
small game data: a new launch starts a fresh match, rather than restoring ball
position or game scores.

```sh
fogcast --api http://127.0.0.1:8787 --json core-settings GAME_ID
fogcast --api http://127.0.0.1:8787 --json core-progress GAME_ID
fogcast --api http://127.0.0.1:8787 --json core-settings-set GAME_ID PACKAGE_ID REVISION 2
```

| Request | Body / result |
| --- | --- |
| `GET /api/v1/library/core-entries/{game_id}/settings` | Empty body; durable core data |
| `GET /api/v1/library/core-entries/{game_id}/progress` | Empty body; durable core data |
| `PUT /api/v1/library/core-entries/{game_id}/settings` | `{"expected_package_id":"<64 lowercase hex>","expected_revision":"absent or record digest","paddle_speed":2}`; updated durable core data |

Both reads return the same complete data observation, including the exact
admitted descriptor:

```json
{
  "target": "dev",
  "target_id": "<target identity>",
  "package_id": "<selected package ID>",
  "core_id": "fes.pong",
  "descriptor": { "...": "complete admitted format-2 descriptor" },
  "layout": { "id": "fes.pong.progress", "major": 1, "minor": 0 },
  "mode": "persistent",
  "revision": "absent",
  "paddle_speed": 1,
  "best_rally": 0
}
```

These are durable values, not a live score feed. A missing record reports
`revision:"absent"`; a published record reports lowercase SHA-256 of the whole
canonical record. PUT requires both the selected package ID and that revision.
It changes only the setting, preserves progress, and applies on the next launch.
A stale selection or revision must be refreshed before retrying. Updates to an
active namespace and updates while recovery is pending are refused. A settings
write uses the existing kit lease; a successful settings-only operation or a
definite admission/revision rejection releases its new grant. Existing session
grants and ambiguous writes retain ownership.
Read-only inspection never claims the kit.

Every inactive data request sends the exact selected archive through bounded
private target staging. The target accepts no storage paths. Its three bounded
binary POST routes are `/v1/library/core/load`,
`/v1/library/core/data/inspect`, and `/v1/library/core/settings`. Each requires
`X-FogCast-Package-ID`; settings also requires `X-FogCast-Expected-Revision` and
`X-FogCast-Paddle-Speed`. Load and settings require the current kit lease.
Staging is cleaned after inspection/write; a cleanup failure is an error even
if the data write completed. A lost mutation response is never replayed.

Selection checks the current target's record and active namespace and compares
runtime-advertised `persistence_layout` metadata. A volatile version can upgrade
to a persistent version; a persistent version cannot select a different or
missing layout, even before its first save. Identical layouts permit upgrades
and rollbacks. Offline state remains unknown. Launch repeats these checks and
refreshes incoming data after flushing the outgoing generation, including a
same-core replacement.

Data lives on the kit under `/media/fat/fogcast/core-data/`, in
`<SHA-256(core.id)>/record.bin`. The production agent creates this fixed root;
the runtime validates filesystem access and owns canonical record decoding and
atomic publication. Package staging, package-version changes, and appliance
image replacement do not remove it. SNES cartridge saves retain their existing
namespace and format. There is no reset, migration, deletion, import, periodic
checkpoint, host synchronization, or backup API in this slice.

| Error | Meaning / next action |
| --- | --- |
| `UNSUPPORTED_OPERATION` (400) | Selected core/runtime has no supported persistence contract |
| `MISTER_UNAVAILABLE` (503) | Target data state is unknown; reconnect and inspect |
| `BUSY` (409) | Another owner/transition, active namespace, or pending recovery; finish the existing session |
| `STALE_REVISION` (409) | Selected package or durable revision changed; GET before a new explicit PUT |
| `INCOMPATIBLE_DATA` (409) | Stored/active/selected layout conflicts; no automatic migration |
| `CORRUPT_DATA` (409) | Record failed bounded validation; it is never replaced with defaults |
| `SAVE_FAILED` (500) | Publication failed; Stop did not succeed; inspect status and explicitly retry |
| `INTERNAL` with recovery phase (500) | Staging or lease cleanup failed; do not assume the operation fully completed |

A successful Stop or replacement is the durability boundary. A save failure
keeps the same generation active only after the runtime resumes gameplay and
restores its native input. The host reattaches input only for that exact prior
package and generation. Unsafe recovery retains the package and generation in
failed status, with input blocked and ownership retained. Sudden power loss can
lose progress since the last successful boundary, while preserving a complete
previous record.
