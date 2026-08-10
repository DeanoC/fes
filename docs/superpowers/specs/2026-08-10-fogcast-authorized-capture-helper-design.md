# FogCast Authorized macOS Capture Helper Design

## Problem

The native AVFoundation worker receives no video callbacks when launched from
the Codex app-server shell, while the identical hash-bound worker receives
frames when launched from signed Terminal. The named ShadowCast CoreAudio
endpoint likewise produces non-zero PCM only under the authorized launcher
context. The MiSTer source and HDMI path are not implicated. The current
capture code does not report the effective AVFoundation authorization state,
and the normal Makefile build intentionally emits a cgo-disabled
`fogcast-api` that cannot use the Darwin backend.

## Goals and boundaries

The change will make the FogCast host capture path operable and diagnosable
under a stable authorized macOS identity. It will:

1. expose a Darwin authorization preflight that reports camera permission
   state before opening an external UVC device;
2. fail with an actionable error for denied or unresolved authorization rather
   than silently waiting with zero frames;
3. provide a cgo-enabled macOS helper bundle build with camera/microphone usage
   descriptions and an operator-supplied signing identity; and
4. document the grant/launch workflow and the required hardware build.

It will not add a raw-audio worker, change the public host/target protocol,
modify target code or images, change Stage A acceptance, alter `rtmp-services/`,
or commit signing credentials/private configuration. The raw UAC evidence
remains a diagnostic comparator; this change targets FogCast's existing
video-only media path.

## Design

### Authorization preflight

The Darwin capture adapter will query
`+[AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo]` before
device enumeration/open. A new small C bridge returns the status as a stable
integer. For `notDetermined`, the signed helper calls
`requestAccessForMediaType:completionHandler:` with a bounded wait so macOS can
present the first-run Camera consent dialog; a raw executable without the
helper usage-description plist fails with an actionable error. Go maps
`authorized` to success and returns a typed actionable error for unresolved,
denied, and restricted states, including the helper bundle's Camera permission
requirement. Screen capture keeps its existing path and does not require the
external-device camera check.

The adapter will not attempt to manufacture permissions or bypass TCC. The
helper bundle is responsible for the user-visible grant. The command will
therefore fail early with the exact next action instead of producing a
zero-frame report after an apparently successful session open.

### Stable helper bundle

The repository will provide a Darwin-only build script and template bundle:

- build `cmd/fogcast-api` with `CGO_ENABLED=1`, `GOOS=darwin`, and `GOARCH=arm64`;
- place the binary at `FogCastHost.app/Contents/MacOS/fogcast-api`;
- embed `NSCameraUsageDescription` and `NSMicrophoneUsageDescription` in the
  bundle `Info.plist`; and
- sign the hardened-runtime bundle with
  `com.apple.security.device.camera`; the microphone entitlement is deferred
  until FogCast has a product audio-input worker; and
- sign the bundle with `FOGCAST_SIGNING_IDENTITY`, supplied only in the local
  environment. The script fails if no identity is supplied, so an ephemeral
  ad-hoc binary is not presented as a durable authorization identity.

The helper is launched as the bundle executable, and the operator grants
Camera/Microphone access to that bundle in System Settings. The launch command
and the bundle's identity are recorded in the local diagnostic receipt, never
in tracked private configuration.

### Build and failure behavior

The existing generic `make build` target remains suitable for non-hardware
checks and continues to emit its cgo-disabled binary. A separate explicit
`build-fogcast-host` target invokes the helper script. The sender's existing
metrics remain authoritative; authorization errors are returned before the
capture session starts, and the existing capture statistics still describe
post-start runtime failures.

## Alternatives considered

1. **Keep launching raw binaries from Terminal.** This is a valid diagnostic
   workaround but does not give FogCast a stable responsible-process identity
   for unattended use.
2. **Add a raw CoreAudio worker to FogCast.** This would expand the product
   media contract and is unnecessary to fix the proven AVFoundation video
   authorization boundary.
3. **Use an unsigned/ad-hoc helper bundle.** This could be useful for a one-off
   local test but would make TCC authorization depend on changing code hashes.

The stable signed helper plus explicit preflight is the smallest durable fix.

## Testing and acceptance

- Add unit coverage for status-to-error mapping and the non-Darwin/cgo-disabled
  build path.
- Add a script/package smoke test that rejects a missing signing identity and
  verifies the bundle's usage descriptions and executable layout.
- Run `go test ./...`, `go vet ./...`, the Darwin cgo build, and shell/static
  checks.
- On the authorized host, launch the helper with the named `ShadowCast 3`
  endpoint while Sonic 2 is active and require `captured_frames > 0`; retain
  the helper identity and report hash. This is host integration evidence only
  and does not modify Stage A acceptance.
