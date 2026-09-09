# Installed FPGA packages and library launch

Status: implementing the approved package-library milestone.

## Outcome

An operator imports a `.fcore` once, creates a ROM-less library entry selecting
that immutable package, and launches it through the existing library session
API. A second package version can be installed without changing the entry.
Explicit selection changes affect the next launch; selecting an earlier
installed package provides rollback. Incompatible selections leave the prior
selection and any active session intact.

The acceptance example is standalone FES Pong: import, add to the library,
launch, play, return to a usable menu, install a second version, select it, then
select the original again. Existing catalog Pong and cartridge games retain
their separate identities and behavior.

## Ownership and scope

FogCast owns persistent host installation, catalog selection, public APIs and
session coordination. The target agent exposes read-only compatibility
inspection using the existing native runtime inspector. libmister-runtime
continues to own authoritative admission, programming, identity confirmation,
input capabilities and recovery. FES owns component selection, integrated
builds and exact-artifact evidence. No new package format, ABI, compiler recipe
or FPGA design is required.

The UI team owns package-management presentation. This work supplies documented
APIs and CLI operations, and makes package entries visible through existing
library queries and launch requests. Only adapter changes essential for those
existing flows belong here; no layout, theme or graphics changes.

This slice covers ROM-less entries only. The manifest's optional `core.system`
is descriptive metadata, not a declaration of cartridge transport. Do not infer
ROM paths, save formats or media roles from it. Remote registries, automatic
updates, dependency resolution, package deletion/garbage collection, signatures,
and the menu FPGA accelerator are separate work.

## Approach

Use the current host catalog plus a host-owned content-addressed archive store.
This retains installed versions independently of the target's temporary
activation staging and allows the same library to address a replacement kit.
A target-only installation database would duplicate ownership and disappear
with the card. Baking each selection into an appliance image would require an
image update for ordinary library changes. Neither is needed for this slice.

### Immutable installation

The default store is `~/.local/share/fogcast/core-packages/`, with a Paths
field allowing isolated tests and operator deployments. Each installed archive
is named by its existing content-derived package ID, not the uploaded filename,
core name or semantic version. Use the existing closed format-2 parser.

Import accepts a bounded binary upload. Copy to a private temporary file,
validate the complete archive and derive its descriptor/ID from those bytes,
then sync and publish atomically. Cancellation, truncation and invalid content
must not publish a package. Concurrent/repeated imports of identical bytes are
idempotent. An existing ID with corrupt bytes is an error, not an overwrite.
Do not follow symlinks or expose server filesystem paths in the public API.

Installed descriptors are derived from authenticated archive bytes; metadata
alone cannot authorize activation. Revalidate the opened archive before use,
and send the same validated bytes. Package size is already bounded by the
format-2 archive limit. Incomplete temporary files are not inventory entries.
All installed versions remain available across host restart and appliance update.

Inventory includes package ID, core ID/name/description/version, target,
ABI/interfaces, and selected library references. Semantic versions are labels;
there is no implicit `latest` selection and no requirement that core ID plus
version be globally unique. The package ID resolves ambiguity.

### Compatibility

Installation is allowed while the target is offline and can retain a valid
unsupported package for inspection. Inventory availability is separate from
compatibility: report `unknown`, `compatible` or `incompatible`, with structured
reason and the target identity used for the observation. Never treat an offline
or unsupported backend as compatible.

Explicit compatibility inspection uploads the selected installed archive to
a read-only target operation. The native adapter privately stages it, invokes
existing runtime `inspect_core`, verifies the returned identity and descriptor,
and cleans up. It must not call LoadCore, Stop, an input replacement barrier,
or acquire/steal a physical session lease. Authentication and bounded body/
timeout rules follow the existing target API. Inspection serializes with target
transitions and may return busy without changing published active state. Cleanup
failure fails the observation and cannot report compatible. The Main backend reports the
operation unsupported. Runtime ABI/profile/interface rules remain authoritative;
there is no second host allowlist.

