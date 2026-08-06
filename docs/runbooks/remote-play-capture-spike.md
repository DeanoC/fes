# Remote-play HDMI capture and RTP/H.264 sender spike

This is an isolated host-side proof of the MiSTer HDMI-output-to-Mac capture and low-latency H.264/RTP sender path. It does not modify `Main_MiSTer`, FPGA sources, target launch, cache, or the existing host API.

## Scope and evidence boundary

The sender accepts only a real macOS AVFoundation video capture device. It never synthesizes frames. The native adapter captures a UVC HDMI input, records a monotonic timestamp at the AVFoundation callback, encodes with VideoToolbox H.264, converts VideoToolbox AVCC output to Annex-B, and packetizes RFC 6184 H.264 over UDP.

Local tests prove RTP/H.264 framing, AVCC conversion, timestamp conversion, bounded queue behavior, control-message framing/authentication, metrics, command validation, and (when the named device is present) a native open/start/encode/close smoke path. They do not prove an attached MiSTer HDMI signal, supported capture mode under the operator's chosen format, sustained H.264 hardware performance, independent receiver decode, wired-LAN loss behavior, or glass-to-glass latency.

## Hardware and OS assumptions

- macOS 26.5.2 on the development host (Darwin 25.5.0, arm64; Mac mini Mac16,10 with Apple M4 and 16 GB RAM). The native build links AVFoundation, CoreMedia, CoreVideo, VideoToolbox, and Foundation. Confirm the operator's runtime OS and hardware before treating this as a portable guarantee.
- MiSTer has a stable HDMI output and an operator-approved test pattern/core.
- A UVC HDMI capture device is connected to the Mac, with a mode sustaining the selected resolution and frame rate. USB 3 bandwidth is expected for 720p/60 or similar modes.
- The host has permission to access the camera/capture device when macOS requests it.
- The development Mac currently enumerates an external device named `ShadowCast 3` (unique ID is intentionally omitted from this runbook). The runtime command still requires the operator to choose and verify the physical device; no device name is hard-coded into production startup.
- The receiver is an independent RTP/H.264 implementation listening on the configured UDP port. A receiver/display is intentionally not included in this sender card.
- Wired Ethernet is the baseline for latency/loss measurements. Wi-Fi is a separate experiment.
- Runtime force-IDR is not exposed by this native adapter; the VideoToolbox session is configured for real-time encoding, no frame reordering/B-frames, a short GOP, and a 500 ms keyframe interval. Recovery therefore relies on the next periodic IDR and this limitation must remain visible in reports.

## Build and enumerate devices

Use the repository-pinned Go toolchain:

```sh
mise exec go@1.26.5 -- go build -trimpath -o bin/remote-play-spike ./cmd/remote-play-spike
bin/remote-play-spike devices
```

`devices` prints AVFoundation's capture-device name and unique ID as JSON. Use the unique ID where possible. If no physical device is present, enumeration fails; do not replace it with synthetic frames.

Record a redacted capability report before a run:

```sh
mkdir -p artifacts/remote-play/<run-id>
bin/remote-play-spike probe \
  --output artifacts/remote-play/<run-id>/capabilities.json
```

Do not commit reports containing device serials, private paths, target addresses, tokens, ROM names, or content digests. The `probe` result marks the selected backend and explicit unknowns; it is not a hardware pass by itself.

## Run the sender

Build and start the independent receiver first. The receiver validates the
authenticated control session, RTP packet structure, SSRC/generation, sequence
continuity, FU-A reassembly, and marker-delimited access units. It reports
decoded/display state as explicitly unavailable until a native decoder/display
backend is configured; binding a UDP socket is not treated as playback evidence.

```sh
mise exec go@1.26.5 -- go build -trimpath -o bin/remote-play-receiver ./cmd/remote-play-receiver
bin/remote-play-receiver \
  --rtp 0.0.0.0:5004 \
  --control 0.0.0.0:5005 \
  --session operator-run-20260805 \
  --generation 1 \
  --token 'LOCAL_RUNTIME_SECRET' \
  --metrics artifacts/remote-play/<run-id>/receiver.json
```

The receiver also supports an optional local `ffplay` display backend:

```sh
bin/remote-play-receiver \
  --rtp 0.0.0.0:5004 \
  --control 0.0.0.0:5005 \
  --session operator-run-20260805 \
  --generation 1 \
  --token 'LOCAL_RUNTIME_SECRET' \
  --decode ffplay \
  --metrics artifacts/remote-play/<run-id>/receiver.json
```

It consumes validated Annex-B access units after the authenticated
`MEDIA_HELLO`. The receiver report marks the decoder as configured, but G3
still requires operator-confirmed displayed SPS/PPS + IDR and steady playback.

The token is a local runtime secret and must not be placed in repository files
or committed reports. Start the sender only after the receiver's control and
RTP endpoints are ready. Then run:

```sh
bin/remote-play-spike sender \
  --capture-device 'AVFOUNDATION_UNIQUE_ID_OR_NAME' \
  --rtp 192.0.2.10:5004 \
  --control 192.0.2.10:5005 \
  --session operator-run-20260805 \
  --generation 1 \
  --width 1280 \
  --height 720 \
  --fps 60000/1001 \
  --bitrate 8000000 \
  --gop 30 \
  --metrics artifacts/remote-play/<run-id>/sender.json
```

The sender generates a fresh token if `--token` is omitted and never prints it. Session and generation are control-plane identifiers; RTP uses a fresh random nonzero SSRC and initial sequence number per process. When `--control` is supplied, the sender opens the authenticated TCP control channel, sends `MEDIA_HELLO`, emits one `MEDIA_REPORT` per second, answers `PING`, counts `KEYFRAME_REQUEST` failures when force-IDR is unsupported, and stops on `STOP`.

Stop with Ctrl-C. The report includes detected resolution, source FPS, encoded FPS, encoded bitrate, encode p50/p95/max timing, packet/frame/keyframe counts, bounded queue depth/high-water, capture/queue drops, encode/packetization/UDP errors, and shutdown reason.

The sender exits clearly when `--capture-device` is missing, the device cannot be found/opened, macOS is not the runtime OS, the native adapter cannot start, or the RTP destination cannot be reached. There is no synthetic fallback.

## Wire contract

- RTP version 2, no CSRC or extension, dynamic payload type 96, H.264 clock rate 90 kHz.
- Complete UDP datagrams are capped at 1,200 bytes. NAL units that fit use single-NAL packets; larger NAL units use FU-A.
- Every packet in an access unit shares one timestamp. The marker bit is set only on its final packet.
- Sequence numbers are 16-bit and wrap naturally. SSRC is random/nonzero per sender generation.
- VideoToolbox's AVCC length-prefixed output is copied and converted NAL-by-NAL to Annex-B. SPS/PPS are extracted from the format description and prepended to IDR access units when absent.
- Capture/encode is bounded at one pending frame. AVFoundation late frames and native queue replacement are counted separately. The sender does not allow a software queue to grow without bound.

## Measurement procedure

1. Start the independent receiver and confirm its UDP bind.
2. Connect MiSTer HDMI to the capture device and display a deterministic flashing/timestamp pattern.
3. Start the sender and verify the first SPS/PPS + IDR reaches the receiver.
4. Run a clean wired session for ten minutes. Save the JSON report and receiver telemetry.
5. Separately inject loss/reordering in a loopback or controlled network test. The sender's metrics do not claim receiver-side loss; receiver reports must record packet gaps, corrupted access units, recovery, and keyframe age.
6. For physical glass-to-glass latency, measure the flashing pattern with a high-speed camera or photodiode. Software capture/encode/send timings are not glass-to-glass latency.

Target values from the architecture plan are targets, not assumed results: host encode p95 <=8 ms, clean wired media drop rate <0.5%, and initial glass-to-glass p95 <=120 ms (stretch <=80 ms). Mark each as pass, fail, or untested using the raw measurements.

For deterministic local impairment, build `remote-play-impair` and place it
between the sender and receiver:

```sh
mise exec go@1.26.5 -- go build -trimpath -o bin/remote-play-impair ./cmd/remote-play-impair
bin/remote-play-impair --listen 127.0.0.1:5604 --forward 127.0.0.1:5504 \
  --drop-every 20 --reorder-window 4
```

The harness validates minimum RTP packet length, drops every Nth valid packet,
and reverses bounded packet windows. Its JSON report is supporting evidence;
the receiver report remains authoritative for sequence gaps and incomplete
access units.

## Focused verification

```sh
mise exec go@1.26.5 -- go test -race ./internal/remotemedia ./cmd/remote-play-spike
mise exec go@1.26.5 -- go vet ./internal/remotemedia ./cmd/remote-play-spike
mise exec go@1.26.5 -- go build ./cmd/remote-play-spike
mise exec go@1.26.5 -- go test -tags=remote_play_hardware -run TestTemporaryNative -v ./internal/remotemedia
```

The native files are built under the normal macOS cgo path. The temporary native smoke tests use the operator's named physical device when present and skip when it is unavailable; they must not be confused with a MiSTer HDMI or sustained-performance acceptance run. A successful build proves headers/linkage only.

## Known limitations

- The current card implements a host-side sender and an independent receiver
  ingest/report boundary. A native H.264 decoder/display backend is still not
  configured by default; receiver ingest success must not be reported as
  decoded/displayed video.
- The native adapter uses an AVFoundation capture source and a VideoToolbox compression session, but the source's active capture mode should be confirmed by an operator before claiming a 60 fps result.
- Keyframe request signaling is part of the transport/control package, but this adapter reports runtime force-IDR as unsupported and relies on its periodic GOP.
- No current run in this repository proves physical HDMI, receiver decode, wired-LAN loss/recovery, target-side decode, or glass-to-glass latency.

## Files and scope

The spike is limited to:

- `cmd/remote-play-spike/`
- `internal/remotemedia/`
- this runbook

`Main_MiSTer`, FPGA sources, `internal/mister`, target image/init scripts, cache semantics, launch commands, and existing host API routes remain untouched.
