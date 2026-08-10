# FogCast authorized macOS capture launch

This runbook is for the physical Darwin host capture path only. It fixes the
launcher-context problem observed with the Codex app-server shell by giving
FogCast a stable signed bundle identity. It does not change the MiSTer target,
Stage A acceptance, or the public host/target protocol.

## Why the bundle is required

macOS Camera and Microphone consent belongs to the responsible application and
its code-signing identity through the TCC privacy boundary. Genki's permission does not grant access to
FogCast, FFmpeg, or an ad-hoc child binary. The FogCast worker must therefore
run from the signed `com.fogcast.host` helper after the operator grants that
helper Camera and Microphone access.

The ordinary `make build` target deliberately produces a cgo-disabled
`bin/fogcast-api` for non-hardware checks. It cannot open the AVFoundation /
VideoToolbox backend. Use the explicit helper build below for physical
capture.

## Build the helper

Configure the signing identity in the local environment only. Do not write it
to a tracked file, command transcript, target configuration, or evidence
receipt:

```sh
export FOGCAST_SIGNING_IDENTITY
test -n "$FOGCAST_SIGNING_IDENTITY"
make build-fogcast-host \
  FOGCAST_HOST_OUTPUT="$PWD/bin/FogCastHost.app"
```

The build refuses to replace an existing bundle unless
`FOGCAST_HOST_REPLACE=1` is set explicitly.

The build fails if `FOGCAST_SIGNING_IDENTITY` is empty or the host is not
Darwin. It builds `cmd/fogcast-api` with `CGO_ENABLED=1`, embeds
`NSCameraUsageDescription` and `NSMicrophoneUsageDescription`, and signs the
bundle. Inspect the result without recording the identity string:

```sh
codesign --display --verbose=4 bin/FogCastHost.app
codesign --display --entitlements :- bin/FogCastHost.app
plutil -p bin/FogCastHost.app/Contents/Info.plist
```

The plist must report bundle identifier `com.fogcast.host`, executable
`fogcast-api`, and both usage descriptions.
The hardened-runtime signature must also carry
`com.apple.security.device.camera`; the microphone entitlement is intentionally
not included until FogCast has a product audio-input worker.

## Grant and launch

1. On the first helper launch, the AVFoundation preflight calls
   `requestAccessForMediaType` and macOS presents the Camera consent dialog.
   Accept it for **FogCast Host Capture**. If the prompt is unavailable or a
   prior denial is cached, open System Settings → Privacy & Security → Camera
   and enable **FogCast Host Capture**. Repeat under Microphone when an audio
   worker is enabled. Grant Screen Recording only when using the separate
   screen-capture path; ShadowCast UVC capture is a camera device.
2. Quit stale FogCast/Genki capture owners before the run.
3. Launch the executable inside the signed bundle, not a separately rebuilt
   binary from the Codex shell:

```sh
bin/FogCastHost.app/Contents/MacOS/fogcast-api \
  --config /absolute/path/to/private-fogcast.toml \
  --listen 127.0.0.1:8787
```

The config path is intentionally private and must not be copied into source,
logs, or evidence.

## Verify the capture boundary

Use the named device reported by AVFoundation, normally `ShadowCast 3`, and
keep Sonic active on the disposable target. The adapter now performs a Camera
authorization preflight and fails with the System Settings action when the
status is denied or unresolved. After authorization, it waits up to two
seconds for the first encoded frame; a timeout reports that the authorized
helper or UVC ownership must be checked instead of silently running at zero
frames.

For the FogCast API session, require the capture report to show a positive
`captured_frames` value and zero startup/runtime authorization errors. Preserve
only redacted counters, code-signature hashes, and report hashes in ignored
local artifacts. A Terminal-launched `remote-play-spike` remains a useful
comparator, but it is not a substitute for the signed FogCast helper smoke
test.

If a signed helper and the Terminal comparator both regress to zero, return to
the device/HDMI investigation. An unchanged Codex-shell capture is not new
evidence about MiSTer source audio.
