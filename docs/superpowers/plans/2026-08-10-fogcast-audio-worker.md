# FogCast Audio Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox ( - [ ] ) syntax for tracking.

**Goal:** Add an opt-in, authenticated FogCast audio path with a separate host audio worker, pluggable ShadowCast UAC and host-output sources, and a target audio playout handoff seam while preserving the accepted Stage A boundary.

**Architecture:** Keep CaptureSource and the existing H.264 sender unchanged for backwards-compatible video-only sessions. Add an AudioSource contract and a separate AudioSender/AudioReceiver pair using session-authenticated control plus a source-bound RTP stream carrying PCM16 at 48 kHz with its own SSRC, RTP port, control port, and lifecycle handle. The first deterministic source is the named/hash-bound ShadowCast UAC endpoint; the product source is a host-output adapter using the same interface. The target audio bridge is initially a retained-testbed diagnostic. Production host_cast audio requires a native coordinator-granted composite lease covering presentation, audio route, both workers, and the transport, plus a real sink readiness/drain/reset contract. No physical acceptance is claimed until that coordinator/sink gate passes.

**Tech Stack:** Go 1.26.5, Go standard library RTP/control code, cgo Objective-C AVFoundation/CoreMedia for ShadowCast UAC, ScreenCaptureKit/CoreMedia for host-output capture, signed macOS helper bundle, Linux ARMv7 diagnostic target bridge, and existing FogCast session/generation lifecycle. Hardware FFmpeg/ALSA playout belongs only to the future coordinator-owned sink adapter and is not implemented by this plan.

## Global Constraints

- Do not modify or downgrade accepted Stage A evidence; new product-audio evidence is a separate dated record with Software-tested, Reproducible, HIL-observed, and Accepted labels.
- Leave rtmp-services/ and the two pre-existing unrelated documentation edits untouched.
- Keep the public host/target protocol independent of Linux paths, /dev/fb0, /dev/MiSTer_cmd, FFmpeg command lines, and target process layout; audio bridge arguments are target-private configuration.
- Keep the existing video-only configuration and ManagedSender behavior valid when audio is disabled or absent.
- Use PCM16 for the first end-to-end implementation so the repository needs no new codec dependency; record Opus as a later bandwidth optimization, not as an unimplemented requirement in this plan.
- Use the already pinned golang.org/x/text v0.40.0 unicode/norm NFC implementation for digest canonicalization; do not add a dependency.
- Bind physical audio sources by stable CoreAudio/ScreenCaptureKit identity plus a recorded hash; do not select none:0 by assumption and do not put private identifiers or credentials in tracked files.
- Use a canonical endpoint digest of normalized UID, normalized name, sample rate, channel count, and encoding; record only the digest in tracked evidence.
- A ShadowCast capture must never be looped back into the same MiSTer HDMI output for HIL; use it for host-worker/transport loopback and use host-output audio for the physical host_cast target gate.
- The public cast admission descriptor carries only a versioned optional media set (video/audio enabled); codec and sample-format capabilities remain inside the private media control contract.
- RTP has session-authenticated control and a bound source address after hello, but no per-packet integrity. The trusted-network boundary is explicit; hostile-network protection requires a separate reviewed SRTP/AEAD design.
- No target image mutation, deployment, reboot, or hardware acceptance is authorized by this plan. Any disposable-kit HIL operation uses the existing operator authorization, identity-resolution, rollback, provenance, and evidence gates.
- The root coordinator owns integration in an isolated worktree/branch rooted at base 95a0a739: codex/fogcast-audio-worker. The current main worktree retains the user’s unrelated documentation edits. Writable files are disjoint per task; reviewers are read-only and do not edit the owner’s files.
- Each implementation milestone receives an independent Vega review before the next cross-boundary milestone: after Task 2, after Task 3, after Task 5, and before final integration. Critical/Important findings are resolved in the owning worktree.

---

### Task 0: Gate target audio-route feasibility and coordinator ownership

**Owner:** Root coordinator with Sol architecture decision; HIL verifier supplies read-only target observations; no target mutation.

**Files:**
- Create: docs/superpowers/specs/2026-08-10-fogcast-audio-worker-design.md
- Create: docs/audio-worker/target-sink-feasibility-2026-08-10.md

- [ ] **Step 1: Inspect the current target audio capability**

On the designated disposable kit, record only the non-secret device class, available sink backends, and whether the target reports a non-Dummy audio route. Do not alter the image or start a host audio loop.

- [ ] **Step 2: Define the coordinator lease contract**

The native coordinator must atomically grant a composite host_cast lease naming the session, generation, mode, presentation surface, audio route, transport sockets, and both workers. Activation is visible only after video worker, audio worker, and AudioSink report ready. Either worker’s death enters recovery, stops/reaps its sibling, mutes/drains/resets the sink with independent deadlines, and retains retryable ownership when cleanup is incomplete.

