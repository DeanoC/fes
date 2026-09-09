# Core settings and progress

Standalone FES Pong packages with the persistence interfaces store paddle speed
and best rally on the kit. A successful Stop saves the complete record. The next
launch starts a new game with settings and progress restored, rather than the
previous ball position. Best rally counts player-paddle returns in one rally,
including an unfinished rally when you exit.

See the [validation record](validation/2026-09-09-core-persistence.md) for tested
artifacts and hardware acceptance status. An older image's validation does not
cover these changes.

## Use it

Install and select the described package through the [package library](core-packages.md).
Use the stable library game ID returned when creating its entry:

```sh
./out/native-integration-dev/fogcast core-settings GAME_ID
./out/native-integration-dev/fogcast core-progress GAME_ID
./out/native-integration-dev/fogcast core-settings-set GAME_ID PACKAGE_ID REVISION 2
```

Speed is 0 slow, 1 normal (default), or 2 fast. Copy `package_id` and `revision`
from the settings response into the update; a missing record has revision
`absent`. Updates reject stale revisions, preserve best rally and apply to the
next launch. Stop an active session for that core before editing. The CLI uses
the existing host API/configuration conventions.

Launch through the library, play, then Stop or hold Select+Start to return to the
launcher. GET reports durable data, not the active score. Sudden power loss can
lose progress since the last successful save boundary. Development package loads
remain volatile and neither read nor overwrite library progress. Raw RBFs keep
their MiSTer interpretation. Existing [SNES saves](snes-saves.md) are unchanged.

## Identity and version changes

Each validated `core.id` has one profile on the kit at
`/media/fat/fogcast/core-data/<sha256-of-core-id>/record.bin`, separate from
package staging, ROM caches and the replaceable Linux image. Package hashes,
versions and display names do not name save files. Different kits have separate
data; synchronization is not included.

Versions with the same data contract share progress. Selecting a version that
removes persistence or changes its layout is rejected, even before an active
core's first save. Installation alone does not select a version. There is no
implicit migration or reset during rollback.

Missing data starts with defaults. Corrupt or incompatible data blocks launch
and is never silently replaced. Back up this directory separately from system
images while the core is stopped. Core IDs are operator-trusted namespaces,
not authenticated publisher identities.

## Errors and retry

A failed save is not successful Stop. When safe, the runtime resumes the same
game and input so you can fix storage and retry. Unsafe resume reports recovery
and retains ownership and the available snapshot. Do not delete data or force
release merely to hide a save error.

Atomic publication leaves an old or complete new record, never a partial one.
A sync failure after replacement can leave uncertain durability and is reported
as an error. Startup, fault cleanup and failed launch do not save unverified
gameplay state. Periodic checkpoints and host backup are separate future work.

## API for the UI team

| Operation | Host route |
| --- | --- |
| Read saved settings | `GET /api/v1/library/core-entries/{game_id}/settings` |
| Read saved progress | `GET /api/v1/library/core-entries/{game_id}/progress` |
| Change next-launch settings | `PUT /api/v1/library/core-entries/{game_id}/settings` |

PUT accepts `expected_package_id`, `expected_revision` and numeric `paddle_speed`.
Responses identify target, package, core, data layout, mode and durable revision,
plus speed and best rally. The selected target must be available. Each operation
admits exact archive bytes; the UI never supplies target filesystem paths.
See the [component API guide](../sources/FogCast/docs/core-package-library.md).

## Ownership and extension

mister-packages defines the required `fes.persistence.words` and
`fes.pong.progress` 1.0 interfaces and fixtures. misteross supplies Pong's mailbox
and events; libmister-runtime validates, restores, captures and publishes data.
FogCast supplies library context, leases and APIs. FES selects matching commits
and records acceptance. Another data layout needs an explicit contract and
runtime support, not merely a metadata declaration.

The [approved design](superpowers/specs/2026-09-09-core-persistence-design.md)
and [implementation plan](superpowers/plans/2026-09-09-core-persistence.md)
define this milestone's boundaries and verification.
