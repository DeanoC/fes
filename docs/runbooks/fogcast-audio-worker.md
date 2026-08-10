# FogCast audio-worker diagnostic runbook

This runbook covers the opt-in host audio worker and the retained-testbed
`remote-play-audiobridge`. It does not grant the target a `host_cast` audio
lease, does not drive a physical ALSA/HDMI sink, and does not reopen or
downgrade Stage A acceptance.

## Host source and permissions

Use the signed `com.fogcast.host` bundle. Grant Microphone to that helper for
`shadowcast_uac`; grant Screen Recording separately for `host_output`. Genki's
permission is application-specific and does not authorize FogCast. Keep the
Genki viewer available as an operator ground-truth comparator, but do not
terminate it or treat its rendered stream as proof that a raw UVC audio
endpoint is readable.

Select the endpoint by its exact AVFoundation/CoreAudio display name and the
recorded endpoint digest. Record only the digest in tracked evidence; keep
UIDs, private configuration, and credentials out of logs and source control.
The first transport format is 48,000 Hz PCM16 with one or two channels and
240-sample frames. A positive non-zero PCM counter is required; an exact-zero
counter is a host-capture failure, not evidence that the MiSTer source is
silent when Genki is audible.

## Host worker checks

1. Keep Sonic active on the disposable MiSTer and confirm Genki can display
   and hear it.
2. Launch the signed helper with the private FogCast configuration and the
   named/hash-bound source. Record the session, generation, distinct video and
   audio SSRCs, and redacted RTP/control topology.
3. Run the authenticated receiver and verify positive `packets`, `frames`, and
   `non_zero_samples` counters. Repeat with the Mac mini speaker muted; the
   capture counters must remain non-zero.
4. Test viewer/capture ownership both alternately and concurrently. If only
   the viewer has audio, record the remaining hypothesis as an application
   rendered path or an exclusive CoreAudio/UVC owner rather than changing the
   target-silence conclusion.

## Retained-testbed bridge

`remote-play-audiobridge` is a standalone target-private diagnostic process.
It accepts the authenticated session token only through `-token-file`, never
as a process argument. Its supported sinks are `-audio-device=null` and the
explicit `-audio-device=dump -dump-pcm <private-path>`; hardware ALSA/FFmpeg
playout is intentionally not implemented. The dump path must be a new regular
file (not a symlink, device, FIFO, or existing path). Example shape (with private values
substituted locally, never committed):

```sh
remote-play-audiobridge \
  -rtp 127.0.0.1:5101 \
  -control 127.0.0.1:5102 \
  -session <session> -generation <generation> -ssrc <audio-ssrc> \
  -token-file /private/path/session.token \
  -sample-rate 48000 -channels 2 -audio-device null
```

The bridge authenticates `MEDIA_HELLO`, pins the RTP source address/port under
the trusted-network TOFU rule, and reports diagnostic counters only. Its
shutdown mutes, drains, resets, and closes the sink with bounded deadlines.
Unexpected bridge death is a diagnostic failure and must not be projected as
public cast status.

## Evidence and stop conditions

Record commands, binary hashes, and machine counters separately from operator
audibility. Do not record token contents, target identity, endpoint UID, or
private paths. Stop the experiment if the target exposes only the historical
ALSA Dummy route: physical HDMI playback and `host_cast` audio remain blocked
until a native coordinator grants a composite lease for the presentation,
audio route, both workers, transport, and a ready AudioSink. A known
sound-capable HDMI sink or independent analyzer is the next safe HIL
experiment; another zero-PCM ShadowCast probe is not a substitute.
