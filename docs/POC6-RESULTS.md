# FogCast POC6 results

## Status

POC6's host-to-MiSTer video, authenticated session ownership, and lifecycle
acceptance passed on 2026-08-08. Two gates remain explicitly deferred rather
than inferred from incomplete evidence:

- unified local/remote controller injection into host RetroArch is tracked by
  [issue #3](https://github.com/DeanoC/FogCast-POC/issues/3);
- physical glass-to-glass latency remains tracked by
  [issue #1](https://github.com/DeanoC/FogCast-POC/issues/1) and
  [issue #2](https://github.com/DeanoC/FogCast-POC/issues/2), because the
  required physical measurement fixture is not currently available.

The accepted claim is therefore narrower than the roadmap's flagship "one
controller" sentence: a host-only catalog game can be launched and stopped
through the single session API and presented on MiSTer HDMI, with deterministic
resource ownership and teardown. This report does **not** claim complete
controller symmetry or measured physical latency.

## Post-acceptance disposition (2026-08-08)

This report is retained **Accepted** historical POC6 evidence, not a statement
of the current installation, image, configuration, credentials, recoverability,
or physical state of any target. Every measurement, command, observation, and
hash below identifies the accepted run and its artifacts only. A rebuilt or
replaced artifact needs its own hash, validation, and evidence classification;
the hashes below must not be relabeled as current-artifact hashes.

Under [ADR 0002](adr/0002-disposable-local-development-target.md), the exact
privately designated local MiSTer Pi is disposable project development
hardware. It may be accessed, deployed to, rebooted, wiped, rebuilt, or have
software, image, configuration, and credentials replaced under standing
authorization. Before a destructive operation, the actor must resolve the
designation through operator-controlled private configuration and verify the
exact target identity; missing, ambiguous, or mismatched verification fails
closed. This standing grant does not relax the lifecycle, reconciliation,
provenance, or evidence requirements recorded below. Other and production
targets remain subject to explicit authorization and their applicable rollback
and security gates. See the current [architecture](ARCHITECTURE.md) and
[active roadmap](ROADMAP.md).

## Accepted end-to-end video path

The final accepted path was:

```text
NAS ActRaiser.smc
→ arm64 RetroArch + arm64 Snes9x
→ macOS screen capture
→ VideoToolbox H.264
→ authenticated RTP/control transport
→ target-owned ARMv7 bridge
→ FFmpeg H.264 decode
→ /dev/fb0
→ disposable native Main_MiSTer presentation hook
→ MiSTer HDMI
→ ShadowCast 3 physical verification
```

The source ROM was read from the authorized NAS-backed SNES library. The
archive contained `ActRaiser.smc`, 1,049,088 bytes. RetroArch identified the
loaded content as `ACTRAISER-USA` and reported `Checksum OK`.

Fresh paired acceptance captures showed the same recognizable ActRaiser title
content at the host and on physical MiSTer HDMI:

These captures are **local-only evidence artifacts** under the ignored
`artifacts/` tree. They are not tracked by Git and are not included in this
change; the paths and hashes below identify the retained local copies only.

```text
artifacts/poc6/capture/poc6-actraiser-fullscreen-host.png
sha256 d3f2ac31c536a43ce051a489d09d188e1873e8b5290ca052e030f095c39fd98c

artifacts/poc6/capture/poc6-actraiser-fullscreen-hdmi.png
sha256 f08beb3e51029caa2a71d5a2f618dde41c5a2d5d87c4067676a60659fd55c189
```

The physical ShadowCast frame showed the ActRaiser logo, `START`, and copyright
text. It was not a synthetic gradient, black output, the MiSTer menu, a pause
overlay, host wallpaper, or stale content.

The accepted receiver run recorded sustained clean transport and presentation:

```text
packets:             15,115+
access units:        674+
decoder writes:      445+
decoded frames:      436+
framebuffer writes:  435+
packet_errors:       0
```

An earlier historical capture with `packet_errors=117` is retained only as
diagnostic history and is not used for final acceptance.

## Target decode and native presentation

The target decoded H.264 with FFmpeg, converted frames to RGBA, and wrote a
1920x1080, 32-bpp framebuffer with stride 7680. Target SDL/`ffplay` was not a
usable display path.

Stock `Main_MiSTer` did not expose arbitrary `/dev/fb0` writes on HDMI. A
disposable native presentation hook was therefore used during acceptance. Its
`fb_cmd_fogcast` command enables the native framebuffer and translates to the
parser's required `fb_cmd1` form. Synthetic gradient captures proved only the
presentation path; the paired ActRaiser captures above prove the real-game
video gate.

Exactly one presentation owner ran from the canonical `/media/fat/MiSTer`
path during HIL. The disposable hook remained installed through all acceptance
gates. It is intentionally retained after shipping as the immediate next-stage
video-plane testbed; stock restoration remains an available rollback action,
not a POC6 acceptance condition.

## Managed target and session ownership

The target agent exposes authenticated cast lifecycle endpoints:

```text
POST /v1/cast/start
POST /v1/cast/stop
GET  /v1/cast/status
```

Cast identity is explicit: status reports session and generation, and stop
requires both. A stale generation cannot terminate a replacement cast. Invalid
or generation-zero starts are rejected, while ambiguous starts are rolled back
against the requested identity and retain retryable ownership if cleanup fails.

The target owns bridge process creation and termination. The host session owns
RetroArch, capture, sender, and target cast composition. Successful sender
runtime is owned by the managed media handle rather than by the short-lived
HTTP launch request context.

Media terminal state propagates through the managed sender, media component,
media session, host API adapter, composition handle, and session coordinator.
Unexpected bridge or sender termination therefore reaps the whole session
instead of leaving stale `active` state.

The final bridge-death HIL killed only the target bridge and then waited on the
host RetroArch process directly. RetroArch exited before any session-status
request was issued; a later status query only confirmed the already-completed
`idle` / `media=stopped` transition. Target inspection found no bridge process
or control/RTP socket owner, and sender logging exposed only a stable sanitized
termination label.

## Input boundary

The input result is intentionally split by execution direction:

- A physical native-keyboard Return changed the running ActRaiser state. This
  proves RetroArch's native host keyboard path works.
- Controlled CUA Return and newline events were reported as delivered by macOS,
  but the ActRaiser content pixels remained byte-identical. Those sends are
  **not** counted as emulator-input success.
- The existing `host/remote_input.go` path sends host-originated input toward
  the MiSTer target. It is not a host-RetroArch input injector.
- Host-only composition now leaves that target input bridge detached, so a
  host-only launch does not fail merely because target remote input is enabled.
  FPGA-native launches retain the established target-input behavior.

Consequently, POC6 does not claim unified controller symmetry. A supported
host-emulator controller capture/injection path, press/release lifecycle, and
HIL state-change proof are tracked by issue #3.

## Repeated launch and teardown

A single API process completed two consecutive full launch/stop cycles. Both
launch responses reached:

```text
state:     active
execution: host_only
media:     active
```

Both stops returned:

```text
state:     idle
execution: host_only
media:     stopped
```

Each host media session created a fresh capture source. This fixes the earlier
second-launch failure caused by reusing a stopped capture handle.

After the second stop, inspection found no RetroArch process, target bridge, or
control/RTP socket owner. A separate live shutdown gate launched an active
session, terminated the tracked host API, and then verified:

- no listener remained on the host API port;
- no RetroArch process remained;
- no host control/RTP socket remained;
- no target bridge process or control/RTP socket remained.

Graceful target-agent shutdown explicitly stops its cast controller, preventing
a bridge from surviving a managed replacement that allows shutdown cleanup to
run. An ungraceful crash or SIGKILL can still orphan the bridge and requires the
manual reconciliation documented in the development guide. Startup rejects a
bridge child that exits during the startup grace period.

The final exact-tree HIL used these binaries:

```text
host fogcast-api sha256
3c45d235ded9268b378aeb2b7e3ad435454e53c42f75566704094451cca70ea0

deployed ARMv7 mister-agent sha256
0d0c660e4899d095bb6f2c96323a9bd83adb68aec4ce9783edd1e2008a519704

retained ARMv7 decode bridge sha256
86ac553a5427ebfe1ed506295106426b82c2fdf8f313a8e6aa4a705c80825b58

retained native presentation hook sha256
138e6f0471fbedd94b4c76fce3ce20a2ec61b459b04657a367c77256a3a347b2
```

The bridge and hook hashes above were re-verified directly on the retained
authorized target before the final documentation shipment. No target path,
address, credential source, or token is recorded in this report beyond the
canonical public testbed path `/media/fat/MiSTer`.

After deploying that agent, the bridge-death gate again recorded
`AUTONOMOUS_HOST_REAP=passed`; the later API observation was
`state=idle, media=stopped`, target inspection found no bridge or media socket,
and host teardown left no API listener or RetroArch process.

## Latency boundary

Physical glass-to-glass latency was not measured. The required physical
flash/timestamp fixture and measurement setup are unavailable. Encode timing,
RTP timing, decoder counts, and framebuffer writes are not substitutes for a
source-to-display measurement.

The existing physical-latency follow-ups remain open in issues #1 and #2. No
p50, p95, `<=120 ms`, or stretch-target claim is made for POC6.

## Verification

Focused regressions cover:

- immediate target bridge child exit;
- sender liveness while screen capture is idle;
- managed sender lifetime beyond the HTTP start context;
- media terminal-state propagation and session reaping;
- host-only exclusion of the target input bridge;
- fresh capture ownership for each repeated host-media session;
- graceful target-agent shutdown cleanup;
- retryable capture-source close after an initial failure;
- partial local and target-start cleanup ownership across rollback;
- session/generation-conditioned target stop and stale replacement safety;
- generation-zero rejection at both HTTP and controller boundaries;
- serialized status/replacement observation and first-error-preserving cleanup
  retries.

Final repository-wide validation passed:

```sh
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make fmt
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make test
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make check
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make build
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go test -race ./...
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go vet ./...
git diff --check
```

`make test` is itself race-enabled; the explicit race run provides a second
repository-wide confirmation.

## Disposition

POC6 accepts the real-game host-to-MiSTer HDMI path and its managed lifecycle.
The validated POC4 RTP/H.264 transport remains the baseline. Product work
should next choose between library/control UX productization and closing the
controller-injection follow-up in issue #3; the video transport should not be
redesigned without new evidence or a new scope decision.

That recommendation is the historical POC6 disposition. Current work is
selected and gated by the [active migration roadmap](ROADMAP.md), under the
current [architecture](ARCHITECTURE.md), rather than by this accepted report.

The [POC6 development guide](POC6-DEVELOPMENT.md) is the operating and
extension handoff for the intentionally retained target testbed. It records the
private configuration boundaries, normal launch/status/stop workflow, recovery
rules, code map, and the evidence required for future changes.
