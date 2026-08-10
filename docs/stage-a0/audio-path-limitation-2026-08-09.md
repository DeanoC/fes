# Stage A0 audio-path limitation — 2026-08-09

## Classification and scope

The target/capture observations below are **HIL-observed diagnostic evidence**
from the authorized disposable `misterpi` development kit. The byte-level
measurements and deterministic classification are **Software-tested** over
those retained observations. This formally documents the current ShadowCast
capture-path limitation; it does not prove that the Mega Drive core produces no
audio. A later operator listening observation through the live USB HDMI
viewer/listening path now records audible Stage A HDMI output in [the audible
HIL observation](audio-hil-observation-2026-08-10.md).

The target was restored to its normal development configuration and verified
healthy/idle after the probe. The raw WAV captures and the normalized report
remain ignored local artifacts:

`artifacts/stage-a0/observed/audio-probe/misterpi-audio-path-diagnostic-20260809.json`

The live-kit baseline recheck below is retained alongside that diagnostic
receipt as `artifacts/stage-a0/observed/audio-probe/live-unmodified-20260809.wav`.

Report SHA-256:
`e23cce3042a9e518970900aab5208a915c7f3aca16ff78788b55dd8ecba98032`.

## Observations

| Layer | Observation |
| --- | --- |
| MiSTer configuration | `dvi_mode=0`, `hdmi_audio_96k=0`; a debug-only probe also forced `video_mode=8` |
| MiSTer debug output | The target parsed the ShadowCast EDID as `HDMI TO USB`, loaded `DVI_MODE=0`, and emitted the requested 1920×1080 HDMI timing |
| Linux target audio | `/proc/asound/cards` reports only `Dummy`; this is diagnostic context, not a prerequisite for FPGA HDMI audio |
| Host capture endpoint | `ShadowCast 3`, GENKI USB audio endpoint, 2 input channels at 48 kHz; AVFoundation audio index `0` |
| Live HDMI transmitter diagnostic | A read-only `i2c-1`/`0x39` ADV7513 snapshot on the verified disposable kit showed power-on (`0x41=0x10`), HDMI output mode (`0xAF=0x06`), I²S audio select (`0x0A=0x00`), I²S0 enabled (`0x0C=0x04`), 48 kHz N=6144 configuration (`0x01..0x03=0x00,0x18,0x00`), N/CTS packet enable (`0x44=0x79`), HPD/monitor-sense plus 64-bit I²S framing detection (`0x42=0xF8`), and no audio-FIFO-full interrupt (`0x96[4]=0`) while the unmodified baseline capture remained exact-zero; see the [ADV7513 programming guide](https://www.analog.com/media/en/technical-documentation/user-guides/ADV7513_Programming_Guide.pdf) for the register meanings |
| Baseline direct PCM capture | 6,336 ms, 48 kHz stereo, 304,128 samples, min/max sample `0`, peak/RMS `-inf`; SHA-256 `ceca4b1b0472ed975ec50dceae90ad1056dae079663fa8540b3460e5ab4bdcc8` |
| Forced-1080p direct PCM capture | 6,443 ms, 48 kHz stereo, 309,248 samples, min/max sample `0`, peak/RMS `-inf`; SHA-256 `065203e567a24722d2b6396fffd43bdd440264d3647216e0595b737f36c8c159` |
| Post-cleanup audio-only capture | 6,304 ms, 48 kHz stereo, 302,592 samples, min/max sample `0`, peak/RMS `-inf`; SHA-256 `ecb8ffcc87647826f7fbc39b0e1127fc041c2b665d0d2e12152e9487a1d5113f` after the two known stale FFmpeg capture owners were terminated |
| Explicit-unmute audio-only capture | 6,219 ms, 48 kHz stereo, 298,496 samples, min/max sample `0`, peak/RMS `-inf`; SHA-256 `c732893928df343e0e020d407ba05b1bf99d6cbe6d515153f13471a803bf40e9` after sending Main `volume unmute` |
| Fresh live-kit baseline recheck | 6,357 ms, 48 kHz stereo, 305,152 samples, min/max sample `0`, peak/RMS `-inf`; SHA-256 `478199294b5780b0c73d9a066bd4a61f5ce6656de66d004229ba5afa3c4fe4ee` with the unmodified development configuration |
| Named-device direct PCM capture | AVFoundation selected the same endpoint by name (`none:ShadowCast 3`), 48 kHz stereo, 152,576 samples, min/max sample `0`, peak/RMS `-inf`; SHA-256 `a7b76d2d21add8e390d2788117d2ae15d9cba901c14858db2b39699bcae9e2fe`; retained as `artifacts/stage-a0/observed/audio-probe/shadowcast-named-audio-20260809.wav` |

The listed captures ran while the Sonic Mega Drive test was active. The direct
capture command was:

```text
ffmpeg -f avfoundation -i none:0 -t 8 -af astats=metadata=0:reset=0 -c:a pcm_s16le output.wav
```

The earlier paired AAC recordings and the fresh `dvi_mode=0` probe remain
silent as documented in the audio-evidence gate record from source commit
`3cdc75f66409104cd2f9f6dfcbedf69f9d564ae0`
(`docs/stage-a0/audio-evidence-gate-2026-08-09.md`); that historical record is
not imported into this focused capture-diagnosis package.
The post-cleanup probe was repeated with no stale FFmpeg capture owner
remaining and remained exact-zero, so the result is not explained by those
known development-process owners.

The live transmitter snapshot narrows the observation boundary: the ADV7513
was powered, in HDMI mode, configured for 48 kHz I²S/N-CTS output, and reported
HPD, monitor-sense, 64-bit I²S framing detection, and no audio-FIFO-full
interrupt at the same time that ShadowCast returned exact-zero PCM. This is
HIL-observed diagnostic evidence about the current fixture, not proof of
non-zero sample payloads. The operator's live-viewer listening result is
recorded separately; it is the authority for the audible-output observation,
not this capture-path diagnostic.

## Subsequent physical HIL observation — 2026-08-10

While the Stage A Mega Drive Sonic test was active, the operator unmuted the
USB HDMI viewer's system volume and reported hearing Sonic audio through that
viewer. A simultaneous direct FFmpeg capture from the named ShadowCast
endpoint remained exact-zero (WAV SHA-256
`39bcf39dfa464316668798c021fe0e58ff95ebda5737fab2ad657cd62fb3d88`). The
paired result is documented in [the audible HIL observation](audio-hil-observation-2026-08-10.md):
the source HDMI path is audibly producing audio, while this ShadowCast endpoint
is not a valid audio acceptance instrument under the current host setup.

## Decision

The unexplained ShadowCast audio blocker is now resolved to a bounded
**capture-path limitation**: the host presents a stereo audio endpoint, but it
delivers exactly zero PCM samples for this target/sink path under the normal,
forced-1080p, post-cleanup, explicit-unmute, and post-viewer-unmute probes.
The live-viewer listening observation closes the Stage A audible-output
observation for the exercised Sonic run, while the deterministic ShadowCast
recording gate remains blocked and must not be used to deny that observation.

Another unchanged ShadowCast recording is not new audio evidence. To claim
audio compatibility, use a known sound-capable HDMI sink/capture fixture or an
independent HDMI audio analyzer and repeat the paired comparator/Stage-A run.

## Current-policy addendum — 2026-08-10

The live viewer/listening result is sufficient for the bounded Accepted Stage A
technical scope. This limitation is now a follow-up diagnostic for the Genki /
ShadowCast / FFmpeg path; it does not reopen Stage A or imply a silent source.
See the [capture-path follow-up](audio-capture-follow-up-2026-08-10.md).
