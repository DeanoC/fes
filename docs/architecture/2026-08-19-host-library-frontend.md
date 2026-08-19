# Host library frontend

Date: 2026-08-19
Status: Designed
Disposition: **GO** for host-only implementation under
[`docs/superpowers/specs/2026-08-19-host-library-frontend-design.md`](../superpowers/specs/2026-08-19-host-library-frontend-design.md).

This note is subordinate to [`ARCHITECTURE.md`](../ARCHITECTURE.md) and ADR
0001. It does not change the public host/target protocol, target runtime,
hardware ownership, or POC6 evidence.

## Contract

- Host catalog platforms are independent of `protocol.System`. Launch stays
  capability-gated; unmapped platforms never cause a target round-trip.
- Loopback `/api/v1` gains pagination, platforms, favorites/recents, local
  media handles, and a bounded attract playlist.
- Operator `[[library_media]]` roots are the only extra-media source. EmuMovies
  remains unauthorized.
- Attract runs on the host display only.

## Evidence

Implementation gates are **Software-tested**. They do not imply Reproducible,
HIL-observed, or Accepted hardware behavior.