Compatibility is advisory until actual activation: a target can change between
inspection and launch. Actual LoadCore repeats authoritative admission. Failure
to inspect or incompatible results prevent changing an entry's selection.

### Durable library selection

Store package-entry selections in the existing catalog database, transactionally
with their ordinary game rows. Use a distinct source kind `core_package` and
an explicit package-ID association; do not encode package paths as ROM paths or
fabricate ROM hashes. Package rows belong to reserved logical libraries that
filesystem scans never traverse or mark missing. Use a dedicated `FPGA cores`
platform grouping so optional descriptive system metadata cannot accidentally
route a package through the cartridge loader.

There is one durable entry per core ID. An entry has a stable game ID and title,
a core ID and an explicitly selected
package ID. Create an entry only after successful compatibility inspection.
Replacement must be an installed package for that same core ID. Preserve the
entry's stable identity, favorites and play history across replacements. Give
each entry its own group key so ordinary grouped queries cannot collapse it.

Selection writes include the expected current package ID (empty on create).
The catalog transaction compares that value before updating; stale concurrent
requests return a conflict. Incompatible packages, missing archives, invalid
IDs, persistence failures and conflicts leave the existing selection unchanged.
Selection affects future launches; it neither replaces nor relabels an active
FPGA generation. An active session retains the package ID actually loaded.
Reverting is the same checked selection operation with a retained prior ID;
there is no separate implicit rollback mechanism.

### Normal session launch

`POST /api/v1/session/launch` continues to accept a game ID. Resolve the package
selection inside the same lifecycle boundary as launch. Package entries use
the existing LoadCore admission and post-mutation recovery path; extract shared
helpers where necessary instead of nesting lifecycle locks or introducing an
independent coordinator.

The existing development-core path preserves input/media through pre-mutation
rejection. The normal launch coordinator currently detaches input before
launch, so package-backed library launch needs an explicit branch through that
proven package transition behavior. Invalid archives and compatibility rejection
must preserve the active game, input source and media owner. After confirmed
activation, retire the previous host ownership and attach input only when the
observed package advertises gamepad capability.

Publish the library game identity together with the active package identity and
generation. Preserve the native target's honest package status; do not pretend
it loaded a cartridge. Existing status polling, launcher reconnect, Stop and
Select+Start must work for the library entry. A raw development package retains
its development semantics. After host restart, reconcile active package
identity without inventing the origin of a prior launch: the same package may
have been loaded through a library entry or the development API. Leave the
game association unknown until a new explicit library launch; the active
package remains stoppable through the existing path.

## Public API contract

Use the existing host API access controls and canonical error envelope; target
requests retain bearer authentication.

| Operation | Host route | Semantics |
| --- | --- | --- |
| Import | `POST /api/v1/core-packages` | Bounded `application/octet-stream`; return installed descriptor and package ID |
| Inventory | `GET /api/v1/core-packages` | Deterministic installed list and entry references; no hardware mutation |
| Inspect installed | `GET /api/v1/core-packages/{package_id}` | Validated metadata; no arbitrary host path |
| Compatibility | `POST /api/v1/core-packages/{package_id}/compatibility` | Explicit read-only target inspection and target identity |
| Create entry | `POST /api/v1/library/core-entries` | Title and package ID; validate compatibility before catalog publication |
| Read selection | `GET /api/v1/library/core-entries/{game_id}` | Stable entry identity and selected package |
| Change selection | `PUT /api/v1/library/core-entries/{game_id}` | Package ID plus expected current ID; checked atomic update |
| Launch/Stop | Existing `/api/v1/session/launch` and `/stop` | Existing session/input lifecycle |

Import returns 201 for a new archive and 200 for an existing identical archive.
Malformed requests return 400; missing IDs return 404; stale selection returns
409; incompatible selection returns a structured compatibility error; offline
inspection returns unavailable. Compatibility responses distinguish a valid
incompatible result from failed observation. Do not return raw private paths,
tokens or internal errors. Publish exact JSON examples and CLI equivalents in
the component guide alongside implementation.