- [ ] **Step 3: Define media admission negotiation**

Add a backward-compatible optional CastMediaSet to cast start with fields Version, Video, and Audio. Legacy clients omit the field and deterministically request video-only. An audio-enabled host must send audio=true; the target acknowledges the exact media set before reporting active. Codec/sample-format details remain private to the authenticated media hello.

The wire rules are exact: omitted media means legacy video-only; a present object must have Version=1, Video=true, and Audio either false or true; Version=0, unknown versions, and Video=false are rejected. The status projection echoes the acknowledged set and capability readiness when the coordinator supports it.

- [ ] **Step 4: Record the gate disposition**

If there is no coordinator-owned non-Dummy sink contract in the current target, mark the target bridge work as retained-testbed software only. Do not claim that internal/cast.Controller alone owns host_cast audio or that a PCM dump is physical HDMI evidence. The next safe experiment is a known sound-capable HDMI sink or independent analyzer.

- [ ] **Step 5: Write the durable audio design record**

Record the source distinction, separate worker boundary, private audio RTP/control ports, PCM16 packet contract, AudioSink/AudioPlayout contract, target coordinator lease requirements, media admission rules, permission split, no-loopback HIL rule, trusted-network RTP limitation, canonical endpoint/display digests, and evidence gates. Link the existing Stage A audio records without editing their historical measurements.

- [ ] **Step 6: Obtain architecture review before platform or protocol code**

Have Sol review the design for lifecycle, target ownership, portability, security, and protocol compatibility. Have Vega independently review the exact design diff. Record base commit, reviewer model/fallback, findings, and dispositions in the handoff.

### Task 1: Freeze the audio architecture and source/target contracts

**Owner:** Root coordinator; Sol architecture review before implementation; Vega independent review after the draft.

**Files:**
- Create: internal/remotemedia/audio.go
- Create: internal/remotemedia/audio_test.go
- Create: internal/remotemedia/audio_sink.go
- Create: internal/remotemedia/audio_sink_test.go

**Interfaces:**
- AudioFormat contains SampleRate, Channels, Encoding, and FrameSamples.
- AudioSample contains CaptureMonoNS, interleaved PCM16 bytes, frame count, and the effective AudioFormat.
- AudioStats reports source format, callbacks, non-zero samples, dropped frames, queue depth/high-water, runtime errors, and shutdown state.
- AudioFrame contains RTP Sequence, RTP Timestamp, receiver monotonic timestamp, interleaved PCM16 bytes, frame count, and AudioFormat. Source CaptureMonoNS is not placed on the wire.
- AudioSink is exactly:

~~~go
type AudioSink interface {
	Open(context.Context, AudioSinkConfig) error
	Ready(context.Context) error
	Write(context.Context, AudioFrame) error
	Drain(context.Context) error
	Mute(context.Context) error
	Reset(context.Context) error
	Stats() AudioSinkStats
	Close(context.Context) error
}
~~~

- AudioPlayout owns sequence extension, bounded jitter/reordering, a 48 kHz playout clock, five-millisecond frame cadence, startup prebuffer, sequence-gap-to-silence policy, bounded drift correction, and underrun/overrun metrics. AudioReceiver does not own sink metrics.
- AudioPlayout is exactly:

~~~go
type AudioPlayout interface {
	Push(context.Context, AudioFrame) error
	Run(context.Context, AudioSink) error
	Stop(context.Context) error
	Stats() AudioPlayoutStats
}
~~~

- AudioSink readiness and cleanup are caller-bounded by context deadlines; Close is idempotent and must return within its supplied context.
- AudioSinkConfig identifies only target-private sink capabilities; it includes no public Linux device path or process layout.
- AudioSource is exactly:

~~~go
type AudioSource interface {
	Start() error
	Next(context.Context) (AudioSample, error)
	Stats() AudioStats
	Close() error
}
~~~

- AudioSourceKind has the values shadowcast_uac and host_output.
- AudioSourceConfig contains source kind, endpoint name/UID/hash, display identity when applicable, sample rate, channels, frame size, and an enabled flag.
- AudioTransportConfig contains RTP destination/control addresses, SSRC, MTU, and the private format capability version.
- AudioConfig contains Enabled, Source AudioSourceConfig, and Transport AudioTransportConfig. A zero AudioConfig is disabled.
- AudioSourceFactory is exactly func(AudioSourceConfig) (AudioSource, error).
- The canonical endpoint digest is SHA-256 over a binary, length-prefixed encoding:

~~~text
domain = "fogcast-audio-endpoint-v1" followed by NUL
field 1 = uint32 big-endian byte length + NFC-normalized exact UID bytes (no whitespace collapse)
field 2 = uint32 big-endian byte length + NFC-normalized, whitespace-collapsed display name
field 3 = uint32 big-endian sample rate
field 4 = uint32 big-endian channel count
field 5 = uint32 big-endian byte length + UTF-8 string "pcm_s16le"
~~~

The digest is recorded as sha256:<hex>. Display identity uses a separate domain, fogcast-display-v1, with persistent fields in this exact order: NFC-normalized hardware UUID, EDID vendor, EDID model, and EDID serial (empty serial is explicit). Bounds and scale are selection context stored beside the digest, not identity material. String fields are uint32-length-prefixed UTF-8; numeric fields are fixed-width big-endian. When no persistent UUID or EDID serial exists, require explicit operator selection and label the resulting digest a configuration fingerprint. Add golden vectors for both schemas.

- [ ] **Step 1: Write the failing contract tests**

Add tests for:

~~~go
func TestAudioFormatRejectsNonPCM16OrInvalidLayout(t *testing.T) {}
func TestAudioConfigRejectsUnknownSourceAndMissingIdentity(t *testing.T) {}
func TestAudioConfigAllowsDisabledZeroValue(t *testing.T) {}
func TestAudioSampleRejectsPartialPCMFrame(t *testing.T) {}
func TestAudioSinkRequiresBoundedReadyDrainMuteReset(t *testing.T) {}
func TestAudioEndpointDigestIsCanonicalAndStable(t *testing.T) {}
func TestAudioDisplayDigestHasSeparateDomainAndGoldenVector(t *testing.T) {}
~~~

The tests must require 48,000 Hz for the first implementation, one or two channels, positive frame size, and len(PCM) == Frames*Channels*2.

- [ ] **Step 2: Run the focused test and confirm the contract is absent**

Run: mise exec go@1.26.5 -- go test ./internal/remotemedia -run TestAudio -count=1

Expected: compile failure because the audio types and validation functions do not yet exist.

- [ ] **Step 3: Implement the platform-neutral contract**

Implement the types, constants, validation, defensive PCM copying, and AudioStats helpers. Keep all Darwin and target code behind these interfaces.

- [ ] **Step 4: Run the focused test**

Run: mise exec go@1.26.5 -- go test ./internal/remotemedia -run TestAudio -count=1

Expected: PASS.

### Task 2: Add audio configuration and session composition inputs

**Owner:** Root coordinator.

**Files:**
- Create: protocol/media.go
- Create: protocol/media_test.go
- Modify: fogcast/config.go
- Modify: fogcast/config_test.go
- Modify: fogcast/service.go
- Test: fogcast/service_test.go
- Modify: host/client.go
- Modify: host/client_test.go
- Modify: internal/httpapi/content.go
- Modify: internal/httpapi/cast.go
- Modify: internal/httpapi/cast_test.go
- Modify: internal/cast/controller.go
- Modify: internal/cast/controller_test.go
- Modify: cmd/fogcast-api/main.go
- Test: cmd/fogcast-api/main_test.go

**Interfaces:**
- Parse an optional [media.audio] table into MediaConfig.Audio without changing existing video keys; normalize source fields separately from transport/auth fields.
- MediaConfig.Audio.Enabled controls whether FogCast opens an audio worker.
- MediaConfig.Audio.Source.Kind is shadowcast_uac or host_output.
- MediaConfig.Audio.Transport.RTPDestination and ControlAddress are distinct from video addresses.
- Add CastMediaSet and an optional CastStartWithMedia path carrying only Version, Video, and Audio booleans. CastStatus echoes an acknowledged media set and a capability/readiness projection when present. Keep Client.CastStart and legacy target peers video-only when the optional descriptor is omitted.
- Define CastMediaSet, CastMediaCapabilities, and CastStatusMedia in protocol/media.go as the single wire representation. host.CastStatus and internal/cast.Status project these types rather than defining duplicate JSON structs.
- The exact JSON shape is media: {version: 1, video: true, audio: false} on start and media: {version: 1, video: true, audio: false, ready: true, capabilities: {version: 1, video: true, audio: true}} on an acknowledged status. Legacy status omits media entirely. No codec, device path, sink name, or private process identifier appears in this object.
- The only supported media-set version is 1; an omitted media object means legacy video-only, version 0 is rejected when the object is present, unknown versions are rejected, Video must be true, and Audio-only is invalid for this path.

- [ ] **Step 1: Add failing TOML and composition tests**

Add a valid configuration fixture with:

~~~toml
[media]
enabled = true
session = "session-1"
ssrc = 42
rtp_listen = "127.0.0.1:5000"
rtp_destination = "127.0.0.1:5001"
capture_device = "screen"

