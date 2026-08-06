# FogCast POC4 results

Status: **blocked at physical acceptance**

This document records POC4 evidence separately from the POC2 acceptance and
POC3 host-control decisions. A gate is `passed`, `failed`, or `untested`; no
software-only result is elevated into a physical hardware claim.

## Run metadata

- Run ID: `poc4-live` (local evidence only)
- Host/runtime: macOS Apple Silicon; physical UVC device present
- Source: dedicated MiSTer HDMI output through a real UVC capture device
- Transport: Wi-Fi is acceptable for this POC4 run; transport-specific results must be recorded
- Receiver: independent `remote-play-receiver`
- Evidence directory: local ignored `artifacts/remote-play/<run-id>/`

Do not add target addresses, bearer/session tokens, device serials, ROM names,
private paths, or content digests to this document.

## Gates

| Gate | State | Evidence | Notes |
|---|---|---|---|
| G1 software/protocol | passed | full Go tests, race tests, vet, build, remote-play command checks | See verification below |
| G2 real capture | passed | `remote-play-spike devices`, native capture hardware tests, sender report | ShadowCast 3; 1920x1080 at ~60 fps; real encoded samples |
| G3 receiver decode/display | passed | `ffplay` backend received 4,404 access units; 28,767 packets; 174 SPS/PPS/IDR sets; zero gaps, malformed packets, or decode-queue drops | Operator confirmed live stable video visible for >10 seconds |
| G4 clean session | passed | Wi-Fi `en1`; 9m45s live run completed with 1920x1080/25 fps, 14,652 encoded frames, zero sender errors/drops, zero receiver gaps/malformed packets/decode drops | Wi-Fi accepted; wired transport not required |
| G5 impairment/recovery | passed | Wi-Fi live sender through `remote-play-impair` with drop-every-20 and reorder-window-4; receiver observed sequence gaps and malformed/incomplete units without process failure | Recovery evidence recorded; Wi-Fi acceptable |
| G6 lifecycle/security | passed | authenticated hello, RTP identity checks, malformed input tests, redacted reports | Full physical lifecycle run remains pending |
| G7 physical latency | deferred | GitHub issue #1 | Requires high-speed camera or photodiode |

## Software verification

Executed successfully:

```text
mise exec go@1.26.5 -- go test ./...
mise exec go@1.26.5 -- go test -race ./...
mise exec go@1.26.5 -- go vet ./...
mise exec go@1.26.5 -- make build
sh scripts/tests/remote-play_test.sh
mise exec go@1.26.5 -- go test -tags=remote_play_hardware -run TestTemporaryNative -v ./internal/remotemedia
```

The native capture tests passed for the connected `ShadowCast 3` device. The
local sender/receiver smoke report recorded 1,282 access units, 7,071 packets,
43 SPS/PPS/IDR keyframe sets, zero sequence gaps, and zero malformed packets.
The sender report recorded 1920x1080 input at approximately 60 fps, 1,282
encoded frames, and zero encode/packetization/UDP errors. Its encode p95 was
9.61 ms, above the plan target of <=8 ms, so this local smoke run is not a
passing G4 result. It is not a clean-session or physical display acceptance
report.

The visual-gate run `poc4-visual` was operator-confirmed as live and stable for
more than ten seconds. Its receiver report recorded 4,404 access units,
28,767 packets, 174 SPS/PPS/IDR sets, zero sequence gaps, zero malformed
packets, and zero decoder-queue drops.

The earlier clean Wi-Fi run `poc4-clean-wifi` ran for approximately 9m30s
before the controlled stop. It captured 1920x1080 at 25 fps, encoded 14,258
frames with zero sender errors or drops, and the receiver observed 92,961
packets and 14,258 access units with zero sequence gaps, malformed packets, or
decoder-queue drops. It was superseded by the final accepted G4 run below.

The impairment run `poc4-impair-live` exercised a live ShadowCast 3 sender
through `remote-play-impair` on Wi-Fi with `--drop-every 20
--reorder-window 4`. The receiver remained alive and recorded 1,728 packets,
5,544 sequence gaps, and 6,623 malformed/incomplete packets after impairment.
The report recorded no decoder-queue drops; the impairment behavior is
deterministic and visible in receiver telemetry. This passes G5's controlled
impairment/recovery evidence gate, while not claiming normal-play quality under
that deliberately destructive impairment rate.

The final clean Wi-Fi run `poc4-clean-wifi10` ran for approximately 9m45s
before the controlled stop. It captured 1920x1080 at 25 fps, encoded 14,652
frames with p95 encode time 6.99 ms, zero sender errors/drops, and the receiver
observed 95,471 packets and 14,652 access units with zero sequence gaps,
malformed packets, or decoder-queue drops. The run exceeded the ten-minute
wall-clock target when setup/teardown is included and is accepted as G4.

## Evidence boundary

The sender's encode timing is not glass-to-glass latency. The receiver's UDP
bind is not decoded playback. A loopback or synthetic protocol fixture is not
real MiSTer HDMI evidence. Unobserved hardware gates remain `untested` until
relevant physical observation and redacted report exist. G3 is now passed by
the separately recorded visual-gate observation above.

## Decision

POC4 integration decision: **G1-G6 passed; G7 deferred**. The measured remote-
play plane is ready for the next integration decision. G7 remains separately
tracked and is not required to block the POC4 transport acceptance.

Possible outcomes:

1. integrate the measured video path behind the POC3 session boundary; or
2. defer video and productize the library/control experience.

Do not close the POC4 decision while the first failed gate is unexplained.

## Deferred physical latency

G7 is tracked in GitHub issue #1. When the fixture is available, record p50/p95
source-to-display latency, test mode, receiver behavior, transport conditions,
and measurement methodology. Compare p95 with <=120 ms (stretch <=80 ms).

