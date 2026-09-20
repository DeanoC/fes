# Installed FPGA packages in the library

FogCast keeps installed `.fcore` archives on the host, independently of the
kit's temporary activation staging and immutable appliance image. Installation
does not program the FPGA. A library entry selects an exact installed package
ID; importing another version leaves that selection unchanged.

Entries can be package-only or select immutable imported media. Selection is
data, not compiled code: changing an installed package or media needs no host
rebuild, image rewrite, or service restart. Host import/storage accepts
1..33,554,432 bytes (32 MiB), using bounded-memory streaming. That is a host
storage policy, not a claim about any core's cartridge size.
The target media role is `blob`: legacy delivery accepts 1..16384 bytes with
`fes.simple-computer` 1.0 and `fes.media.blob` 1.0. Packages declaring both
required blob 1.0 and required `fes.media.blob-stream` 1.0 have an offline safe
guarantee of 1..32768 bytes (32 KiB). Launch also requires the runtime's actual
observed endpoint limits and matching package generation.
Transport is never inferred from a core ID or descriptive system field.
Beyond that guarantee, larger media and new roles need additional support; this is not a retail-media
compatibility claim. Original catalog Pong and standalone FES Pong remain separate.

## Native launcher status

The native fogcast-kit launcher performs a read-only join of the library
entries and installed package inventory. For the selected fes.pong,
fes.zx81, and fes.coleco packages it shows the core ID, selected package
version and a compact state (installed, missing, mismatch, or
incompatible) in the catalog and detail metadata. A failed read clears the
previous status and leaves ordinary catalog browsing available.

This UI status does not claim target compatibility when inventory reports
compatibility:"unknown"; hardware acceptance still requires the explicit
target inspection path described below. The native launcher does not install
packages or change the selected package. Package replacement remains the
explicit core-select/API operation and is outside this read-only UI slice.

## Browser management

Open **Manage FPGA library** on the normal host browser listener. Import a
sealed `.fcore` archive, select an installed version, and check its target
compatibility. Installed inventory is not proof of compatibility; an offline
or busy target remains unknown. Creating a title and replacing its package
still require the existing host's authoritative compatibility checks.

For a media-backed title, upload a ROM/media file explicitly. The panel shows
its immutable digest and size beside the selected package's declared capacity.
The host's 32 MiB import policy is separate from the core's supported media
size. No file extension, core name or successful upload proves a ROM will run.
This panel uploads and selects media; it does not add a media inventory browser.

Create a title with the selected package and optional media, or select an
existing title to change its package or select/clear its media. These operations
affect the next launch, not the running session. A conflicting selection must
be refreshed and explicitly selected again; the browser never retries mutations
automatically.

Requests, including response reads, have a two-minute browser deadline. A
timeout releases the panel controls but does not prove that an upload or
selection was rejected: refresh to confirm the outcome before another explicit
attempt.

The title appears in the ordinary **FPGA cores** catalog. The Kit's existing
catalog refresh (normally every 30 seconds while connected and not busy)
discovers it, and its normal launch/Stop controls use the same
host session API. The management panel does not launch or stop a core, expose
target credentials, or add management routes to the paired Kit listener.
Shipping this panel requires a host software update once; subsequent compatible
package/media changes require neither a host rebuild/restart nor a kit-image
rewrite. New runtime capabilities remain a separate software change.

## Operator commands

Run the host normally, then use its existing API origin:

```sh
fogcast --api http://127.0.0.1:8787 --json core-install /absolute/path/core.fcore
fogcast --api http://127.0.0.1:8787 --json core-list
fogcast --api http://127.0.0.1:8787 --json core-check PACKAGE_ID
fogcast --api http://127.0.0.1:8787 --json core-media-capabilities PACKAGE_ID
fogcast --api http://127.0.0.1:8787 --json core-entry 'Standalone FES Pong' PACKAGE_ID
fogcast --api http://127.0.0.1:8787 --json core-entry 'ZX81' PACKAGE_ID
```

`core-entry` returns the stable `game_id`. The entry appears under **FPGA cores**
in the normal library, where the existing launcher can launch it. The standard
session launch API takes that game ID. Held Select+Start uses the existing
package session lifecycle.

Browse package-backed titles through the ordinary library query, including
the existing library-user favorites overlay (use the host's configured access
credentials when required):