[media.audio]
enabled = true
source = "shadowcast_uac"
device = "ShadowCast 3"
device_uid = "uid-redacted-in-test"
device_hash = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
rtp_destination = "127.0.0.1:5101"
control_address = "127.0.0.1:5102"
ssrc = 43
sample_rate = 48000
channels = 2
frame_samples = 240
mtu = 1200
~~~

Assert that unknown audio fields, duplicate/zero SSRCs, invalid addresses, invalid source names, and missing endpoint identity are rejected. Add a composition test proving audio is not opened when enabled = false.

- [ ] **Step 2: Run the focused tests before implementation**

Run: mise exec go@1.26.5 -- go test ./protocol ./host ./fogcast ./internal/cast ./internal/httpapi ./cmd/fogcast-api -run 'Test.*Audio|Test.*Media|TestCast' -count=1

Expected: FAIL because the nested audio configuration and factory wiring do not exist.

- [ ] **Step 3: Implement normalized audio configuration**

Add a nested fileAudio parser and normalizeAudio. Require stable endpoint identity for shadowcast_uac, require screen:<display> or an explicit display identity for host_output, validate private RTP/control addresses, constrain the first path to 48 kHz PCM16, and reject an audio SSRC equal to the video SSRC.

- [ ] **Step 4: Add injectable audio-source construction**

Extend compositionDeps with an audioSourceFactory accepting AudioSourceConfig and add withAudioSourceFactory. Keep defaultCaptureSource and all video test factories unchanged. The default factory returns an actionable non-Darwin/unavailable error rather than synthesizing samples.

- [ ] **Step 5: Add backward-compatible media admission**

Add the optional media object to host/client CastStartWithMedia, fogcast.Service, the CastController interface, and target HTTP cast-start parsing. The production HTTP path rejects audio=true when the target cannot advertise the coordinator/sink capability and echoes the exact acknowledged set in CastStatus. Keep old CastStart callers and video-only fixtures unchanged.

- [ ] **Step 6: Add a diagnostic-only bridge admission path**

Keep coordinator-less retained-testbed audio reachable only through an explicit target-private diagnostic command/configuration that starts remote-play-audiobridge with a manually supplied session/token file. It is not exposed by the public cast-start route and is never reported as an active host_cast audio lease.

- [ ] **Step 7: Run focused configuration and composition tests**

Run: mise exec go@1.26.5 -- go test ./protocol ./host ./fogcast ./internal/cast ./internal/httpapi ./cmd/fogcast-api -run 'Test.*Audio|Test.*Media|TestCast' -count=1

Expected: PASS, with existing video-only tests unchanged.

- [ ] **Step 8: Review the cross-boundary configuration milestone**

Have Vega inspect the exact Task 2 diff, including host/client.go, fogcast/service.go, internal/httpapi/cast.go, CastController/CastStatus, and compositionMediaSession. Resolve Critical/Important findings before Task 3.

### Task 3: Implement session-authenticated PCM16 RTP transport

**Owner:** Root coordinator.

**Files:**
- Create: internal/remotemedia/audio_rtp.go
- Create: internal/remotemedia/audio_sender.go
- Create: internal/remotemedia/audio_receiver.go
- Create: internal/remotemedia/audio_playout.go
- Create: internal/remotemedia/audio_rtp_test.go
- Create: internal/remotemedia/audio_sender_test.go
- Create: internal/remotemedia/audio_receiver_test.go
- Create: internal/remotemedia/audio_playout_test.go
- Modify: internal/remotemedia/control.go

**Interfaces:**
- RTPPayloadTypePCM16 = 97, RTPAudioClockRate = 48000, and DefaultAudioFrameSamples = 240.
- AudioSenderConfig contains RTPAddress, ControlAddress, Session, Generation, Token, SSRC, InitialSequence, RTPBaseTimestamp, MTU, SampleRate, Channels, FrameSamples, and FormatCapabilityVersion.
- AudioReceiverConfig contains ListenAddress, ControlAddress, Session, Generation, Token, SSRC, PayloadType, SampleRate, Channels, FrameSamples, and FormatCapabilityVersion.
- AudioSender owns one AudioSource, one UDP destination, one authenticated control connection, one audio SSRC, and one 48 kHz RTP clock.
- AudioReceiver authenticates the same (session, generation, token) identity, requires the UDP source IP to match the authenticated control peer, and pins the first observed source port using explicit trusted-network TOFU semantics. It validates the audio MEDIA_HELLO, rejects unexpected payload type/SSRC/format, and returns AudioFrame values to AudioPlayout. RTP packets have no per-packet integrity and the port is not authenticated in this design.
- Audio MEDIA_HELLO bodies include media_kind:"audio", a private format capability version, payload_type, clock_rate, ssrc, encoding:"pcm_s16le", sample_rate, channels, and frame_samples. Existing video hello bodies remain valid and default to media_kind:"video"; the public cast admission descriptor does not carry the codec string.

