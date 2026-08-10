# Stage A0 audible HDMI observation — 2026-08-10

## Evidence class

This is **HIL-observed** evidence from the operator-controlled disposable
`misterpi` development kit. It records an operator listening observation on the
live USB HDMI viewer/listening path, outside the Genki/FFmpeg capture process;
it is not an inference from host capture samples and it does not expose target
addresses, credentials, or content paths.

## Procedure and identity

| Item | Observation |
| --- | --- |
| Target | Disposable `misterpi`, Terasic DE10-nano |
| Software | Stage A Main fork, commit `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d`, binary SHA-256 `f9e6fd646740449186b74821a3684686d5dbc7b33e28052e9de28b8f4c751f2e` |
| Exercise | Existing target-owned Mega Drive Sonic test, launched through `mister-agent`; expected and observed core were both `MegaDrive` |
| Physical/listening path | Operator's live USB HDMI video viewer connected to the MiSTer HDMI output; listening was performed outside the Genki/FFmpeg capture process |
| Listening condition | The viewer/system volume was initially muted; the operator unmuted it and listened to the active Sonic run |

## Observation

The operator reported that Sonic audio was audible through the USB HDMI viewer
after unmuting the viewer/system volume. This is a physical observation made
outside the Genki/FFmpeg capture process of audible Stage A HDMI output. It
resolves the previously open audible-output question for this Stage A
exercise; the earlier records deliberately did not infer silent source audio
from the ShadowCast recording.

The target was stopped through the agent API after the observation and returned
to `idle`; its health tuple remained ready, with the MiSTer process and command
pipe present. The tunnel and capture owners were then closed cleanly.

## Corroborating capture-path result

During the same active run, the named ShadowCast audio endpoint was captured
directly with FFmpeg after the viewer was unmuted. That WAV is retained locally
as the ignored artifact
`artifacts/stage-a0/observed/audio-probe/audio-after-viewer-unmute-20260810.wav`:

| Measurement | Result |
| --- | --- |
| Format | PCM signed 16-bit, 48 kHz, stereo |
| Samples | 332,288 per channel over 6.922667 seconds |
| Samples measured | Min/max `0`, peak/RMS `-inf` |
| WAV SHA-256 | `39bcf39dfa464316668798c021fe0e58ff95ebda5737fab2ad657cd62fb3d88e` |

The simultaneous physical-audibility observation and exact-zero FFmpeg
recording show that the Genki/FFmpeg capture path (or its host-side audio
presentation) is not carrying the audio heard through the live viewer path.
They do not establish that the viewer and capture process use separate
physical devices. The zero capture remains useful as a capture-path limitation
record, but it is not an audio acceptance instrument for this fixture.

## Decision impact

- Stage A now has **HIL-observed audible HDMI output** for the Stage A Sonic
  exercise.
- The retained paired AAC and direct-PCM measurements remain
  **Software-tested/policy-blocked** evidence about the ShadowCast capture
  path; they must not be treated as contrary evidence to the live-viewer
  listening observation.
- This observation closes the audio criterion for the bounded **Accepted**
  Stage A technical scope. It does not establish comparator capture parity,
  close the separate 17-material Distribution-ready release gate, or change
  the Stage B save/reload deferral. The capture-path investigation is tracked
  in the [follow-up handoff](audio-capture-follow-up-2026-08-10.md).