```sh
curl 'http://127.0.0.1:8787/api/v1/games?platform=fpga&sort=title&limit=50'
curl 'http://127.0.0.1:8787/api/v1/games?platform=fpga&collection=favorites&limit=50'
```

These queries return library game IDs, not package digests. Use
`GET /api/v1/library/core-entries/{game_id}` for the selected package/media;
favorites and play history remain tied to the stable game ID.

Standalone FES ZX81 is a ROM-less `fes.simple-computer` 1.0 package
(`fes.zx81` 1.0.0). Required interfaces are `fes.keyboard`,
`fes.media.blob` and `fes.video.fixed-720p60`. It has no `fes.gamepad` and
no persistence layout, so library launches are volatile and there is no
paddle-speed settings API. Remote input attaches on `fes.keyboard`; the
target agent posts the 40-bit matrix through runtime `set_keyboard`. A
package-only library launch starts BASIC (empty `LOAD ""` reports `0/0`).
A selected bounded `.p` blob is delivered through the existing runtime
`load_media` path as part of the host session launch.
`core-load` remains the development loader and does not create this entry.

Standalone FES ColecoVision is also a ROM-less `fes.simple-computer` package
(`fes.coleco`). Existing entries migrate once to an ordinary selected 2,299-byte
controller diagnostic through the existing development-media path. New entries
require explicit media selection; a package-only entry uploads no media. The asset
was generated by the MIT-licensed misteross emitter at revision
`1308f94d62061d308a756d5f197d14e81cb4cf5c`:

```sh
python3 cores/fes-coleco/diagnostic/generate.py --controllers \
  --output controller.rom
```

The exact media SHA-256 is
`ef9443c2787cd02b6d78d233d015b0bbf3fb21d53d1a5890497cdbb7897f053c`.
The embedded bytes and license are retained under
`catalog/seeds/coleco-controllers.{hex,LICENSE}` for migration. The diagnostic
continuously displays both players' joystick and keypad bytes, making
directions, Fire 1, and Fire 2 visible through the existing keyboard transport.
It is a controls diagnostic, not a retail-game compatibility claim. The
explicit `core-media` operation remains available for development uploads.
No default ZX81 tape is selected.

Import bytes, then create an entry or change an existing entry:

```sh
fogcast --api http://127.0.0.1:8787 --json core-media-install /absolute/path/controller.rom
fogcast --api http://127.0.0.1:8787 --json core-entry 'Coleco controls' PACKAGE_ID blob MEDIA_ID
fogcast --api http://127.0.0.1:8787 --json core-media-select GAME_ID PACKAGE_ID none MEDIA_ID
fogcast --api http://127.0.0.1:8787 --json core-media-select GAME_ID PACKAGE_ID OLD_MEDIA_ID NEW_MEDIA_ID
fogcast --api http://127.0.0.1:8787 --json core-media-select GAME_ID PACKAGE_ID OLD_MEDIA_ID none
```

`core-media-install` returns `media_id` (SHA-256) and `size`. It streams a
bounded regular file into a private snapshot; changing that file later does not
change stored bytes. CLI upload and catalog storage use bounded buffers rather
than loading the whole file into memory.
Import a changed file to get its new ID, then select it explicitly. `none`
means no media in the checked CLI selection. Media selection validates the
installed descriptor and stored bytes locally, including while the target is
offline. Launch revalidates the target's actual active interfaces and sends the
selected snapshot. Selection never rewrites an active session.
An optional blob declaration is eligible for selection, not a promise that
every target activates it; missing active support fails launch with cleanup.

`core-media-capabilities PACKAGE_ID` works offline and returns:

```json
{
  "package_id": "<selected package digest>",
  "source": "declared-contract",
  "compatibility": "unknown",
  "import_max_bytes": 33554432,
  "media": [{
    "role": "blob",
    "format": "raw",
    "min_bytes": 1,
    "max_bytes": 16384,
    "interface": {"id": "fes.media.blob", "major": 1, "minor": 0},
    "transport": "fes-simple-computer-mailbox-v1"
  }]
}
```

The response describes the host's supported interpretation of the installed
package declaration, not negotiated or measured hardware capacity. A package
with no supported media contract returns `media:[]`. No name-based core
allowlist or mapper inference is used.