- [ ] **Step 1: Write failing packetizer, receiver, and playout tests**

Cover five-millisecond stereo packets fitting a 1,200-byte MTU, RTP timestamp increments of 240 samples, marker behavior, sequence-gap accounting, malformed PCM-length rejection, control authentication, peer-IP mismatch rejection, first-observed-port TOFU behavior, and AudioPlayout reorder/gap-to-silence/underrun behavior. Use a deterministic 48 kHz sine fixture with non-zero sample counts.

- [ ] **Step 2: Run focused transport tests**

Run: mise exec go@1.26.5 -- go test ./internal/remotemedia -run 'TestAudio(RTP|Sender|Receiver)' -count=1

Expected: FAIL because the packetizer, sender, receiver, and hello schema do not exist.

- [ ] **Step 3: Implement the PCM16 packetizer and clock**

Split interleaved PCM into 240-frame payloads, preserve sample order, stamp the first chunk from the source monotonic timestamp using the 48 kHz clock, advance every subsequent chunk by exactly 240 RTP ticks, and reject partial frames. Do not reuse the 90 kHz H.264 clock. Source CaptureMonoNS remains local evidence and is not serialized on the wire.

- [ ] **Step 4: Implement AudioSender**

Mirror the existing sender’s bounded startup, context cancellation, source cleanup, control-reader, and one-second report loop. Reports include receiver counters only; AudioPlayout and AudioSink own underrun/overrun metrics. No report logs endpoint names, UIDs, tokens, or private target data.

- [ ] **Step 5: Implement AudioReceiver**

Validate the hello before accepting RTP. Track sequence gaps, malformed packets, bytes, frames, non-zero samples, and shutdown. Return a defensive AudioFrame copy to AudioPlayout; underrun/overrun metrics remain with AudioPlayout and AudioSink.

- [ ] **Step 6: Run package tests and race tests**

Run: mise exec go@1.26.5 -- go test ./internal/remotemedia -count=1 and mise exec go@1.26.5 -- go test -race ./internal/remotemedia -run 'TestAudio' -count=1

Expected: PASS.

- [ ] **Step 7: Review the transport milestone**

Have Vega inspect the exact Task 3 diff for RTP clock/sequence semantics, source binding, control authentication wording, AudioPlayout ownership, and bounded cleanup. Resolve Critical/Important findings before Task 4 or target work.

### Task 4: Implement the signed Darwin audio sources

**Owner:** Root coordinator; Sol review of permission/ownership boundary; Vega review of the native diff.

**Files:**
- Create: internal/remotemedia/audio_darwin.h
- Create: internal/remotemedia/audio_darwin.m
- Create: internal/remotemedia/audio_darwin.go
- Create: internal/remotemedia/audio_stub.go
- Create: internal/remotemedia/audio_authorization.go
- Create: internal/remotemedia/audio_authorization_test.go
- Modify: resources/fogcast-host/Entitlements.plist
- Modify: resources/fogcast-host/Info.plist

**Interfaces:**
- OpenNativeAudioSource(AudioSourceConfig) (AudioSource, error) dispatches only the two configured source kinds.
- ShadowCastAudioSource uses an explicit AVCaptureDeviceInput for the named/UID-bound audio device and an AVCaptureAudioDataOutput callback. It converts the callback’s CMSampleBuffer to interleaved PCM16 and queues at most one pending frame.
- HostOutputAudioSource uses a ScreenCaptureKit SCStream with audio capture enabled for the configured display. It converts CMSampleBuffer audio to the same PCM16 contract and requires Screen Recording permission.
- C helpers expose status/request functions for microphone and Screen Recording, bounded start/first-buffer waits, endpoint enumeration, queue pop, stats, and close. The Go layer maps every denial/restriction to an actionable System Settings error.
- ScreenCaptureKit support is macOS 13+; older systems return a distinct unavailable error. Display identity uses persistent hardware UUID/EDID vendor/model/serial metadata hashed with the separate fogcast-display-v1 schema; bounds and scale remain selection context rather than identity. Without persistent metadata, require explicit operator selection and label the digest a configuration fingerprint.

- [ ] **Step 1: Write authorization and conversion tests**

Test status mapping for authorized, denied, restricted, and not-determined microphone and Screen Recording states. Test float32/interleaved/planar conversion, clipping, channel count, exact frame lengths, monotonic timestamps, and one-buffer queue replacement with a deterministic C-free Go fixture.

