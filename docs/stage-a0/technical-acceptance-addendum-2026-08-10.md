# Stage A0 technical acceptance addendum — 2026-08-10

## Decision

The bounded Stage A0 technical scope is **Accepted** as of 2026-08-10. This
append-only addendum updates the active acceptance interpretation of the dated
Stage A0 records; it does not rewrite their hashes, observations, or original
evidence classifications.

The accepted scope is:

- the pinned long-lived `Main_MiSTer` fork and its reproducible two-build
  comparison;
- the provenance-bound Overlord resource slice and its scoped
  Software-tested generation evidence;
- native Main/core launch on the disposable `misterpi`, including Mega Drive
  and SNES exercises, authenticated input transport, HDMI/video observation,
  and clean session stop; and
- HIL-observed audible Stage A Sonic HDMI output through the live Genki/USB
  HDMI viewer/listening path after its system volume was unmuted.

For this bounded fixture and scope, the live physical viewer observation is the
audio acceptance instrument. A paired measured comparator/Stage-A recording is
not required to accept this scope.

## Explicit boundaries

- The Genki/ShadowCast/FFmpeg endpoint still produces exact-zero PCM and the
  retained AAC tracks remain `Software-tested`/`policy-blocked`. This is a
  capture-path limitation, not contrary evidence about the audible Stage A
  source output.
- Stage A does not claim measured capture parity, save/reload behavior, full
  FPGA gateware or HPS implementation, production-appliance readiness, or
  Distribution-ready publication.
- The 17-material license queue remains the separate Distribution-ready
  release gate under ADR 0003 from source commit
  `3cdc75f66409104cd2f9f6dfcbedf69f9d564ae0`
  (`docs/adr/0003-license-preservation-and-distribution-boundary.md`); that
  historical release record is not imported into this focused capture-
  diagnosis package.

## Follow-up session

The Genki/ShadowCast audio-capture issue is moved to a separate diagnostic
session. That work should inspect host-side device selection, stream ownership,
CoreAudio/AVFoundation routing, channel/format negotiation, mute/gain state,
and viewer-versus-FFmpeg configuration. It must preserve this acceptance
decision and should update the deterministic audio-evidence record only after
non-zero PCM is reproduced from a named, hash-bound capture path.

See the [capture-path follow-up handoff](audio-capture-follow-up-2026-08-10.md).
