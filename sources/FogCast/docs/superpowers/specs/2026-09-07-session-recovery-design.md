# Recoverable native sessions

The user approved the appliance-reliability milestone and starting implementation
on 2026-09-07. This document describes the intended milestone; only checked
implementation-plan tasks are delivered. Base: FogCast ba9cd93, including the
previously pushed launcher recovery and runtime media-deadline pin.

## Outcome and sequence

1. Resolve ambiguous native Stop results without replaying hardware operations.
2. Reconcile failed launches through the existing agent/host session paths, so
   an observed safe idle state permits another launch while preserving the
   original failure for diagnostics.
3. Exercise host/agent restart, target reboot and lease loss against the existing
   discovery and startup cleanup paths; repair demonstrated inconsistencies.
4. Validate controller neutralization and reconnect, then supported SNES save
   durability across clean Stop/reboot. These receive separate detailed plans.

The delivered backend changes cover Stop observation and failed-launch idle
reconciliation. Diagnostic restart and lease-expiry checks reuse existing cleanup;
see the implementation plan and dated report for the precise acceptance boundary.

## Approach

Extend the current lifecycle in place. A new recovery service would duplicate
lease and transition ownership. Simply raising timeouts would leave ambiguous
outcomes unresolved. Prefer one bounded read-only observation after a lost Stop
reply, reusing the existing native status contract and error mapping.

FogCast owns request/session reconciliation; libmister-runtime remains the only
hardware transition authority. FES owns eventual component pinning and dated
assembled-image evidence. UI rendering and navigation belong to the separate UI
team. No public API or runtime wire-protocol change is needed for the first task.

## Stop observation contract

Send Stop exactly once. A transport error while the operation context remains
live triggers one Status request bounded by the existing health timeout and that
same operation context. Do not detach cancellation or extend lease cleanup beyond
its admitted operation lifetime. A status error or canceled context remains
unavailable. A normal Stop reply follows existing behavior without another read.

Accept only a clean, valid native idle response as successful observed cleanup.
A valid reboot-required response preserves the existing recovery marker. A valid
retryable SNES save failure preserves its mapped error and retained session.
Running, starting, malformed responses, and idle with a retained error remain
unavailable: they do not prove that this Stop completed successfully. Never replay
Stop, launch, input, or reboot during observation. No automatic takeover.

The agent clears durable active content only after confirmed Stop success. An
integration regression must exercise the real native adapter and coordinator:
launch, lose Stop reply after reaching idle, observe idle, then relaunch.

## Later recovery rules

A failed launch must retain its original error; readiness is evidence of current
state, not evidence that the launch succeeded. Unknown hardware state blocks new
launches. Other owners' leases remain untouched. Explicit save failure cannot be
turned into successful cleanup or automatic reboot. Restart handling must reuse
existing target identity, boot ID, lease generation and durable content records;
it must not invent a second source of session truth. Detailed changes follow
fault-injection evidence before expanding the first task's implementation.

## Validation

First run focused Go tests with the race detector, including a real Unix-socket
client test for a dropped Stop response. Check cancellation, malformed state,
reboot-required and save failure, and assert one mutation only. Then run affected
agent/HTTP/host tests. No compiler, Linux or FPGA rebuild is needed for these
host-only checks. Physical acceptance later uses a claimed kit lease and an
identified diagnostic image; software tests alone do not establish hardware
support. Final integration uses FES make dev during iteration and a clean build
at the stabilized milestone.
