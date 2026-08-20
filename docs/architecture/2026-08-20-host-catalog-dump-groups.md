# Host catalog dump groups

Date: 2026-08-20
Status: Designed
Disposition: **GO** for host-only implementation under
[`docs/superpowers/specs/2026-08-20-host-catalog-dump-groups-design.md`](../superpowers/specs/2026-08-20-host-catalog-dump-groups-design.md).

This note is subordinate to [`ARCHITECTURE.md`](../ARCHITECTURE.md), ADR 0001,
and the 2026-08-19 host library frontend note. It does not change the public
host/target protocol, target runtime, hardware ownership, or POC6 evidence.

## Contract

- File `game_id` stays path-stable. Grouping is a catalog query overlay from
  scan-time dump fields and `group_key`.
- Loopback `/api/v1/games` filters region, flags, availability, genre, and year
  on the host index. The browser must not facet the loaded page.
- `protocol.System` stays Mega Drive and SNES. Host-emulator cores are
  per-platform; unmapped platforms never cause a target round-trip.
- Target content identity stays ≤ 32 MiB. Host-only cue/gdi/chd and oversized
  files launch from a confined library path and never enter the target cache
  protocol.
- Continue is last-played catalog identity, not save-state sync.

## Evidence

Implementation gates are **Software-tested**. They do not imply Reproducible,
HIL-observed, or Accepted hardware behavior.
