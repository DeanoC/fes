# FogCast POC6 roadmap

## Historical status and current direction

This is the completed historical POC6 scope and acceptance disposition. It
remains authoritative for the milestones, limits, and evidence it records, but
it is not the active migration plan. The current destination is the portable
target appliance and library-shaped runtime in [ARCHITECTURE.md](ARCHITECTURE.md);
its future gates are in the [active roadmap](ROADMAP.md). The accepted POC6
evidence is in [POC6 results](POC6-RESULTS.md), and retained-testbed operations
and reconciliation are in the [development guide](POC6-DEVELOPMENT.md).

The following language describes the original POC6 intent. Its statement that
all intelligence stays on the host is historical scope, not current
architecture: the host owns catalog, policy, and user intent, while the target
appliance owns local mechanisms, arbitration, observed state, and recovery.

POC6 is the stage after the POC5 unified play session. It delivers the
flagship "GoogleCast for games" appliance behavior from IDEA.md: a game that
cannot run on the MiSTer FPGA is launched on the host and **cast to the
MiSTer-attached TV**, with the same catalog, launch flow, and local controller
as an FPGA-native game.

POC5 established the unified session boundary and integrated the host-side
launch/display direction (`MiSTer HDMI -> host`). It did not close the original
controller-symmetry or physical-latency criteria. POC6 adds the reverse video
direction: `host emulator framebuffer -> encode -> RTP/H.264 -> decode +
display on the MiSTer ARM Linux side`.

## Why a separate stage

The cast-to-TV direction has one genuinely unproven endpoint: decode and
display on the MiSTer target itself. The POC4/POC5 transport and session
machinery is reusable, but there is no existing on-target video player story,
and open questions (software decode performance at 1080p on the MiSTer's ARM
cores, coexistence with the main MiSTer binary and scaler, returning to FPGA
mode without a reboot) are target-side unknowns of exactly the kind that
POC3/4/5 deliberately avoided bundling into other gates. POC6 therefore opens
with a derisking spike before any session integration.

## Goal

A user selects any game from the catalog. If it is FPGA-native it launches on
the MiSTer as today. If it is host-only, the host runs the emulator and the
MiSTer-attached TV shows the result, with the local controller working
identically in both cases. The MiSTer behaves as a simple network appliance;
all intelligence stays on the host.

The flagship acceptance claim: *"any game in the catalog, playable on the
MiSTer TV, one UI, one controller"* — with measured latency on the cast path.

## Milestone disposition

1. **M1 — On-target decode/display spike (accepted for feasibility).** The
   target decoded the existing POC4 H.264/RTP stream, wrote `/dev/fb0`, and
   presented recognizable game content on physical MiSTer HDMI through the
   disposable native hook. Receiver/framebuffer counters and the 1920x1080
   presentation geometry were recorded. Decode CPU cost, a quantified achieved
   frame rate, and stock-`Main_MiSTer` coexistence were not measured and are not
   part of the accepted claim.

2. **M2 — Target cast agent (accepted for managed ownership).** The target
   agent owns authenticated bridge start/status/stop, session/generation
   identity, bounded cleanup, and graceful agent-shutdown teardown. Process and
   socket cleanup passed for normal stop and managed replacement. Return to
   stock FPGA presentation without reboot was not an acceptance gate: the
   non-stock presentation hook remains intentionally installed as the
   development testbed.

3. **M3 — Host encode path (accepted)**: capture and encode the host emulator
   framebuffer (reusing the POC4 encode/transport stack where possible),
   paired with the emulator lifecycle from the POC3 host execution boundary.

4. **M4 — Session integration (accepted)**: extend the POC5 session boundary so a
   host-only launch selects display target (host display or MiSTer TV), owns
   the cast stream lifecycle, and reports cast state through the session
   event feed. FPGA-native launches remain unchanged.

5. **M5 — Input symmetry + latency (deferred)**: local controller works on
   the cast path identically to FPGA-native; glass-to-glass latency measured
   on the cast path against the same target (p95 <= 120 ms, stretch
   <= 80 ms); full acceptance report in `docs/POC6-RESULTS.md`.

## Gates and evidence

Same evidence discipline as POC4/POC5:

- Uniquely identified runs with redacted reports; unobserved hardware
  behavior stays `untested`.
- Encode timing, packet counts, and event emission are not display evidence;
  the cast path requires observed decoded output on the MiSTer TV.
- Failure-path coverage via the deterministic impairment harness; real
  capture runs cover the happy path.
