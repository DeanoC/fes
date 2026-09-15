# Read-only native core status design

**Date:** 2026-09-15

**Status:** Approved for implementation

## Goal

Make the native FES launcher expose truthful, read-only status for the
selected package behind each FPGA-core library entry while preserving the
existing browse, launch, and stop flow.

The first supported FES set is `fes.pong`, `fes.zx81`, and `fes.coleco`.
The client and UI remain generic so later FES cores do not require a new UI
path.

## Existing contract

FogCast already exposes the required read paths:

- `GET /api/v1/library/core-entries` returns the stable game/core/selected
  package mapping.
- `GET /api/v1/core-packages` returns installed package identities,
  descriptors, entries, and the current compatibility observation.
- `POST /api/v1/session/launch` and `POST /api/v1/session/stop` already own
  the native package lifecycle.

The launcher must not call the package-selection `PUT` endpoint in this
slice. Selection remains an operator/API operation until a later UI design.

## Normalized read model

The ten-foot client will expose a `CoreLibrary` read model containing the
entries and installed packages, plus a deterministic `Availability()` join.
Each joined `CoreAvailability` identifies the game, core, selected package,
package descriptor identity/version, raw compatibility value, and a state.

States are:

- `installed`: selected package is installed and declares the same core ID.
- `missing`: the selected package ID is not in the installed inventory.
- `mismatch`: the selected package exists but declares a different core ID.
- `incompatible`: the installed package reports explicit incompatibility.

The inventory's normal `compatibility:"unknown"` value is preserved and is
shown as unknown; it must not be presented as hardware-ready. If either read
request fails, the launcher clears stale per-core claims and reports package
status unavailable while leaving ordinary catalog browsing usable.

## UI behavior

The kit launcher refreshes core status alongside its periodic catalog refresh.
Core rows and the focused detail pane append a compact status containing the
core ID, state, and a short package identity/version where available. Normal
non-FPGA games are unchanged. Existing launch and Select+Start stop behavior
is unchanged; the status is informational and does not disable or mutate a
selection.

The status revision participates in the renderer key so a package inventory
change repaints immediately. The on-kit catalog cache does not persist this
host/package status, avoiding stale claims after an offline boot.

## Verification boundary

Unit tests cover client decoding/joining, launcher refresh/error behavior, and
render-model status copy for all three FES core IDs. The existing
`scripts/tests/package-runtime-smoke_test.sh` remains the bounded load/stop
coverage for Pong, ZX81, and Coleco. No full image rebuild, FPGA synthesis,
or physical-display acceptance is part of this software slice.