A 512 KiB ROM can be imported and retained, but selecting it for an existing
16 KiB core fails before activation and preserves the current selection.
Larger core delivery needs a separately implemented, versioned transport and
an advertised capacity; increasing host storage policy does not enable it.

For a package declaring both required blob interfaces, the offline capability
query instead reports `max_bytes:32768`, interface
`{"id":"fes.media.blob-stream","major":1,"minor":0}` and transport
`fes-simple-computer-mailbox-stream-v1`. `compatibility` remains `unknown`:
this is the declared safe guarantee, not a live endpoint query or mapper promise.
For example, import a permitted 32 KiB image and bind it explicitly:

```sh
fogcast --api http://127.0.0.1:8787 --json core-media-capabilities PACKAGE_ID
fogcast --api http://127.0.0.1:8787 --json core-media-install /absolute/path/image.sms
fogcast --api http://127.0.0.1:8787 --json core-entry 'SMS test title' PACKAGE_ID blob MEDIA_ID
```

Use the returned `game_id` with the normal session launch API. The host selects
the explicit target `/v1/development/media-stream` operation, not a widened
legacy endpoint. The runtime observation must report min=1, max in
32768..33554432, and chunk=512; missing support rejects launch with cleanup.
Host delivery remains capped at 32768 even when the endpoint advertises more.
The raw development command `core-media PATH` still accepts only 1..16384
bytes; use `core-media-install` and a library entry for stream delivery.
Software tests and serializer fixtures do not establish SMS hardware acceptance.

To select another installed version or return to a retained version:

```sh
fogcast --api http://127.0.0.1:8787 --json core-select GAME_ID CURRENT_PACKAGE_ID NEXT_PACKAGE_ID
```

This is a checked update: the expected current ID must still match, and the
replacement must belong to the same core and pass target compatibility
inspection. It changes the next launch, not the currently running FPGA.
The game ID, favorites and play history remain stable. Multiple titles may use
one core/package; the same core/title pair returns a conflict. Package changes
preserve selected media and must support its role.

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
| `POST /api/v1/core-media` | Bounded `application/octet-stream` bytes; `{media_id,size}` (201 new, 200 already stored) |
| `GET /api/v1/core-media/{id}` | Verified `{media_id,size}`, no path or bytes |
| `POST /api/v1/core-packages/{id}/compatibility` | Empty body; `{package_id,descriptor,compatible,compatibility_error,target,target_id,state}` |
| `GET /api/v1/core-packages/{id}/media-capabilities` | Offline declared-contract projection with separate host import policy and supported role limits |
| `POST /api/v1/library/core-entries` | `{"title":"Standalone FES Pong","package_id":"..."}`, optionally `media_role:"blob",media_id:"..."`; returns entry |
| `GET /api/v1/library/core-entries` | `{entries:[...]}` |
| `GET /api/v1/library/core-entries/{game_id}` | `{game_id,title,core_id,package_id}`, plus `media_role,media_id` when selected |
| `PUT /api/v1/library/core-entries/{game_id}` | `{"package_id":"...","expected_package_id":"..."}`; returns entry |
| `PUT /api/v1/library/core-entries/{game_id}/media` | `{"expected_package_id":"...","expected_media_id":"","media_role":"blob","media_id":"..."}`; both expected fields required; empty new role/ID clears |
| `POST /api/v1/session/launch` | Existing `{"game_id":"..."}` request |
| `POST /api/v1/session/stop` | Existing Stop operation |

Inventory is local and works offline. Its compatibility is deliberately
`unknown`: use explicit inspection to obtain a current target-specific result.
A valid incompatible inspection returns HTTP 200 with `compatible:false`,
`state:"incompatible"` and a structured compatibility error. An unreachable
or busy target is an observation error, not a compatible result. Entry creation
and package selection require a successful compatible observation. Actual launch
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

Selections and media bytes live in the existing catalog database. Schema 8
adds 64 KiB chunk rows for new imports while retaining existing inline objects,
digests, entries and history. The migration does not rewrite package archives.
Every media read rechecks its size and digest; corrupt objects are refused,
never silently replaced. Upload staging and verified read snapshots are private
temporary files, removed on failure or close; they are not a second persistent
store. Reserved package libraries
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

Persistence remains core-scoped, not title-scoped: entries using the same core
share its settings/progress namespace. Media selection does not create separate
save slots.

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
