# Explicit core input diagnostics — 2026-09-18

FES `feat/core-input-diagnostics`, based on main `7bf4d02`, on Powerboat at
`/home/deano/fes/out/dev/core-input-diagnostics/fes`.
This slice changes parent acceptance tooling only. Component pins, target
software, the factory package set and the physical kit are unchanged.

## Contract

An optional digest-bound JSON event file adds an input phase to the existing
owned-session lifecycle. The default remains lifecycle-only. Explicit platform
identities and execution opt-in remain required. The isolated wrapper retains
one validated event snapshot and requires matching diagnostic evidence in both
the initial and host-restart cycles.

The input phase requires the requested observed interface, attached/ready input,
stable target/session/flight/game/package/generation and input-session identity.
It checks finite time bounds, refuses reconnect/counter regression and never
replays ambiguous event requests. Existing owned Stop cleanup is reused.

The host endpoint has no event-level session CAS token. The surrounding checks
do not eliminate a race with another launcher; exclusive host/kit use remains
mandatory. This change does not detach or take over a launcher-owned input source.

## Evidence interpretation

`make test` passed: 400 parent tests (36 outer delegated skips), delegated
media/card checks, platform Go tests and image-script tests. `make check`
passed: 18 generated consumers, 15 fixture copies and four copied source pins
matched. Independent reviews of the runner and isolated wrapper found no
remaining blockers. Logs are retained in `out/validation/core-input-tests.log`
and `out/validation/core-input-check.log` in this worktree.

The committed Pong example parsed successfully with two events and SHA-256
`3d80d5f5f2ce7991af1f623b8e51630a027cd534a6478e5c86577d177b446460`.

Requested events, host acknowledgements and input frame-counter deltas are
recorded separately. Host frame counters include heartbeats and state resyncs;
they are not per-event FPGA acknowledgements. A successful diagnostic does not
prove physical USB controls, visible screen response, game correctness or
exact-image release acceptance.

All tests for this slice are host-only fixtures. No live host API, kit lease,
FPGA programming, input injection or HDMI capture was performed. The earlier
physical acceptance record is not evidence for this new diagnostic command.
