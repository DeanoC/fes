# FogCast POC3 roadmap

POC3 is the next productization step after the POC2 target cache is stable. It
is host-library and workflow work, not a second target-image rewrite.

## Goal

Make the Mac app a useful controller for a large personal library while keeping
the MiSTer a simple network appliance. A user should be able to browse a
normalized catalog, select a game, see whether it is FPGA-native or host-only,
and receive a clear launch/error state using one controller.

## Proposed milestones

1. **Catalog durability:** incremental scans, stable metadata, file-change
   detection, duplicate identity handling, and explicit NAS-offline state.
2. **Library model:** system/core capability registry, region/title metadata,
   favorites, recently played, and a deterministic game-detail API.
3. **Host execution boundary:** define a host-only emulator adapter contract
   and prove one non-MiSTer emulator locally without adding video streaming.
4. **Unified control session:** launch/stop/status events, reconnect handling,
   progress reporting, and controller ownership rules.
5. **UI shell:** a small Mac-first browser or native shell over the host API;
   the Pi remains UI-free.

## Explicit exclusions

POC3 does not yet promise arbitrary systems, wireless controllers, save-state
synchronization, internet access, remote administration, or video streaming
through the MiSTer. Those are separate design questions after the host-only
adapter and control session are reliable.

## Entry criteria

- POC2 cache publication works on the MiSTer exFAT filesystem.
- Reboot and NAS-offline cache behavior remain green.
- The POC2 deterministic interrupted-upload gate passed separately as recorded
  in `docs/POC2-HANDOFF.md`; it is not a POC3 host-control gate.
- POC1 rollback remains tested and the target provenance locks are unchanged.

## First POC3 design question

Choose the host API boundary before building UI: a local HTTP/JSON service
backed by the existing Go packages is the recommended default because it keeps
CLI, browser, and future native clients aligned without coupling UI code to
the MiSTer protocol.

## Decision: local host API

POC3 uses a versioned HTTP/JSON application API served only on loopback. The
POC3 application slice is implemented by `fogcast-api` and now exposes host health,
public target status, the normalized game list, deterministic search, explicit
`fpga_native` execution capability, deterministic game details, a serialized
launch/stop session boundary with bounded polling events, a self-contained
browser shell, and the initial `host_only` RetroArch adapter contract.
Public models intentionally omit NAS paths, library IDs, target credentials,
cache digests, and ROM filenames. Write operations and event streaming remain
future vertical slices; they should be added behind this host boundary rather
than exposing the MiSTer API to UI clients.

## Completion disposition

The POC3 implementation is complete and ready for the next phase for its
defined host-control scope. The authoritative checkout contains the local
versioned HTTP/JSON API, normalized catalog and session workflow, host-only
execution boundary, authenticated target-owned MiSTer remote input,
reconnect-safe state handling, and verified managed-target HIL. The remote-input
evidence is recorded in `docs/remote-input-evidence.md`.

Video streaming, receiver/display integration, wired-network measurement, and
physical glass-to-glass latency are explicitly post-POC3 work. They must not be
used to block this POC3 completion decision.

## Transition to POC4

POC4 began after the final deterministic POC2 interrupted-upload report passed.
Its evidence-driven remote-play decision was to complete the integrated
capture/encode/transport/receiver/display vertical slice, whose accepted
disposition is recorded below. The stage used one authoritative integration
checkout and measured each gate rather than repeating broad overlapping
worktrees.

Implementation tracking and the current receiver/evidence plan are in
`.hermes/plans/2026-08-06_121559-poc4-remote-play.md`. The physical
glass-to-glass latency gate is deliberately deferred and tracked in GitHub
issue #1; receiver ingest is not equivalent to decoded/displayed playback.

## POC4 completion disposition

POC4 is accepted for its defined transport scope on commit `92fca47`:
software/protocol validation, real ShadowCast 3 capture, receiver decode and
display, clean Wi-Fi operation, impairment/recovery telemetry, and lifecycle /
security behavior are evidenced in `docs/POC4-RESULTS.md`. This acceptance does
not claim physical glass-to-glass latency; that remains deferred under GitHub
issue #1. The next stage should make an explicit decision about integrating the
measured video plane behind the POC3 session boundary versus productizing the
library/control experience first. Do not redesign MiSTer video transport or
require wired Ethernet without a new scope decision.
