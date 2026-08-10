# FogCast audio worker design

## Status and authority

**Decision status:** Designed — Sol architecture review and independent Vega
review approved on 2026-08-10.

**Evidence basis:** design only; no Software-tested, Reproducible,
HIL-observed, or Accepted claim. This is an approved Designed gate subordinate
to the canonical architecture; it authorizes no target mutation or deployment.

This focused design implements no protocol or platform code and authorizes no
target mutation, deployment, or HIL claim. It is subordinate to
[ARCHITECTURE.md](../../ARCHITECTURE.md), [ROADMAP.md](../../ROADMAP.md), and
[ADR 0001](../../adr/0001-portable-target-runtime.md). It preserves, rather
than revises, the historical Stage A records linked below.

## Separate source and worker boundary

FogCast has opt-in, separate video and audio workers. They share the admitted
`(session, generation)`, but each owns its source handle, SSRC, RTP/control
ports, queue, metrics, and independently bounded cleanup. `shadowcast_uac` is
a named/hash-bound UAC capture diagnostic. `host_output` is the product source
for a future physical host-cast experiment. ShadowCast capture must never be
looped back into the same MiSTer HDMI output for HIL.

Endpoint identity is a `sha256:<hex>` canonical digest, not a tracked UID. Its
exact byte schema is `fogcast-audio-endpoint-v1\0`, followed by uint32
big-endian byte-length-prefixed UTF-8 fields in this order: NFC-normalized UID
exact bytes (no whitespace collapse); NFC-normalized, whitespace-collapsed
display name; uint32 big-endian sample rate; uint32 big-endian channel count;
and length-prefixed UTF-8 `pcm_s16le`.

Display identity uses the literal `fogcast-display-v1\0` domain, followed by
exactly four uint32-big-endian length-prefixed NFC UTF-8 fields in this order:
hardware UUID, EDID vendor, EDID model, and EDID serial. Empty fields are
zero-length. Bounds and scale are selection context, not identity. Without a
persistent UUID or EDID serial, explicit operator selection is required and the
digest is only a configuration fingerprint. Golden vectors are required for
both schemas. No endpoint UID, target identity, address, credential, or token
belongs in a tracked record.

## Private audio transport and playout

Audio is interleaved PCM16 (`pcm_s16le`), initially 48 kHz, one or two
channels, 240 samples/channel (five milliseconds). It has private RTP and
authenticated control ports, its own SSRC, and a 48 kHz RTP clock. The private
media hello authenticates session/generation and declares audio kind, format
capability version, payload type, clock, SSRC, encoding, rate, channels, and
frame size. Capture monotonic time is local-only evidence.

Authenticated control binds the peer address; first RTP source port pinning is
explicit trusted-network TOFU. RTP has no per-packet integrity or hostile
network protection; SRTP/AEAD requires a separate reviewed design.

`AudioPlayout` owns bounded reorder/jitter, sequence extension, startup
prebuffer, a 48 kHz playout clock, gap-to-silence, drift handling, and
underrun/overrun metrics. It writes through platform-neutral `AudioSink`.
`AudioSink` provides bounded `Open`, `Ready`, `Write`, `Drain`, `Mute`,
`Reset`, `Stats`, and idempotent `Close`; implementation-private device names,
Linux APIs, and process layout stay behind it.

## Coordinator lease and recovery

The native hardware coordinator alone performs the architecture's
crash-consistent handoff. It first durably records `recovering`, names the old
session/generation as quiescing/release owner, and records candidate intent;
the candidate has no lease and admission is refused. It then quiesces and
releases, or proves dead and resets, old presentation and audio leases. After
that, it durably records a no-exclusive-owner checkpoint. Only from this
checkpoint may it atomically transfer the composite `host_cast` lease naming
session, generation, mode, presentation surface, audio route, video/audio RTP
and control sockets, and both worker handles. Active state is visible only
after video worker, audio worker, and AudioSink all report ready for that lease.

Startup reconciliation examines the recorded generation and worker handles,
sockets, sink, and underlying device state. Stale stop/release/ready operations
are conditioned on the recorded `(session, generation)` and cannot affect a
replacement. A failure after intent completes old-owner reconciliation; after
the no-owner checkpoint it remains recovering; after transfer it unwinds the
new generation or becomes failed if safety cannot be proven.

Either worker's death enters `recovering`, refuses admission, stops/reaps its
sibling, and independently mutes, drains, and resets the sink under deadlines.
Transport and queues are reconciled. If cleanup is incomplete, the coordinator
retains retryable ownership in `recovering`; it must not release a resource to
another generation or silently report idle. `internal/cast.Controller`, a
compatibility agent, or a diagnostic bridge cannot substitute for that owner.

## Backward-compatible media admission

`protocol/media.go` is the canonical public representation for `CastMediaSet`,
`CastMediaCapabilities`, and `CastStatusMedia`; projections must not duplicate
these JSON structures. Cast start optionally contains exactly
`media:{"version":1,"video":true,"audio":false|true}`. Omitted media
deterministically means legacy video-only and legacy status omits `media`.
When acknowledged, status is exactly
`media:{"version":1,"video":true,"audio":false|true,"ready":true,"capabilities":{"version":1,"video":true,"audio":true}}`.
A present object requires Version 1, video true, and audio a Boolean; version
0, unknown versions, video false, and audio-only are rejected. An audio host
must explicitly request `audio:true`. The target acknowledges the exact
admitted set before active. Codec, sample format, sink, and endpoint details
remain private to the authenticated media hello.

Audio admission requires the composite lease and sound-capable sink readiness.
Until then a diagnostic bridge is null-sink/PCM-dump retained-testbed software
only, not host-cast ownership or HDMI evidence.

## Permissions and evidence gates

Host capture permissions belong to the responsible host helper: audio-input for
`shadowcast_uac`, and Screen Recording/applicable system capture permission for
`host_output`. Target sink permission belongs to the coordinator; neither side
receives the other's credentials.

PCM counters, hashes, packets, and dumps are Software-tested transport evidence
only. Physical audio needs host-output, acknowledged `Audio=true`, coordinator
lease and sink readiness, and an HDMI/TV audibility observation or independent
analyzer. This Designed gate does not advance Stage A/B/D or claim
HIL-observed/Accepted. Historical context remains unchanged in [audio path
limitation](../../stage-a0/audio-path-limitation-2026-08-09.md), [audio capture
follow-up](../../stage-a0/audio-capture-follow-up-2026-08-10.md), [audio HIL
observation](../../stage-a0/audio-hil-observation-2026-08-10.md), and
[technical acceptance addendum](../../stage-a0/technical-acceptance-addendum-2026-08-10.md).

The substantive Task 0 round-two diff (`39ed6be2de84774b0109155a6881971e46e771399a7f88e5a9573e61d6918cf7`)
was reviewed by Sol (`gpt-5.6-sol`, no fallback) and Vega (`gpt-5.6-sol`, no
fallback); both returned Spec and Quality PASS with no Critical, Important, or
Minor findings. The status-only round-three diff is separately recorded as
`9253ea4c4fad7cc0ea4d6ab2ee9210ce2658d087c571ea251da25ecf53954069`. The
current gate is in [target sink
feasibility](../../audio-worker/target-sink-feasibility-2026-08-10.md).
