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
helper Camera access. The same signed identity owns the separate FogCast
audio-input worker when the ShadowCast UAC source is enabled.

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
bundle. The entitlements include both camera and
`com.apple.security.device.audio-input`; no certificate or identity is
recorded in the repository. Inspect the result without recording the identity
string:

```sh
codesign --display --verbose=4 bin/FogCastHost.app
codesign --display --entitlements :- bin/FogCastHost.app
plutil -p bin/FogCastHost.app/Contents/Info.plist
```

The plist must report bundle identifier `com.fogcast.host`, executable
`fogcast-api`, and both usage descriptions.
The hardened-runtime signature must carry
`com.apple.security.device.camera` and
`com.apple.security.device.audio-input`. Screen Recording is a separate TCC
grant for the `host_output` source; the current SDK has no usage-description
key for it, so the helper does not invent one.

## Grant and launch

1. On the first helper launch, the AVFoundation preflight calls
   `requestAccessForMediaType` and macOS presents the Camera consent dialog.
   Accept it for **FogCast Host Capture**. If the prompt is unavailable or a
   prior denial is cached, open System Settings → Privacy & Security → Camera
   and enable **FogCast Host Capture**. When the `shadowcast_uac` worker is
   enabled, repeat under Microphone for the same signed helper. Grant Screen
   Recording only for the `host_output` path; ShadowCast UVC capture is a
   camera device. Genki's microphone grant does not transfer to FogCast.
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
keep Sonic active on the disposable target. Record the normalized endpoint
digest and use the exact named/hash-bound selection; do not infer `none:0` or
reuse an index after a device list change. The adapter performs Camera and
Microphone authorization preflight and fails with the relevant System Settings
action when a status is denied or unresolved. After authorization, it waits up
to two seconds for the first encoded frame and audio callback; a timeout
reports that the authorized helper or UVC ownership must be checked instead of
silently running at zero frames.

For a muted-speaker comparator, start the signed helper and the local
authenticated audio receiver while Sonic remains active, then mute the Mac
mini output device. A successful run must still report positive PCM counters,
the configured 48 kHz PCM16 format, and the endpoint digest. Keep Genki open
only for the documented concurrent-ownership experiment; do not terminate it
or assume that its application-rendered audio is the raw ShadowCast endpoint.

For the FogCast API session, require the capture report to show a positive
`captured_frames` value and zero startup/runtime authorization errors. Preserve
only redacted counters, code-signature hashes, and report hashes in ignored
local artifacts. A Terminal-launched `remote-play-spike` remains a useful
comparator, but it is not a substitute for the signed FogCast helper smoke
test.

If a signed helper and the Terminal comparator both regress to zero, return to
the device/HDMI investigation. An unchanged Codex-shell capture is not new
evidence about MiSTer source audio.
