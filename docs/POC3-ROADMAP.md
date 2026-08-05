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
- A deterministic interrupted-upload fixture is added or the gate is formally
  redesigned and documented.
- POC1 rollback remains tested and the target provenance locks are unchanged.

## First POC3 design question

Choose the host API boundary before building UI: a local HTTP/JSON service
backed by the existing Go packages is the recommended default because it keeps
CLI, browser, and future native clients aligned without coupling UI code to
the MiSTer protocol.

## Decision: local host API

POC3 uses a versioned HTTP/JSON application API served only on loopback. The
first read-only slice is implemented by `fogcast-api` and exposes host health,
public target status, the normalized game list, and deterministic game details.
Public models intentionally omit NAS paths, library IDs, target credentials,
cache digests, and ROM filenames. Write operations and event streaming remain
future vertical slices; they should be added behind this host boundary rather
than exposing the MiSTer API to UI clients.