- [ ] **Step 2: Run platform-neutral tests before cgo implementation**

Run: mise exec go@1.26.5 -- go test ./internal/remotemedia -run 'TestAudioAuthorization|TestAudioConversion' -count=1

Expected: FAIL because the new status and conversion functions do not exist.

- [ ] **Step 3: Implement the AVFoundation ShadowCast graph**

Request microphone access only from the signed bundle, enumerate by stable UID, add the explicit audio input before starting the audio output, wait for the first callback with a finite deadline, and fail if the effective device hash differs from the configured hash. Preserve Genki’s ownership as a concurrent diagnostic case; do not steal or terminate its process.

- [ ] **Step 4: Implement the ScreenCaptureKit host-output graph**

Preflight/request Screen Recording access, select the configured display, set 48 kHz/two-channel output, and convert the stream callback to the shared PCM16 queue. Return a distinct error when Screen Recording is absent; do not misreport it as microphone denial.

- [ ] **Step 5: Update helper metadata and entitlements**

Add com.apple.security.device.audio-input to the existing camera entitlement. Preserve the existing camera and microphone usage descriptions; do not invent a Screen Recording usage-description key because the current SDK does not define one. Do not add private identities or certificates.

- [ ] **Step 6: Run Darwin tests and build the unsigned helper**

Run: CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- go test ./internal/remotemedia -run 'TestAudio' -count=1 and CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- go build -buildvcs=false -trimpath ./cmd/fogcast-api

Expected: PASS on macOS; non-Darwin builds use audio_stub.go and remain testable without hardware. TCC tests and physical endpoint tests must run from the final signed com.fogcast.host bundle, not a naked executable.

### Task 5: Compose video and audio under one managed session

**Owner:** Root coordinator.

**Files:**
- Create: internal/remotemedia/managed_media_sender.go
- Create: internal/remotemedia/managed_media_sender_test.go
- Modify: cmd/fogcast-api/main.go
- Modify: cmd/fogcast-api/main_test.go

**Interfaces:**
- ManagedMediaSender owns the existing ManagedSender plus optional ManagedAudioSender and returns one mediasession.ComponentHandle.
- ManagedAudioSender is the lifecycle wrapper around AudioSender and exposes:

~~~go
func NewManagedAudioSender(config AudioSenderConfig, source AudioSource, options ...ManagedAudioSenderOption) (*ManagedAudioSender, error)
func (s *ManagedAudioSender) Start(context.Context, string) (mediasession.ComponentHandle, error)
~~~

- newMediaSources is exactly:

~~~go
func newMediaSources(fogcast.MediaConfig) (remotemedia.CaptureSource, remotemedia.AudioSource, error)
~~~
- compositionMediaSession owns CastStartWithMedia negotiation before local worker start. It compares the acknowledged CastStatus media set/capability projection with local workers and reports active only after the target acknowledgement matches.
- Starting audio after video is successful must be transactional: an audio startup failure stops and closes video before returning an error.
- Stopping the combined handle has independent bounded cleanup for video and audio and reports the first failure without orphaning the second worker.

- [ ] **Step 1: Write failing lifecycle tests**

Use fake video/audio sources and injectable runners to cover video-only compatibility, both workers starting with the same session/generation, audio startup rollback, either worker ending the combined Done channel, idempotent stop, retry after a close error, and no source reuse on a second launch.

- [ ] **Step 2: Run focused lifecycle tests**

Run: mise exec go@1.26.5 -- go test ./internal/remotemedia ./cmd/fogcast-api -run 'TestManaged.*(Audio|Media)|Test.*Audio.*Session' -count=1

Expected: FAIL because the combined handle and audio factory are not wired.

- [ ] **Step 3: Implement the combined handle**

Reuse the existing bounded cleanup primitives where possible, give each worker distinct RTP/control/SSRC configuration, and keep the video sender’s public behavior unchanged. Ensure session replacement cannot leave an audio source running after video stops.

- [ ] **Step 4: Wire the API composition**

Replace the single-source construction only inside the media-enabled composition with newMediaSources and NewManagedMediaSender. Extend targetCast with an optional CastStartWithMedia capability; compositionMediaSession calls it before local worker start, verifies the acknowledged media set, and rejects an audio-enabled composition when only legacy CastStart exists. Preserve injected test factories and the existing target/no-target receiver behavior.

- [ ] **Step 5: Run package and race tests**

Run: mise exec go@1.26.5 -- go test ./internal/remotemedia ./cmd/fogcast-api -count=1 and mise exec go@1.26.5 -- go test -race ./internal/remotemedia ./cmd/fogcast-api -run 'TestManaged|Test.*Audio' -count=1

Expected: PASS.

- [ ] **Step 6: Review the managed-session milestone**

