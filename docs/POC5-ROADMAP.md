# FogCast POC5 roadmap

POC5 is the next product stage after the POC4 remote-play transport acceptance
(commit `92fca47`). It resolves the explicit decision deferred by POC4: the
measured video plane is integrated behind the POC3 session boundary rather
than deferred. Library/catalog productization (favorites, recents, metadata
richness, shell polish) remains a later stage and does not block POC5.

## Goal

Close the core IDEA.md loop: one unified play session in which a user picks a
game from the catalog and plays it, regardless of execution mode. FPGA-native
games launch on the MiSTer exactly as POC3 proved; host-only games launch via
the existing host execution boundary **and become visible and playable** via
the POC4 capture/encode/transport/receiver plane, under one session lifecycle
and one controller.

The flagship acceptance claim for POC5 is: *"any game in the catalog, same
UI, same launch flow, playable locally"* — with measured, not assumed,
playback for the host-only path.

## In scope

1. **Session-plane video integration**
   - Extend the POC3 session boundary (`internal/hostexec`, the
     `/api/v1/session/*` API in `internal/hostapi`/`internal/httpapi`) so a
     `host_only` launch pairs automatically with the POC4 RTP/H.264 stream
     lifecycle.
   - Stream start on launch, stream teardown on stop; both reported through
     the single session event feed (`/api/v1/session/events`).
   - Session owns the stream: no orphaned senders/receivers after stop,
     crash, or API client disconnect.
   - `fpga_native` launches must remain byte-for-byte behaviorally unchanged
     (no video path involvement).

2. **Receiver/display path on the host**
   - Wire the validated POC4 receiver and its local display backend
     (`cmd/remote-play-receiver`, `ffplay` or successor) into the session UX
     so host-only games are actually playable, not merely transport-validated.
   - Receiver lifecycle bound to the session; deterministic handling of
     receiver startup failure (session reports a clear error state rather
     than a silent no-video success).

3. **G7 glass-to-glass latency closure (GitHub issue #1)**
   - The integrated session is exactly the fixture G7 was waiting for. Run
     the deferred measurement: p50/p95 source-to-display latency, test mode,
     receiver behavior, transport conditions, and methodology recorded in
     `docs/POC5-RESULTS.md`.
   - Acceptance target: p95 ≤ 120 ms (stretch ≤ 80 ms). If the target is
     missed, the result is still recorded and the gap becomes an explicit
     follow-up decision — not an unexplained failure.

4. **Input symmetry**
   - Reuse the POC3 target-owned remote-input bridge so the same controller
     works for both execution modes within a session.
   - Verify controller ownership rules hold across a session that switches
     between `fpga_native` and `host_only` launches.

## Explicit exclusions

- No redesign of the MiSTer video transport, encoder, or protocol. The POC4
  measured path (`MiSTer HDMI -> ShadowCast 3 UVC -> AVFoundation/VideoToolbox
  -> RTP/H.264 -> receiver`) is the transport. Changes to it require a new
  scope decision.
- No wired-Ethernet requirement; Wi-Fi remains an accepted transport unless
  G7 measurement shows it is the latency bottleneck.
- No catalog/library UX productization (favorites, recents, rich metadata,
  shell redesign).
- No save-state sync, wireless controller pairing, internet access, remote
  administration, multi-target support, or non-MiSTer FPGA architectures.
- No streaming *from* host emulators to the MiSTer display; POC5's host-only
  path displays on the host side only (the "MiSTer shown locally" IDEA.md use
  case). Rendering host-emulated games onto the MiSTer-attached TV is the
  dedicated POC6 cast-to-TV stage (`docs/POC6-ROADMAP.md`).

## Proposed milestones

1. **M1 — Stream/session lifecycle contract**: define the pairing between
   `host_only` launches and the remote-play stream inside the existing
   session boundary, with events for stream starting/ready/failed/stopped.
2. **M2 — Managed receiver**: session-spawned receiver + display with
   deterministic error reporting and teardown; HIL-style integration test
   with the synthetic protocol fixture.
3. **M3 — Real-hardware integration**: host-only game launched through the
   API becomes playable locally end-to-end with the ShadowCast 3 capture
   path; evidence recorded.
4. **M4 — G7 measurement**: glass-to-glass latency measured on the integrated
   fixture; closes GitHub issue #1.
5. **M5 — Input symmetry + acceptance**: controller works across both modes;
   full POC5 acceptance report in `docs/POC5-RESULTS.md`.

## Gates and evidence

Follow POC4's evidence discipline:

- Every gate has a uniquely identified run and a redacted report; unobserved
  hardware behavior stays `untested`, never inferred from transport or
  timing logs.
- Receiver UDP bind, sender encode timing, and session event emission are
  **not** playback evidence. Playback requires decoded/displayed observation
  (as in POC4 G3).
- Integration tests must use the deterministic impairment harness
  (`bin/remote-play-impair`) for failure-path coverage; real capture runs
  cover the happy path.
- Session teardown correctness (no orphaned processes, no stale streams) is
  verified by inspection after each run, not assumed.

## Entry criteria

- POC4 acceptance stands (`92fca47`); G1–G6 evidence in
  `docs/POC4-RESULTS.md` remains the transport baseline.
- POC3 session boundary and host-only RetroArch adapter remain green.
- POC1 rollback and target provenance locks unchanged.
- Managed MiSTer target reachable via the supervised tunnel; live
  `GET http://127.0.0.1:18182/v1/health` is the readiness signal.

## Completion disposition

POC5 is complete when a host-only catalog game can be launched, viewed,
played, and stopped through the single session API with recorded
glass-to-glass latency and input symmetry evidence, and `docs/POC5-RESULTS.md`
records every gate. The next stage is POC6 (`docs/POC6-ROADMAP.md`): casting
host-emulated games to the MiSTer-attached TV for the full "GoogleCast for
games" appliance behavior. Library/control productization remains a parallel
later-stage option after POC6.
