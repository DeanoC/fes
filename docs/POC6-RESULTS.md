# FogCast POC6 results

## Status

POC6 is **not fully complete**. This checkpoint closes the target-side video
feasibility spike and the host-emulator video path. Session/API ownership,
controller symmetry, latency acceptance, and productized teardown remain open.

## Accepted checkpoint: host-emulator video path

Fresh acceptance run on 2026-08-07:

```text
NAS ActRaiser.zip
→ extracted ActRaiser.smc
→ arm64 RetroArch + arm64 Snes9x
→ macOS screen capture
→ VideoToolbox H.264
→ RTP
→ target FFmpeg decode
→ native Main_MiSTer framebuffer
→ MiSTer HDMI
→ ShadowCast 3 capture
```

The source ROM was read from the authorized NAS-backed SNES library:

```text
/Users/clawzai/FogCastMounts/SNES/ActRaiser.zip
```

The archive contained `ActRaiser.smc`, 1,049,088 bytes. RetroArch's verbose
log identified the loaded content as `ACTRAISER-USA` and reported `Checksum OK`.
The arm64 RetroArch/Snes9x run produced a fresh ShadowCast capture showing the
ActRaiser title screen, not the synthetic test pattern:

- capture: `artifacts/poc6/capture/retroarch-ActRaiser-hdmi.png`;
- evidence: `artifacts/poc6/retroarch-ActRaiser-hdmi-evidence.json`;
- sender report: `artifacts/poc6/retroarch-final-sender.json`.

Host sender observations:

```text
encoded_frames:       5,989
captured_frames:      5,989
capture_drops:        0
encode_errors:        0
packetization_errors: 0
udp_errors:           0
p95 encode:           7.55 ms
```

Target bridge observations from the same run:

```text
decoded_frames:       100
framebuffer_writes:   100
packet_errors:        117
```

The non-zero packet-error count is recorded rather than hidden. The receiver
recovered enough of the stream to decode and present the observed title screen,
but this run is not a clean production transport-quality result and does not
establish the POC6 latency target.

## M1 — target-side decode/display spike

**Passed as a disposable feasibility result.** The target decoded H.264 with
FFmpeg, converted frames to raw RGBA, and wrote them through the native
framebuffer presentation route while stock `Main_MiSTer` was restored after the
experiment. The target framebuffer was verified as 1920x1080, 32 bpp, stride
7680. ShadowCast 3 visibly captured the resulting output.

SDL/`ffplay` was not used as the display path: target SDL had no usable video
device. The disposable bridge instead uses the native `Main_MiSTer` command
FIFO plus `/dev/fb0`.

## M3 — host encode path

**Passed as a video-path checkpoint.** The Darwin capture backend now accepts a
`screen` selector and uses `AVCaptureScreenInput` for the host display. The
existing VideoToolbox/RTP sender then carries those frames to the verified
target bridge. The checkpoint was exercised with both a synthetic host window
and actual RetroArch ActRaiser content.

## Not yet accepted

The roadmap's full POC6 completion disposition still requires:

- M2: a productized managed target cast agent with deterministic start/stop;
- M4: session/API ownership of the host emulator, sender, and target cast
  lifecycle;
- M5: local-controller symmetry on the cast path;
- measured cast-path glass-to-glass latency (target p95 <= 120 ms, stretch <=
  80 ms);
- clean teardown evidence, including no orphaned target processes and return
  to FPGA mode;
- a catalog launch driven through the single session API rather than the
  disposable command-line spike.

Those items remain the next implementation stage. This document deliberately
does not claim that POC6 is complete.

## Verification

The repository verification run for this checkpoint passed:

```sh
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make test
git diff --check
```

The test command includes the race-enabled Go suite and the repository's
FogCast, POC1B, installer, restore, and target checks.

Stock `Main_MiSTer` was restored after the hardware experiments and verified
unchanged. No production Buildroot configuration was modified; the decoder
options are confined to the disposable development defconfig.

## Next decision

Continue with session-owned cast lifecycle integration before attempting to
close M2/M4/M5. Keep the validated POC4 RTP/H.264 transport as the baseline;
do not redesign transport until clean lifecycle and latency measurements show
that it is the bottleneck.