Have Vega inspect the exact Task 5 diff for transactional video/audio startup, target media-set negotiation ordering, generation ownership, and independent teardown. Resolve Critical/Important findings before target bridge integration.

### Task 6: Add the retained-testbed audio bridge and coordinator handoff seam

**Owner:** Root coordinator; Sol review required because this defines the target handoff seam; Vega independent review required. The current Go cast.Controller is not a hardware owner.

**Files:**
- Create: cmd/remote-play-audiobridge/main.go
- Create: cmd/remote-play-audiobridge/main_test.go
- Modify: Makefile
- Create: scripts/tests/remote-play-audiobridge_test.sh

**Interfaces:**
- The audio bridge accepts -rtp, -control, -session, -generation, -token-file, -sample-rate, -channels, and a target-private -audio-device/sink selector. It never accepts a token as a process argument.
- The diagnostic audio bridge is a standalone target-private process. It is not added to cast.Controller, mister-agent, /v1/cast/start, or public cast status; normal video starts can never launch it implicitly.
- The production handoff requires a coordinator adapter that receives CastMediaSet, grants the composite host_cast lease, opens AudioSink, waits for both worker readiness, and exposes active only after sink readiness. That adapter is a separate native-runtime workstream; this task cannot substitute process supervision for it.

- [ ] **Step 1: Write failing standalone bridge and lifecycle tests**

Cover required/omitted diagnostic flags, invalid target sink selectors/addresses, authenticated hello failure, unexpected bridge death, bounded sink cleanup, and token-file cleanup. Keep the public cast/controller tests unchanged to prove no implicit diagnostic launch.

- [ ] **Step 2: Run target package tests before implementation**

Run: mise exec go@1.26.5 -- go test ./cmd/remote-play-audiobridge -run 'Test.*Audio' -count=1

Expected: FAIL because the standalone diagnostic bridge does not exist.

- [ ] **Step 3: Implement remote-play-audiobridge**

Use AudioReceiver for session-authenticated control/RTP, bind the accepted UDP source, and write frames to the platform-neutral AudioSink queue. The retained testbed is strictly -dump-pcm or null-sink only until the native coordinator adapter grants the generation’s audio-route lease. FFmpeg/ALSA or any hardware-driving sink is forbidden in this task, even when a non-Dummy device exists. No process in this task may claim exclusive host_cast audio ownership.

- [ ] **Step 4: Implement standalone diagnostic lifecycle**

Implement the standalone diagnostic bridge with its own bounded startup/stop/reap logic. Reuse a caller-supplied session token file, preserve exact (session, generation) validation, and never expose its status through the public cast controller. The coordinator seam remains a documented prerequisite rather than an implicit fallback.

- [ ] **Step 5: Add the ARMv7 build/package path**

Add build-remote-play-audiobridge to Makefile with the same CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 settings. Keep it out of the accepted POC1A package and production image until the coordinator/sink gate is approved; the testbed binary may be supplied through the existing authorized development workflow without copying secrets.

- [ ] **Step 6: Run target-side software checks**

Run: mise exec go@1.26.5 -- go test ./cmd/remote-play-audiobridge -count=1, mise exec go@1.26.5 -- go test -race ./cmd/remote-play-audiobridge, and GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 mise exec go@1.26.5 -- go build ./cmd/remote-play-audiobridge

Expected: PASS. This proves standalone target software behavior only; it does not prove a physical audio sink, a coordinator lease, or host_cast ownership.

### Task 7: Add signed-helper, runbook, and evidence records

**Owner:** Root coordinator; Vega reviews the documentation diff and evidence classification.

**Files:**
- Modify: scripts/build-fogcast-host.sh
- Modify: scripts/tests/fogcast-host-build_test.sh
- Modify: docs/runbooks/fogcast-authorized-capture.md
- Create: docs/runbooks/fogcast-audio-worker.md
- Create: docs/audio-worker/evidence-2026-08-10.md

**Interfaces:**
- The helper build signs the same com.fogcast.host bundle with camera and com.apple.security.device.audio-input entitlements; no identity or certificate is committed.
- The audio runbook records source selection, TCC grants, endpoint hash capture, muted-speaker procedure, Genki/concurrency checks, and cleanup without recording private endpoint UIDs or target credentials.
- The evidence record separates machine-observed PCM counters/hashes, operator audibility, target sink capability, and inference. It explicitly states that the historical Stage A records remain unchanged.
- The evidence record is neutral product-audio evidence and explicitly says it does not advance active-roadmap Stage B or Stage D.

- [ ] **Step 1: Add failing static checks**

Require the build test and runbook to mention com.apple.security.device.audio-input, Screen Recording, shadowcast_uac, host_output, named/hash-bound endpoint selection, sample_rate = 48000, and speaker-muted verification.

