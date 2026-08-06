# FogCast POC6 roadmap

POC6 is the stage after the POC5 unified play session. It delivers the
flagship "GoogleCast for games" appliance behavior from IDEA.md: a game that
cannot run on the MiSTer FPGA is launched on the host and **cast to the
MiSTer-attached TV**, with the same catalog, launch flow, and local controller
as an FPGA-native game.

POC5 established the host-side direction (`MiSTer HDMI -> host`) and made
host-only games playable on the host display through one session. POC6 adds
the reverse direction: `host emulator framebuffer -> encode -> RTP/H.264 ->
decode + display on the MiSTer ARM Linux side`.

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

## Proposed milestones

1. **M1 — On-target decode/display spike (throwaway).** Prove a minimal
   H.264 decode-and-display component on the MiSTer target (fbdev/V4L2 or
   equivalent) driven by the existing POC4 RTP stream shape. Verdict records:
   achievable resolution/framerate, decode CPU cost, coexistence with the
   main MiSTer binary, and the mechanism for entering/exiting cast mode.
   POC6 session integration does not begin until this spike returns a
   positive verdict; a negative verdict sends the stage back to design
   (e.g. reduced resolution/framerate targets or a hardware decode path).

2. **M2 — Target cast agent**: productize the spike into a managed on-target
   component under the existing agent/config boundary, with deterministic
   start/stop, error reporting, and clean exit back to FPGA mode without a
   target reboot.

3. **M3 — Host encode path**: capture and encode the host emulator
   framebuffer (reusing the POC4 encode/transport stack where possible),
   paired with the emulator lifecycle from the POC3 host execution boundary.

4. **M4 — Session integration**: extend the POC5 session boundary so a
   host-only launch selects display target (host display or MiSTer TV), owns
   the cast stream lifecycle, and reports cast state through the session
   event feed. FPGA-native launches remain unchanged.

5. **M5 — Input symmetry + latency + acceptance**: local controller works on
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
- Cast-mode teardown (return to FPGA mode, no orphaned on-target processes)
  is verified by inspection after each run, including across target reboots
  with the supervised tunnel's readiness signal as the authority.

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

## Entry criteria

- POC5 accepted: unified play session with host-side display, closed G7
  latency evidence, and input symmetry on the host display path.
- POC4 transport acceptance (`92fca47`) remains the baseline.
- POC1 rollback and target provenance locks unchanged.
- Managed MiSTer target reachable via the supervised tunnel; live
  `GET http://127.0.0.1:18182/v1/health` is the readiness signal.

## Completion disposition

POC6 is complete when a host-only catalog game can be launched, viewed on the
MiSTer-attached TV, played with the local controller, and stopped through the
single session API, with recorded cast-path latency and teardown evidence in
`docs/POC6-RESULTS.md`. The next-stage decision after POC6 is expected to be
library/control productization versus broadening target or source coverage.