- Cast-mode process teardown for normal stop and graceful shutdown is verified
  by inspection: no orphaned target bridge or media socket may remain. An
  ungraceful target-agent crash or `SIGKILL` can orphan the bridge and requires
  the reconciliation procedure in the development guide. Return to stock FPGA
  presentation without reboot is unclaimed while the non-stock hook is
  intentionally retained. After target reboots, the supervised tunnel's live
  readiness signal remains authoritative.

## Explicit exclusions

- No redesign of the validated POC4 transport protocol or encoder settings
  without a new scope decision; the spike may propose reduced
  resolution/framerate targets but not a new transport.
- No wired-Ethernet requirement; Wi-Fi remains accepted unless latency
  measurement shows it is the bottleneck.
- No wireless controller pairing, save-state sync, internet access, remote
  administration, or multi-target support.
- No non-MiSTer FPGA architectures or real-hardware capture sources (the
  very-long-term IDEA.md expansion).
- Library/catalog UX productization remains its own later stage.

## Historical entry criteria

- POC5 supplied the unified host-only play-session boundary. Its roadmap's
  controller-symmetry and physical-latency criteria were not closed and are not
  treated as inherited evidence for POC6.
- POC4 transport acceptance (`92fca47`) remains the baseline.
- POC1 rollback and target provenance locks unchanged.
- Managed MiSTer target reachable via the supervised tunnel; live
  `GET http://127.0.0.1:18182/v1/health` is the readiness signal.

## Completion disposition

The original full completion sentence combined video presentation, controller
symmetry, and physical latency into one gate. Acceptance split that sentence at
the available evidence boundary. POC6 is complete and shipped for M1-M4: a
host-only catalog game launches through the single session API, appears on the
MiSTer-attached TV, stops cleanly, and has deterministic target/host lifecycle
ownership. M5 controller symmetry and physical latency remain explicitly
deferred rather than being inferred or fabricated.

Use the [POC6 development guide](POC6-DEVELOPMENT.md) to operate and extend the
retained POC6 testbed.
The next-stage decision is library/control productization versus closing the
host-emulator controller path in
[issue #3](https://github.com/DeanoC/FogCast-POC/issues/3); do not redesign the
accepted POC4 transport without a new evidence-backed scope decision.

That next-stage recommendation is historical POC6 disposition, not current
planning. Current work is selected and gated by the [active roadmap](ROADMAP.md)
under the current [architecture](ARCHITECTURE.md).

## Acceptance disposition

The 2026-08-08 acceptance recorded in `docs/POC6-RESULTS.md` passes the M1
target decode/display path, M2 managed target ownership, M3 host-emulator video
path, and M4 session/API lifecycle. Fresh RetroArch ActRaiser content was
observed through ShadowCast 3 on MiSTer HDMI, and repeated launch, normal stop,
unexpected media exit, graceful target-agent shutdown, and host-API shutdown
were verified without orphaned media resources.

The disposable native presentation hook remains intentionally installed at the
canonical `/media/fat/MiSTer` path as the next-stage video-plane testbed. This
is a deliberate retained development state, not an unverified claim that stock
presentation supports `/dev/fb0`; stock restoration remains the rollback path.

M5 remains explicitly split at its evidence boundary. Native host keyboard
input works, but unified local/remote controller injection into host RetroArch
is deferred to GitHub issue #3. Physical glass-to-glass latency is deferred to
issues #1 and #2 until the required measurement fixture is available. The
results therefore do not claim the roadmap's complete "one controller" or
latency target.

## Post-acceptance disposable-kit disposition (2026-08-08)

The retained-hook and rollback wording above records the accepted POC6 testbed
state; it is not an assertion that the previously retained image, binaries, or
stock presentation are still present or recoverable. Under
[ADR 0002](adr/0002-disposable-local-development-target.md), the exact
privately designated local MiSTer Pi may be accessed, deployed to, rebooted,
wiped, rebuilt, or have software, image, configuration, and credentials
replaced under standing authorization. The prior retained image/binaries and
stock restoration are therefore not unconditional requirements for that kit.

Before a destructive operation, tooling must resolve that designation only from
operator-controlled private configuration and verify the exact target identity;
if either check is missing, ambiguous, or mismatched, it must fail closed. The
standing grant preserves lifecycle cleanup, reconciliation, provenance, and
evidence requirements, and it does not apply to another or production target:
those remain subject to explicit authorization plus the applicable rollback and
security gates. Historical accepted hashes identify accepted artifacts only;
rebuilt artifacts need new hashes and validation.