- [ ] **Step 2: Run the static checks before documentation changes**

Run: sh scripts/tests/fogcast-host-build_test.sh

Expected: FAIL because the new audio metadata and runbook do not exist.

- [ ] **Step 3: Update the signed-helper build and runbook**

Describe the exact signed-launch requirement, the separate permission paths, explicit device selection, and the rule that Genki permission does not transfer to FogCast. Keep generic make build cgo-disabled and retain the existing video-only instructions.

- [ ] **Step 4: Write the evidence template**

Include fields for base commit, helper/bridge hashes, source kind, endpoint identity hash, sample format, session/generation, SSRCs, RTP/control addresses redacted to topology only, commands run, non-zero PCM counters, speaker mute state, target sink identity, and evidence status.

- [ ] **Step 5: Run documentation and repository checks**

Run: sh -n scripts/build-fogcast-host.sh scripts/tests/fogcast-host-build_test.sh, sh scripts/tests/fogcast-host-build_test.sh, git diff --check, and the repository’s Markdown/local-link checks.

Expected: PASS.

### Task 8: HIL closure and architecture handoff

**Owner:** Root coordinator; HIL verifier records observations; Vega performs final independent review.

**Files:**
- Modify only after review: docs/ARCHITECTURE.md
- Modify only after review: docs/ROADMAP.md
- Modify only after successful run: docs/audio-worker/evidence-2026-08-10.md

- [ ] **Step 1: Verify the host worker with ShadowCast without loopback**

With Sonic active and Genki available, launch the signed helper using the exact named/hash-bound ShadowCast endpoint, run the audio sender into a local authenticated receiver, and record non-zero PCM, 48 kHz stereo format, packet counters, and endpoint hash. Repeat while the Mac mini speaker is muted. Do not send this captured audio back to the same MiSTer.

- [ ] **Step 2: Verify the host-output adapter independently**

Run a host-only emulator/audio source, grant Screen Recording, capture host output through the host_output adapter, and record non-zero PCM while the Mac mini speaker is muted. This is the product-source comparator; ShadowCast is not substituted for it.

- [ ] **Step 3: Resolve target sink capability before claiming HIL audio**

On the designated disposable kit, inspect target audio devices and the configured backend. If the target exposes only the historical Dummy ALSA card or the backend cannot drive the MiSTer HDMI path, record the result as a blocker and stop short of physical acceptance. If a sound-capable sink and coordinator lease adapter exist, run the session with CastMediaSet audio=true, wait for both workers and AudioSink readiness, then verify the physical HDMI/TV or independent analyzer hears the active host session. A bridge-only run is software evidence, not host_cast audio ownership.

- [ ] **Step 4: Run lifecycle and ownership gates**

Exercise ten start/stop cycles, video-only compatibility, audio startup rollback, unexpected audio/video child death, target-agent restart reconciliation, native-mode ↔ host_cast transition only when the coordinator lease exists, Genki concurrent ownership, and Mac-speaker-muted capture. Confirm no stale audio process, RTP socket, control socket, or source owner remains; verify mute/drain/reset and retryable recovery when cleanup is interrupted.

- [ ] **Step 5: Update architecture and roadmap only with evidence-backed scope**

Add the separate host audio worker, target media/audio sink boundary, and staged evidence gate to the current architecture/roadmap. Do not mark the active-roadmap stage Accepted from software tests alone; preserve the bounded Stage A technical acceptance evidence and historical audio records.

- [ ] **Step 6: Run final verification and review**

Run:

~~~sh
mise exec go@1.26.5 -- go test ./...
mise exec go@1.26.5 -- go test -race ./internal/remotemedia ./internal/cast ./internal/agentconfig ./cmd/fogcast-api
mise exec go@1.26.5 -- go vet ./...
test -z "$(gofmt -l internal/remotemedia fogcast cmd/fogcast-api cmd/remote-play-audiobridge internal/cast internal/agentconfig cmd/mister-agent)"
git diff --check
git status --short
~~~

Inspect the complete diff, run the required untracked-file no-index whitespace checks and SHA-256 hashes, and obtain Vega’s independent review. Commit or push only after the user explicitly authorizes publication; this plan itself authorizes neither.

## Rollback and unresolved gate

Every implementation task is opt-in and can be rolled back by disabling [media.audio] and removing the target-private audio fields; the existing video sender and accepted POC6 video path remain usable. If the target has no sound-capable sink or coordinator lease adapter, the strongest honest result is Software-tested host capture/transport plus a documented target-sink/coordinator blocker. The next safe experiment is a known sound-capable HDMI sink or independent HDMI audio analyzer, not another zero-PCM ShadowCast probe.