Extend the existing CLI with import/list/compatibility and entry create/select
operations calling these host routes. Keep `core-inspect` local and `core-load`
as the explicit development operations. Existing UI consumers see the entries
through normal library listing, search and platform queries; add package/source
metadata without changing existing fields' meanings.

## Validation

1. Storage tests: real archives, duplicate/concurrent import, truncated or
   corrupt content, symlink/path rejection, cancellation, restart persistence,
   and unchanged previous publications on failure.
2. Catalog tests: upgrade an existing database, create/select/query/search,
   scanner preservation, same-core restriction, stale writes, stable favorites/
   identity and reopening the database after selection.
3. Target inspection tests: read-only even with an active session, unknown ABI,
   offline/unsupported backend, exact descriptor/identity matching, bounded
   uploads and cleanup on success/error/cancellation.
4. Service/API tests: use the actual store/catalog and session coordinator.
   Check import without activation, selection without activation, failed
   compatibility preserving selection, successful library launch/input/Stop,
   pre-mutation rejection preserving active input/media, and repeated recovery.
5. Run affected Go suites, race checks for changed shared state and vet;
   independent integration review before publication. Run parent consistency
   and host build once the selected component revision is available.
6. One kit operator uses the existing lease. First run bounded diagnostics with
   original Pong bytes. Import a second valid package version with changed
   manifest metadata but the same RBF/build ID; this proves version selection
   without another FPGA build. Verify both selected IDs and return to menu.
   Test incompatible replacement preserving selection and active input.
7. Build and verify one stabilized integrated image if target/image inputs
   change. Record its exact identity separately from diagnostic binaries and
   prior physical confirmations. Restore ordinary pairing and release the lease.

## Delivery boundaries

Work in `/home/deano/fes/out/dev/core-package-library/FogCast`, based on merged
FogCast `e5fe84e8f4e63667069629d6afbd057f54d4ee2b`. Parent work is isolated in
`/home/deano/fes/out/dev/rbf-abi/fes` on `feat/core-package-library`, based on
merged FES `86280c4926b5a29df1f1572071176e141654a960`.

Component workers receive bounded storage/catalog, target inspection, and
host/session tasks only after the common interfaces are fixed. The integrator
owns shared API types, parent pins and kit operation. Preserve concurrent UI
branches. No compiler, Linux or FPGA rebuild is necessary during the host
storage/catalog test loop.


## Confirmed integration points

- `internal/corepackage/package.go`: reuse bounded archive validation and
  identity derivation; persistent host archives must have separate ownership
  from disposable target `Staged` publications.
- `catalog/schema.go`, `catalog/builtin.go`, `catalog/store.go`: add durable
  associations and reserved logical-library handling. `Libraries()` currently
  excludes only built-in Pong; extend that protection for package entries so
  startup reconciliation does not retire them.
- `fogcast/service.go`: `SessionExecution`, `Launch`, `LoadCore` and status
  reconciliation own the service transition. Factor shared package loading
  under an already-held lifecycle/target boundary instead of recursively
  calling a locking public method.
- `internal/hostapi/session.go`: the normal `launch` path detaches input before
  calling the service; `loadDevelopmentCore` instead preserves pre-mutation
  ownership. Reuse the latter transition rules for package-backed entries.
- `internal/misterruntime/runtime.go`: the first part of `LoadCoreOwned`
  already stages and invokes `InspectCore` before a replacement barrier.
  Extract a read-only path that cannot fall through to programming.
- `internal/httpapi/development.go` and `host/core_package_client.go`: extend
  the target inspection transport using bounded authenticated requests.
- `catalog/platforms.go` and the existing host catalog projection: expose a
  browse-only platform without registering a fictitious cartridge transport.

Baseline on the new FogCast worktree: `go test ./catalog ./internal/corepackage`
passed. This baseline preceded implementation. Kit acceptance remains a separate
checkpoint after software integration.
