# Native Session Recovery Implementation Plan

> Execute inline using superpowers:executing-plans, with a review after each task.

**Goal:** Make failed native sessions recover to a verified usable state without SSH.

**Architecture:** Extend the existing native adapter and agent/host lifecycle.
Keep hardware transitions in libmister-runtime and ownership in the kit lease.

**Tech Stack:** Go, existing Unix-socket runtime protocol, Go race tests.

**Spec:** ../specs/2026-09-07-session-recovery-design.md

## Global constraints

- UI code belongs to the separate UI team.
- No mutation replay, implicit takeover or new recovery service.
- Preserve save failures and context/lease cancellation.
- Source worktree: out/dev/session-recovery/FogCast; base ba9cd93.
- Hardware claims require dated physical evidence; diagnostics are not release acceptance.

## Task 1: Observe a lost Stop response

Files: internal/misterruntime/runtime.go; internal/misterruntime/stop_recovery_test.go;
internal/agent/coordinator_test.go; docs/ARCHITECTURE.md.

Consumes: Control.Stop(ctx) and Control.Status(ctx), returning Response and error.
Produces: existing Stop/StopOwned/StopOwnedWithRecovery signatures with bounded
observation on transport failure; no public interface additions.

- [x] Add regression cases with recordingControl.stopErr = io.EOF and Status
      returning clean idle, running, malformed idle, retained-error idle,
      reboot-required, and retryable save failure. Assert stopN == 1 and statusN == 1.
- [x] Run `go test ./internal/misterruntime -run LostStop -count=1`; observe clean
      idle/reboot/save outcomes fail before implementation.
- [x] On Stop transport error, return unavailable if ctx.Err() != nil; otherwise
      call r.boundedStatus(ctx), check ctx.Err() again, and accept only
      validCleanIdle, rebootRequiredResponse or retryableSaveFailure. Leave
      normal Stop response validation unchanged.
- [x] Add a Unix-socket dropped-reply regression through the production Client,
      plus cancellation/deadline and coordinator Stop/relaunch regressions.
- [x] Run `go test -race ./internal/misterruntime ./internal/agent` and affected
      HTTP/host packages, then `git diff --check`.
- [x] Document the observed Stop behavior and its software-only validation.

## Task 2: Reconcile failed native launch

Files: internal/agent/coordinator.go; internal/agent/coordinator_test.go;
internal/misterruntime/runtime.go; internal/misterruntime/stop_recovery_test.go.

- [x] Reproduce native failed launch returning idle but leaving the coordinator
      failed/unavailable and unable to relaunch.
- [x] Add bounded native ConfirmIdle observation; reject unknown/malformed state,
      reboot-required and explicit save failure.
- [x] Under the existing transition, clear content only after confirmed idle and
      preserve the original launch error in the idle status and failed response.
- [x] Keep unresolved native failures unready; gate ordinary and development
      launches on coordinator readiness. Reproduce non-transport cleanup failure
      and development bypass before fixing them.
- [x] Verify explicit Stop retries cleanup and restores readiness.
- [x] Run complete Go race suite and independent code review; fix its development
      readiness finding and rerun affected packages.

## Task 3: Diagnostic recovery matrix

- [x] Incrementally cross-build the changed Go agent; leave toolchain, Linux,
      FPGA inputs, UI and parent source pins unchanged.
- [x] Claim the designated kit and install a temporary bind-mounted binary.
- [x] Reject a native launch, preserve diagnostics, observe idle, launch Pong.
- [x] Drop a real successful native Stop response, observe idle, relaunch Pong;
      record proxy operations proving no Stop replay.
- [x] Kill the agent while Pong runs; verify supervisor/startup cleanup, idle,
      stale-token rejection and a new owned launch/Stop.
- [x] Let an owned lease expire while Pong runs; verify cleanup and old-token
      rejection without takeover.
- [x] SNES diagnostic with a unique test game ID: mount a temporary filesystem
      over only its save directory before launch, remount it read-only during
      the game, require Stop failure with retained ownership, restore writes, retry Stop, then
      relaunch and confirm a valid save file survives another Stop. Preserve
      existing user save paths. Remove the mount after Stop. This does not establish
      in-game progress reload or FAT cold-boot durability.
- [x] Record hashes, limitations and final kit state in the dated report.

## Separate acceptance and integration work

The broader appliance spec also includes physical held-controller unplug/replug,
in-game SNES progress across a cold boot, host-process restart and target reboot.
Existing automated discovery, input, lease and save regressions are included in
the full race suite; they are not physical acceptance for those scenarios.
Component publication/pinning and a clean assembled-image build follow reviewed
source revisions. No parent pin or boot image is promoted by these diagnostics.

## Execution record

Implementation is an uncommitted diff on feat/session-recovery, based on
ba9cd93. Red/green regressions establish the Stop ambiguity, sticky failed launch,
content-cleanup readiness and development-admission defects. See the dated
report for hardware artifacts and results. The test injector and local results
are retained under out/dev/session-recovery/evidence in FES.
