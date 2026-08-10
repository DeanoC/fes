# Stage A0 Genki/ShadowCast audio-capture follow-up — 2026-08-10

## Status and boundary

This diagnostic is **resolved for the named AVFoundation comparator and the
named ShadowCast UAC endpoint when launched from the signed Terminal context**.
It produced repeated non-zero PCM from both an explicitly named, authorized,
hash-bound AVFoundation video+audio graph and the exact named FFmpeg UAC
endpoint while Sonic was active. The same endpoint remains exact-zero when
launched by the Codex app-server shell, so the FogCast launcher authorization
or responsible-process context remains the outstanding host integration issue.
The final signed-worker discriminator below completed the previously stated
integration experiment: after Genki released the UVC interface, the same
worker still received zero frames from the Codex app-server shell but received
and encoded live frames when launched from signed Terminal. Named FFmpeg,
AudioQueue, and AudioDeviceIOProc captures were non-zero both concurrently
with that Terminal worker and after its release during an active Sonic 2 run.
This isolates the remaining FogCast issue to launcher/responsible-process
authorization; it does not require composite-device ownership or source
silence to explain the Codex result.
A bounded overlap with the repository's real host video worker also returned
exact-zero UAC in both start orders; that worker captured no video frames while
Genki remained present, so composite-device ownership is a second host
hypothesis rather than a source-silence result.
It also rules out accidental FFmpeg `none:0` selection, a second enumerated
ShadowCast input at inventory time, exposed CoreAudio mute/gain controls,
FFmpeg output-conversion differences, and an audio-open failure caused by
exclusive ownership in the earlier audio-only probes.

The operator's audible Sonic observation through the live Genki viewer remains
the ground truth for that HIL exercise. Exact-zero PCM from the host capture
interface is not evidence that the MiSTer source was silent. This follow-up is
separate from the accepted bounded Stage A0 technical scope and neither
reopens nor downgrades Stage A acceptance.

## Host and endpoint binding

The exercised host configuration was:

| Item | Machine-observed value |
| --- | --- |
| Host | Mac mini, macOS 26.5.2 (25F84) |
| Viewer | Genki Arcade 0.1.3, bundle `com.genkithings.genkiarcade.qt`; executable SHA-256 `88b9ff7157dec3a23b11676efaea00ec161226b1cb66162077040cf1511490b4` |
| FFmpeg | Homebrew FFmpeg 8.1.2; executable SHA-256 `dad4b30b36a1a999bfa4b6ffbde138bd17ee496c69e12eef638227dff2c6415c`; ad-hoc code-signature CDHash `2b31e4927ffdd07e0c54d9e58b042bf8f948a9c1` |
| Successful raw-capture launcher | macOS Terminal, bundle `com.apple.Terminal`; CDHash `2a893a192e4a6b5ab0378a09f178e06e717c2949`; Terminal-launched FFmpeg and AudioQueue both produced non-zero PCM |
| AVFoundation inventory | Video `[0] ShadowCast 3`, video `[1] Capture screen 0`; audio `[0] ShadowCast 3`; no second Genki/ShadowCast audio device |
| USB identity | GENKI composite device, vendor `0x32ed`, product `0x3701`; UVC and UAC are separate USB interfaces |
| CoreAudio identity | `ShadowCast 3`, manufacturer `GENKI`, default input, one input stream, two channels, 48,000 Hz; canonical `name`/`manufacturer`/stable-UID binding SHA-256 `0e7d21e2614597c26b97daad7a4b6a88cef5fc9df93cfdf95d257e4f3215aa76` |
| CoreAudio controls | Device alive and running; input mute `0`; input gain unsupported; hog PID `-1`; zero output channels |
| Stream formats | Virtual LPCM: 48 kHz, two-channel, 32-bit float, 8 bytes/frame; physical LPCM: 48 kHz, two-channel, signed 16-bit packed, 4 bytes/frame |
| Viewer route | Genki log: `audio source — ShadowCast 3 · monitor=Mac mini Speakers [default-out]` and `feed LIVE (audio on)` |
| System output | Mac mini Speakers, 48 kHz stereo, inventory volume `100`, mute `false`; the later speaker-muted discriminator used volume `34` and restored `mute=false` |

The canonical binding is the UTF-8 encoding, with no trailing line feed, of:

```text
name=ShadowCast 3
manufacturer=GENKI
coreaudio_uid=<private stable UID>
```

This record publishes only the resulting SHA-256, not the private UID, device
serial, or private target configuration. The ignored local inventory probe at
`artifacts/stage-a0/observed/audio-probe/coreaudio-inventory.swift` has SHA-256
`df0648498a95668a0c7d8043109faec6b7b2bc24fb59e1546808e7ed30a17dfd`.

The AVFoundation permission API returned raw status `0` (`notDetermined`) for
both camera and microphone to the standalone inventory probe, and direct TCC
database inspection was denied. The operator then allowed both prompts for the
separately signed diagnostic bundle `com.fogcast.diagnostic.avcapturegraph`;
its initial video-only negative control recorded `video=3` and `audio=3` and
received video frames but no audio callbacks because it omitted the separate
audio input. After adding the explicitly named audio input, the same bundle
recorded `video=3 audio=3` and non-zero PCM. Live Genki video and successful
FFmpeg and native CoreAudio opens demonstrate effective device access, but do
**not** settle microphone authorization for each responsible executable or
bundle. The Codex app-server shell launch context produced exact-zero FFmpeg,
AudioQueue, and AudioDeviceIOProc payloads, while the same commands launched
from signed Terminal produced non-zero payloads. This is effective
process-context evidence for a TCC/responsible-launcher difference, although
the direct same-process authorization status for FFmpeg itself remains
unavailable because the Homebrew binary is ad-hoc signed.

## Ownership and routing observations

USB registry inspection showed the UVC interfaces owned through macOS
`UVCAssistant` and the UAC interfaces through `usbaudiod`. CoreAudio reported
no hog owner for `ShadowCast 3`.

With Genki live, a simultaneous FFmpeg video open failed with `Could not lock
device for configuration`, consistent with an exclusive UVC configuration
lock. Simultaneous named and indexed audio opens both succeeded and delivered
frames, so a failure to open the audio interface due to exclusive ownership
did not occur in those probes. After Genki was closed, named audio capture
still opened and remained exact-zero when launched by the Codex app-server
shell. The later Terminal-launched named capture delivered non-zero PCM while
the same device remained available. This separates the observed video lock
from the audio-open result and makes launcher authorization/context the leading
remaining difference at that point. The later FogCast video-worker overlap
below adds a composite UVC/UAC ownership hypothesis but is explicitly
qualified because the worker received no video frames.

During the final live-viewer probe, CoreAudio translated Genki's PID to an
audio-process object and reported `is_running=1`, `is_running_input=1`, and
`is_running_output=0`. Thus Genki was consuming an audio input but did not have
an active CoreAudio output stream at that instant, despite its configuration
log naming the speaker monitor. A process-output tap could not establish an
application-rendered alternative stream in that state. This machine
observation does not negate the earlier operator-heard HIL observation; it
shows that the viewer's logged monitor configuration is not itself proof of a
currently rendered output stream.

## Active-Sonic capture receipt

The disposable kit reported an active Mega Drive Sonic launch immediately
before the following captures. Genki was live, reporting 1920×1080 frames and
the route above.

The exact named FFmpeg command was:

```text
ffmpeg -hide_banner -loglevel verbose -f avfoundation \
  -i 'none:ShadowCast 3' -t 8 -ar 48000 -ac 2 \
  -c:a pcm_s16le active-sonic-ffmpeg-named-s16.wav
```

It produced 6.325333 seconds, 303,616 samples per channel, with every sample
equal to zero, peak/RMS `-inf`, and WAV SHA-256
`f164100852f7866d5299c196f769159fd9379f66fb354508fe5cfc2a79af44ad`.

A native CoreAudio AudioQueue probe then resolved the same endpoint by exact
name and captured its virtual stream without AVFoundation or FFmpeg. It read
288,256 stereo frames (576,512 samples), all zero, with raw PCM SHA-256
`d29d8adcc1060cbf994094e65915066467079630cef3c3961d6a0464b5b90d8e`
and WAV SHA-256
`cf4e438176a6327951a76ab635c18c9b6ed6a85f220757230d441ccde8510844`.
The probe source SHA-256 was
`10b2cae25bd1350f3189b417ad5a3896aed6622f6517e467c3bf20de2f4eb0a4`.
Its bytes were identical to an earlier native capture made before Sonic was
launched. The earlier file is not classified as Sonic evidence; the byte
identity is diagnostic evidence that the API path was returning invariant
silence.

Indexed AVFoundation capture (`none:0`) and the exact-name capture selected
the same single audio device. Explicit `s16`, `s24`, and `f32` output
conversions were all exact-zero. The native read excludes an FFmpeg decoder,
resampler, channel-map, output-conversion, or name/index-selection defect as
the immediate cause; it does not exclude a lower device/driver negotiation
problem.

The target was explicitly stopped after the probe and returned `state=idle`;
the health response remained `ready=true`, `mister_process=true`, and
`command_pipe=true`. No target code or accepted Stage A evidence was changed.

## Synchronized audible recheck — 2026-08-10

After the earlier idle-state confusion, the cached Sonic entry was relaunched
and the target reported `state=active`, `system=megadrive`, and
`core=MegaDrive`. The operator then confirmed that Sonic was visible in Genki
and audible through the Mac mini Speakers while Genki continued to report live
1920×1080 frames and `feed LIVE (audio on)`.

During that exact visible/audible interval, the named FFmpeg capture produced
7.040000 seconds and 337,920 samples per channel. `astats` reported min/max
`0`, peak/RMS `-inf`, and the WAV SHA-256 was
`fb1706c47cc8fe8638cbdcf77043e2dfa71a2ae8f63642baf22480048ba4cdc3`. A
concurrent native CoreAudio read of the same named device reported 288,256
stereo frames (576,512 total samples), `nonzero_samples=0`, min/max `0`, with
raw SHA-256
`d29d8adcc1060cbf994094e65915066467079630cef3c3961d6a0464b5b90d8e`.

At the same time, CoreAudio reported Genki `is_running=1`,
`is_running_input=1`, and `is_running_output=1`. This closes the “Sonic was not
running” explanation for the new zero result. The raw ShadowCast UAC endpoint
was re-observed through both capture implementations during this synchronized
run, even when the viewer is visibly rendering Sonic and the operator hears
its audio. The synchronized run is recorded in
the consolidated ignored receipt below.

## Genki built-in recording comparator — 2026-08-10

The operator supplied Genki's built-in recording
`GenkiArcade-20260810-102521.mp4` from the same live viewer session. Genki's
log records its recording start at 10:25:21 and stop, while the target remained
active. The file contains H.264 video and an AAC stereo audio stream at 48 kHz
for 11.775 seconds. Its source SHA-256 is
`132c653d6765b6eaf6b33a5a34bef96890c5149f44e86d91d72e2353f4c71a93`.

The copied source MP4 is retained at
`artifacts/stage-a0/observed/audio-probe/GenkiArcade-20260810-102521.mp4`.
After decoding its AAC stream to 48 kHz stereo signed 16-bit PCM, the retained
ignored artifact
`artifacts/stage-a0/observed/audio-probe/GenkiArcade-20260810-102521-audio-s16.wav`
has SHA-256
`fc54603a373d1a90a4116cb268a2fe12e92344b28faeff543a258b4f07ad3d6f`.
`astats` reports 565,184 samples per channel, min/max `-12519`/`14611`, peak
`-7.015135 dBFS`, and RMS `-22.194440 dBFS`. This is Software-tested
non-zero audio from Genki's application recording graph.

This comparator is not an exact same-second pair with the 10:13 raw capture;
the synchronized raw capture remains exact-zero and is the authoritative raw
endpoint result. The Genki file nevertheless narrows the diagnosis: under the
same active viewer/session configuration, Genki can retain non-zero audio in
its application recording, whereas the earlier synchronized named ShadowCast
UAC capture returned zero. It does not close the raw named-endpoint acceptance
gate.

The operator supplied a second recording from the activated viewer,
`GenkiArcade-20260810-112459.mp4`. Genki's log records its start at line
`199585` and stop at line `199610`; the target remained `state=active` and the
Genki CoreAudio process remained input/output active after the recording. The
copied ignored source artifact has SHA-256
`3b0c2d3c2018facbc56fe02f56e1cce1a6bd5368365cb94b0ddf40f4da672cf5` and contains
19.175 seconds of H.264 video plus 19.156 seconds of AAC stereo audio at
48 kHz (900 audio frames). Decoding its audio to
`artifacts/stage-a0/observed/audio-probe/GenkiArcade-20260810-112459-audio-s16.wav`
produced SHA-256
`85e04edc13fc70ef597e066e389d0a9008d1599d0bf3b678fb88d5bec9f7ce11` and
non-zero statistics: 919,488 samples per channel, min/max `-15977`/`21147`,
peak `-3.803758 dBFS`, RMS `-21.729006 dBFS`. This is a second/repeated
non-zero Genki application recording during an active Sonic/Genki session. It
is still a comparator rather than raw ShadowCast gate closure because no
sample-exact simultaneous raw capture was retained.

The operator supplied a third recording from the activated viewer,
`GenkiArcade-20260810-112804.mp4`. Genki's log records its start at line
`199819` and stop at line `199839`; the target remained `state=active` and the
Genki process was input/output active. The copied ignored source artifact has
SHA-256 `daafde7c62628e6b048ab1f23849da36e3e9008d8454f619bff64e80ed805ceb`
and contains 15.575 seconds of H.264 video plus 15.551 seconds of AAC stereo
audio at 48 kHz (731 audio frames). Decoding its audio to
`artifacts/stage-a0/observed/audio-probe/GenkiArcade-20260810-112804-audio-s16.wav`
produced SHA-256
`75958dfb8edeb9816b0cfafbce3d94256c411e959bc54607d421d60096acb0a2` and
non-zero statistics: 746,432 samples per channel, min/max `-14150`/`13602`,
peak `-7.293605 dBFS`, RMS `-22.344550 dBFS`. This is a third repeated
non-zero Genki application recording during an active Sonic/Genki session.
It is not temporally paired with the 11:27:38–11:27:58 raw-only capture, so it
remains a comparator rather than raw ShadowCast gate closure.

## Post-click direct-IOProc recheck — 2026-08-10

The operator activated Genki's viewer by clicking its start control. The target
then reported the active Sonic catalog entry, Genki reported live frames, and
CoreAudio reported the Genki process as `running=true input=true output=true`.
The synchronized eight-second named FFmpeg capture still decoded to exact
zero: 338,432 samples per channel, min/max `0`, peak/RMS `-inf`, WAV SHA-256
`e4d5f8617c6a7b1c6a05fe867a5b134ba10325fae6a08abf451b9a946ee65130`.

At the same time, a new direct `AudioDeviceIOProc` probe bypassed both
AVFoundation and AudioQueue. It resolved exact name `ShadowCast 3`, ran for
750 callbacks, received one 4,096-byte buffer per callback (3,072,000 bytes
total), and counted `0` non-zero bytes. The raw SHA-256 is
`76ed5c9c326ba62322d464a22d3fcf7b76c7352c6d33badc36ccea7a33325e97`; the
probe source SHA-256 is
`5a207dddd52ffbcf281076fb590feb947a1cbc79304c033002e402fda83e6f64`.
This is a third distinct host capture API returning zero during the
operator-activated Genki interval. AudioQueue and AudioDeviceIOProc both use
CoreAudio/HAL, so this is not an independent hardware path. It still does not
establish source silence.

## Authorized AVFoundation graph discriminator — 2026-08-10

The installed Genki binary links `libobs.framework` and loads OBS's
`mac-avcapture.plugin`. The plugin's machine-observed Objective-C metadata and
imports include `AVCaptureSession`, `AVCaptureDeviceInput`,
`AVCaptureVideoDataOutput`, `AVCaptureAudioDataOutput`, and the audio sample
buffer delegate. It links AVFoundation/CoreMediaIO/CoreMedia/CoreVideo and has
no direct CoreAudio link. The plugin executable SHA-256 is
`df8f53f8230f93c69945090b8c8ae4883726475c3cc7b66ef23eb883ad6d143b`.
This makes an application camera-session audio path a concrete hypothesis,
not merely an inference from the recording file.

The first probe was deliberately a video-input negative control: it enumerated
the separate audio device but did not add an audio `AVCaptureDeviceInput`.
Its zero audio callbacks are nondiagnostic and are retained only to document
the configuration mistake. The corrected graph added both named inputs and
recorded `audio_output_connections=1` and `video_output_connections=1`.
The authorized bundle identity is bound by Info.plist SHA-256
`fd59bd3c81d120f746108321d2fa47193fb5eb923f59b59f98c5c8188cddc559`, source
SHA-256
`662122e0def4b3c8bf964536e3cdd2e3e11427676974f8d1574315de5bcd9bd8`, and
executable SHA-256
`644127addd8e7396dd88f0986c360292a5b9fe03344b4b0f4686500ca7e853be`.

With authorization `video=3 audio=3`, the explicit graph received 954 audio
callbacks and 252 video frames; the first sample format was `lpcm`, 48 kHz,
two channels, 32-bit float, 4 bytes/frame, and the callback stream contained
3,907,584 bytes with 2,707,997 non-zero bytes. The format flags were `0x29`,
so the callback payload is two non-interleaved 512-frame float32 channel
planes. The valid copied raw artifact
`artifacts/stage-a0/observed/audio-probe/audible-sonic-avcapture-graph-explicit-audio-final.raw`
has SHA-256
`c06e7a71e727aae55c96e7eaf7789297e8d61a76818cc2be633e9ff40d14006d`. The
plane-aware converter source is the ignored
`artifacts/stage-a0/observed/audio-probe/interleave-avcapture-f32-planes.swift`
with SHA-256
`45ff1f268e4dd67e0770f86eb4c7a55e08ffa5fd39025ce9626f7b08caf97906`. It
copies frame `i` from plane 0 followed by frame `i` from plane 1 for each
4096-byte callback, producing interleaved f32le before the exact command
`ffmpeg -f f32le -ar 48000 -ac 2 -i <interleaved.f32le> -c:a pcm_s16le
<plane-aware.wav>`. The resulting 48 kHz stereo signed-16 WAV
`artifacts/stage-a0/observed/audio-probe/audible-sonic-avcapture-graph-explicit-audio-final-plane-aware-s16.wav`
has SHA-256
`cf12927e8c061b7656c0916e1d4f821216f5e86aeb35d76637606768ad80c484` and
`488,448` samples, min/max `-16072`/`16876`, peak `-5.763343 dBFS`, RMS
`-21.410080 dBFS`. The final log SHA-256 is
`d3cc6dc7526f3f71c12051b64ba89c05d825a194438930bad5a0cb9b1f042485`.

This is Software-tested non-zero PCM from an explicitly named, authorized
AVFoundation video+audio graph while the target reported active Sonic. It is
not the standalone `none:ShadowCast 3` UAC result: that raw endpoint remained
exact-zero. The result therefore identifies the capture-path distinction and
provides the requested non-zero named, hash-bound comparator; the raw UAC gate
was still blocked for the Codex-shell launch at that point; the later Terminal
section records non-zero PCM from the endpoint itself.

The same signed bundle and graph was rerun without source or permission
changes. From 11:43:10 to 11:43:20 local, it again resolved the named audio
endpoint, received 952 audio callbacks and 3,899,392 bytes with 2,669,600
non-zero bytes, and decoded to 487,424 samples with min/max `-16166`/`18524`,
peak `-4.954038 dBFS`, RMS `-23.456595 dBFS`. Its `0x29` callback payload was
converted with the same plane-aware source and command. The repeat raw SHA-256
is
`434cfb2b8a1841eeef95d16daeed08f37be830ec490122e1c42aa1361531d0db`, and the
plane-aware WAV SHA-256 is
`83bd9289b07b987f4483ecbefb8d7ec1563ed9c03b7a189f6bd9285a038bb5b6`.
Together the two runs establish a reproducible non-zero comparator receipt
bound to the app bundle, probe source/executable hashes, endpoint binding, and
exact host format. The later Terminal runs establish the separate raw-UAC
receipt under a signed launcher context.

## Mac mini speaker-muted discriminator — 2026-08-10

To test whether the Mac mini's speaker monitor was necessary for capture, the
output was set to volume `34` with `output muted=true` and restored afterward to
volume `34` with `output muted=false`. The target status was explicitly
`state=active` for the Sonic catalog entry before the controlled AVFoundation
run. Genki was stopped only for UVC ownership while the signed graph ran; this
does not alter the input-route test.

While Genki's process was present, the named FFmpeg command still returned
exact-zero PCM under the muted output state: 7.157333 seconds, 343,552
samples/channel, min/max `0`, peak/RMS `-inf`, WAV SHA-256
`3f5dc0f0c28b22e340c8c131b43f2adf7131a77ddc5d84c67bb60bee19d2b3f1` at
`artifacts/stage-a0/observed/audio-probe/20260810T085118Z-speaker-muted-ffmpeg.wav`,
and log SHA-256
`6cbfe9e7d83b7152a2999089a4db0ac3973ab4ba23baeecce6756961dc3a89a5` at
`artifacts/stage-a0/observed/audio-probe/20260810T085118Z-speaker-muted-ffmpeg.log`.

With the same muted output state, the authorized explicitly named
AVFoundation video+audio graph received 953 audio callbacks and 252 video
frames, 3,903,488 bytes with 1,427,125 non-zero bytes. Its log SHA-256 is
`13b78c1e700648e4fcca5fd6b700ee03159c0864eb97a779a8fd635f936b5279` at
`artifacts/stage-a0/observed/audio-probe/20260810T085603Z-speaker-muted-authorized4.log`,
raw SHA-256 is
`88d4c85ce78602cead60ce21d8dc13f8ab97f8bc528cd2e983e0e5d9386e934a` at
`artifacts/stage-a0/observed/audio-probe/20260810T085603Z-speaker-muted-authorized4.raw`,
and the plane-aware signed-16 WAV SHA-256 is
`2c67e8cf612357dd7d75118fccde41e00649d4e5d5a556fc56776cf9d42764f8` with
output at
`artifacts/stage-a0/observed/audio-probe/20260810T085603Z-speaker-muted-authorized4-plane-aware-s16.wav`,
487,936 samples/channel, min/max `-11511`/`12726`, peak `-8.214895 dBFS`, and
RMS `-25.242703 dBFS`. This controlled result shows that muting the Mac mini
speaker does not suppress the named AVFoundation input payload; it also shows
that the raw FFmpeg endpoint remains zero independently of speaker mute.

## Terminal-launcher raw UAC recheck — 2026-08-10

The exact named FFmpeg command was then launched through macOS Terminal rather
than the Codex app-server shell. Terminal is the signed bundle
`com.apple.Terminal` with CDHash
`2a893a192e4a6b5ab0378a09f178e06e717c2949`; the Homebrew FFmpeg executable is
ad-hoc signed with identifier `ffmpeg-555549449de88107c61f39869b4881e2a132eede`
and CDHash `2b31e4927ffdd07e0c54d9e58b042bf8f948a9c1`. The target reported
`state=active` before both runs, Genki's process was present, and the Mac mini
output was muted at volume `34` throughout each capture and restored to
`muted=false` afterward.

The first Terminal-launched raw capture produced 7.082667 seconds and 339,968
samples/channel. `astats` reported min/max `-12009`/`12876`, peak `-8.113114
dBFS`, RMS `-22.706700 dBFS`, and 679,753 of 679,936 samples were non-zero.
The WAV SHA-256 is
`585a85405fe8b5c2ff066359b68dc4aefc87e28ea4e504ece5e42087bd8b7b7c` at
`artifacts/stage-a0/observed/audio-probe/20260810T092653Z-terminal-raw-uac.wav`;
the FFmpeg log SHA-256 is
`ebba02c66f116af83ebbefe7de290e4315fef44b73c29e31034c4f7e04d167da`.

The same Terminal identity and exact command were repeated without source or
permission changes. The second run again produced 7.082667 seconds and
339,968 samples/channel, with min/max `-17489`/`19641`, peak `-4.445462 dBFS`,
RMS `-21.368334 dBFS`, and 679,826 of 679,936 samples non-zero. Its WAV
SHA-256 is
`8dc528eb30400c23faffb207b7c6561d61624c5638f5964434f4fae1c91f7961` at
`artifacts/stage-a0/observed/audio-probe/20260810T092804Z-terminal-raw-uac-repeat.wav`;
the FFmpeg log SHA-256 is
`35bad1a15989b0f49c1f2dea57c60b83df29ad9bf8ce38d207f9b0edf56c4957`.

To test whether the launcher difference also affected native CoreAudio, the
same Terminal context launched
`xcrun swift artifacts/stage-a0/observed/audio-probe/coreaudio-capture.swift
'ShadowCast 3' <output.raw>`. The probe source SHA-256 is
`10b2cae25bd1350f3189b417ad5a3896aed6622f6517e467c3bf20de2f4eb0a4`. It
reported 288,256 frames, 576,512 samples, 576,400 non-zero samples, min/max
`-11858`/`12629`; the raw SHA-256 is
`518210c17db02173da7adf4c83e42fffa3c29d605a9869e5576d921ba036f2c5`, and the
decoded signed-16 WAV SHA-256 is
`8d3a5dd0eac9a6f10b81f3a414b60c8392cd5a1638a634388979da88f221e502` with
288,256 samples, peak `-8.281354 dBFS`, and RMS `-23.104178 dBFS`.

This is reproducible non-zero PCM from the exact named `none:ShadowCast 3`
endpoint under a hash-bound signed launcher, while the same FFmpeg/AudioQueue
families launched by the Codex shell remained exact-zero. The raw UAC evidence
criterion is therefore met for the authorized Terminal launch context. The
remaining FogCast issue is to give its actual capture worker an equivalent
authorized responsible-process context; the earlier AVFoundation comparator
remains valid but is no longer the only non-zero path.

## FogCast video-worker integration and composite ownership — 2026-08-10

Repository inspection found no production raw-audio capture worker. The
FogCast host media path (`fogcast-api` and `cmd/remote-play-spike`) calls
`internal/remotemedia.OpenNativeCapture`, which is an AVFoundation video-only
source; it does not open the ShadowCast CoreAudio/UAC endpoint. The following
bounded overlap therefore exercises the real host video worker and the named
raw-audio comparator without changing target code or adding an audio worker.

The worker was built from this branch with the pinned toolchain using
`mise exec go@1.26.5 -- go build -trimpath -o
/tmp/fogcast-shadowcast-diagnostic/remote-play-spike ./cmd/remote-play-spike`.
The ignored binary was SHA-256
`4341457eff5916c82ddc256c2851c166c854e25c72c9f86d3a33ed11d7e5b8ed`,
ad-hoc-signed with CDHash
`b89b81e777691b1cf3d808e22a8190b839e7f84f`. Both runs were launched from
signed Terminal (`com.apple.Terminal`, CDHash
`2a893a192e4a6b5ab0378a09f178e06e717c2949`) with Mac mini output volume `34`
muted during capture and restored to `muted=false` afterward. Genki's process
was present; no new target-active status was claimed for these host-only
ownership runs.

* Worker-first overlap (`20260810T100500Z-terminal-fogcast-video-worker-repeat`):
  the video sender opened the named `ShadowCast 3` capture at 1920x1080 and
  source FPS `60.00024`, but reported `captured_frames=0`,
  `encoded_frames=0`, and no runtime errors. The concurrent named FFmpeg
  command wrote 3.944000 seconds of 48 kHz stereo `pcm_s16le`; all 378,624
  samples were zero (min/max `0`/`0`, peak/RMS `-inf`). WAV SHA-256
  `d5a8909e92084350527b5c4b2ec338ffe9de409e4d0a1c027e607b8a095d64c7`; FFmpeg
  log SHA-256 `f4b89987d738569de2da7c8f28cd7ab08a53acf5ca7a9273cbf96a1821916ce5`;
  worker receipt SHA-256
  `f21393af999dbfa7d87fad08a01239b22829c3b201c4916daa0eb3f601691891`.
* Audio-first overlap (`20260810T101500Z-terminal-audio-first-video-worker`):
  the named FFmpeg capture started first and ran while the same video worker
  opened `ShadowCast 3`; the worker again reported `captured_frames=0` and
  `encoded_frames=0`. The FFmpeg output measured 6.410667 seconds, 48 kHz
  stereo `pcm_s16le`, and 615,424 zero samples (min/max `0`/`0`, peak/RMS
  `-inf`). WAV SHA-256
  `da25dcde474b32a4431a547a59fc27cfc1bc0cebbbbaf364ccd6c6c70499f33e`;
  FFmpeg log SHA-256 `112f75be03448dde0da6a88e42efd3892d13f627f0a3136684902d4c55ac87b5`;
  worker receipt SHA-256
  `87c2f18d960c6d4e049fc6713d34819c058af6d3ede6b77a101e64f5c7682b7b`.

Both start orders recorded zero PCM concurrently with a configured video
worker that received no video frames while Genki remained present. Neither run
is a clean source or permission test, and the captures do not establish that
the worker itself caused the zero or conclusively held the composite device.
The strongest remaining interpretation is a possible UVC/UAC ownership or
driver-state interaction layered on top of the already demonstrated
responsible-process/TCC difference. It does not contradict the repeated
non-zero Terminal-only raw captures, does not prove that the MiSTer source was
silent, and does not satisfy a target-active paired-evidence claim.

## Final signed-worker integration discriminator — 2026-08-10

The exact next experiment was completed after the disposable target was
restored and the cache-launched Sonic 2 run was active. The target status
command was:

```text
HOME=/tmp/fogcast-home bin/fogcast --json --config /tmp/fogcast-config.nJPeWA status
```

It returned `state=active`, `system=megadrive`, `core=MegaDrive`, and the
Sonic 2 catalog entry. AVFoundation inventory still resolved one external
video device named `ShadowCast 3` and CoreAudio still resolved the one named
48 kHz, two-channel GENKI input bound by the stable-UID digest above. Genki
was fully quit before the worker tests; no Genki process remained in the
process inventory. The Mac mini output route was not used as a capture source.

### Codex-shell worker after Genki release

The repository worker was run from the Codex app-server shell with the named
endpoint and a local UDP sink. It opened the device at 1920x1080 with source
FPS `60.00024`, but after ten seconds its report had
`captured_frames=0`, `encoded_frames=0`, and zero capture/encode/runtime
errors. The report SHA-256 is
`5ea8f7f323b1328692d055d15820787380dc4ec238e2c65ee63deeb6f8b08758` at
`artifacts/stage-a0/observed/audio-probe/20260810T144947Z-codex-worker-after-genki.json`.
This reproduces the launcher-side video failure with Genki no longer owning
the UVC interface; it is not an audio result. To control for the Terminal run
negotiating a 25 fps mode, the worker was repeated from the Codex shell after
the successful Terminal capture. It then opened the same 1920x1080/25 fps mode
and again reported `captured_frames=0` and `encoded_frames=0` with no errors.
That second report is
`artifacts/stage-a0/observed/audio-probe/20260810T150605Z-codex-worker-after-terminal.json`
(SHA-256
`7652543e7256764c9d19f6ce06a94d6e090ac611508590ad62a18001b8b9789d`).

### Signed-Terminal worker and concurrent named audio

The same ad-hoc `remote-play-spike` binary (SHA-256
`c687582dc6b6bf7283cc80028753a3b0f0c369af07f0d6e51e37df2d3d952424`,
worker code-signature CDHash `3a66f0e135b1f3d82e700b7a750fb35004256ce6`) was launched
from signed macOS Terminal (`com.apple.Terminal`, CDHash recorded in the
host-binding table) using the exact-name AVFoundation video device `ShadowCast
3`, which was the sole external video device in the inventory. The stable-UID
digest above binds the separate CoreAudio/UAC endpoint used by the raw audio
probes; the worker report does not publish a video unique ID. With a local UDP
listener present, the 15-second worker report
recorded:

| Field | Result |
| --- | --- |
| Capture mode | 1920x1080, source FPS 25 |
| Captured/encoded frames | 367 / 367 |
| Packets/keyframes | 10,492 / 15 |
| Capture/encode errors | 0 / 0 |
| Drops | capture 0, queue 0, packet queue 0 |
| Encode timing | p50 6.747459 ms, p95 6.995042 ms, max 7.495541 ms |

The worker report SHA-256 is
`fd080181181ffc3e623b1969af7cf2117628552fcdc8fb7e7c4e33e48fdba69c` at
`artifacts/stage-a0/observed/audio-probe/20260810T145550Z-terminal-worker.json`.
The endpoint therefore produces live video to the real repository worker
when the responsible launcher is signed and authorized, while the identical
worker launched by the Codex shell receives no frames.

While that Terminal worker was active, three separate Terminal-launched
captures ran concurrently against the named endpoint:

```text
ffmpeg -nostdin -hide_banner -loglevel error -f avfoundation \
  -i 'none:ShadowCast 3' -t 8 -ar 48000 -ac 2 -c:a pcm_s16le <worker-overlap.wav>
xcrun swift coreaudio-capture.swift 'ShadowCast 3' <worker-overlap.raw>
xcrun swift coreaudio-io-proc-capture.swift 'ShadowCast 3' <worker-overlap.raw>
```

The exact ignored source hashes are `coreaudio-capture.swift`
`10b2cae25bd1350f3189b417ad5a3896aed6622f6517e467c3bf20de2f4eb0a4` and
`coreaudio-io-proc-capture.swift`
`5a207dddd52ffbcf281076fb590feb947a1cbc79304c033002e402fda83e6f64`.
The concurrent outputs are retained in the ignored files
`20260810T145553Z-terminal-concurrent-ffmpeg.wav`,
`20260810T145553Z-terminal-concurrent-audioqueue.raw`, and
`20260810T145553Z-terminal-concurrent-ioproc.raw` under
`artifacts/stage-a0/observed/audio-probe/`. The concurrent results were:

| Capture | Non-zero result | Artifact SHA-256 |
| --- | --- | --- |
| Named FFmpeg UAC | 6.197333 s, 48 kHz stereo PCM; overall peak `-7.124009 dBFS`, RMS `-21.798886 dBFS` | `894a52471deddd27c1fc8133f06c26246f8c0497667dbcac31b74ba343fa202f` |
| AudioQueue | 576,512 samples; 568,564 non-zero; min/max `-12408`/`13966` | `b14755e62b8a99527f199a0f396f644786c21851646cd92a1f7ba64bc82e3941` |
| AudioDeviceIOProc | 3,076,096 bytes; 2,051,199 non-zero; 751 callbacks | `cf10dfc7ae19afced2ae03d7034b2692b13d3da503413c219639e81db7fc6214` |

The concurrent AudioQueue and IOProc logs are bound by SHA-256
`bf704dd3735b4e566ead6b00b626b43a986d739fc8c372afe9f8d5610e105ec1` and
`c42de9fddbb632479a47e2b46bf8a3f2f8d6904c7425d5d0f6b1a1bdc51dc87e`.

After the video worker was released, the same Terminal identity repeated all
three named captures without source or permission changes. The outputs are
retained as `20260810T145610Z-terminal-after-worker-ffmpeg.wav`,
`20260810T145610Z-terminal-after-worker-audioqueue.raw`, and
`20260810T145610Z-terminal-after-worker-ioproc.raw` under the same ignored
directory. FFmpeg produced
6.272000 seconds with overall peak `-7.960656 dBFS` and RMS `-25.331561 dBFS`; its
WAV SHA-256 is
`a7a865d015df64ab3ee78ddffd424c2314fd8fa3866326274a9735d62f692db4`.
AudioQueue reported 576,512 samples with 431,634 non-zero and raw SHA-256
`8f4aa7c41a9a89748fea9d3a58eb79a524367387618740a8152b66bebe226afe`.
AudioDeviceIOProc reported 3,076,096 bytes with 1,639,774 non-zero and raw
SHA-256
`a2e3c8c867aba0b08ad4679443a3172bb81938c01c61f516da34d7ab3622be32`.
The post-release logs are bound by SHA-256
`1916b72bd6273df02375bff6aa8c8c7f1f8ab56a66686c33923104c9f28eadfa` and
`420c4250b5cfbe447579a89e473c8aa61ea4458e00a79c107a6eecfe34abaf29`.

This is Software-tested, named, hash-bound non-zero PCM from the raw
ShadowCast endpoint while a real signed-Terminal FogCast video worker was
receiving frames, with the target status recorded active for Sonic 2
immediately before the capture sequence. It also repeats after worker
release. The controlled discriminator removes composite
UVC/UAC ownership as the necessary explanation: the decisive difference is
the responsible launcher context. The Codex-shell FogCast path still needs a
signed/authorized application or helper with equivalent camera/microphone
authorization; no target-code or Stage A change is indicated.

## Evidence classification and provenance

| Evidence class | Observation and provenance |
| --- | --- |
| Prior HIL-observed | The operator heard Sonic through the Genki viewer; [the audible HIL observation](audio-hil-observation-2026-08-10.md) remains authoritative for audibility. |
| Synchronized HIL-observed | During the recheck, the operator confirmed Sonic was visible and audible in Genki while the target status was active. This is an operator observation, not a PCM measurement. |
| HIL-observed diagnostic context | The disposable target reported an active Mega Drive/Sonic interval before both capture runs and healthy/idle after the earlier explicit stop. Device, route, and ownership state are fixture context, not proof of non-zero audio payloads. |
| Machine-observed process state | Genki's CoreAudio process object reported input and output active during the synchronized run. The status probe source is the ignored `artifacts/stage-a0/observed/audio-probe/coreaudio-process-status.swift` (SHA-256 `52c603b78a4e42b819f07cf2fdeae257b04fab5ec0827be0b1dde4d7ac3fd519`); the safe output is retained in the consolidated receipt. |
| Synchronized direct-IOProc | After the operator activated Genki, a third host layer (`AudioDeviceIOProc`) launched by the Codex shell received 3,072,000 bytes from exact-name `ShadowCast 3`, all zero. This is launcher-context evidence, not proof of source silence. |
| Authorized AVFoundation graph | The initial video-only negative control was nondiagnostic. The corrected explicitly named video+audio graph had `video=3 audio=3`, one connection for each output, 954 audio callbacks, and non-zero PCM. This is Software-tested named comparator evidence; the later Terminal run also closes the raw UAC criterion under its signed launcher context. |
| Speaker-muted discriminator | With Mac mini Speakers explicitly muted, the Codex-shell FFmpeg capture remained exact-zero while the authorized named AVFoundation graph and later Terminal-launched raw endpoint remained non-zero. This separates host output mute from launcher context. |
| Terminal-launcher raw UAC | Two Terminal-launched FFmpeg captures and one Terminal-launched AudioQueue capture from exact-name `ShadowCast 3` produced non-zero PCM with target `state=active`; this is Software-tested named, hash-bound raw UAC evidence. |
| FogCast video-worker overlap | The real repository video worker and named UAC comparator were run concurrently in both start orders; both UAC outputs were exact-zero while the worker reported zero captured video frames. This is Software-tested host-ownership diagnostic evidence only, not source-silence or target-active evidence. |
| Signed-worker discriminator | After Genki released UVC, the Codex-shell worker still captured zero frames, while the identical exact-name worker bound to the sole-inventory AVFoundation video device and launched from signed Terminal captured 367/367 frames; concurrent and post-release named FFmpeg, AudioQueue, and AudioDeviceIOProc captures were non-zero with target status recorded active for Sonic 2 immediately before the sequence. The stable-UID digest applies to the separate CoreAudio/UAC endpoint. This is Software-tested launcher-context evidence and completes the stated integration experiment. |
| Genki binary path | The installed OBS `mac-avcapture` plugin's imports and strings identify an AVFoundation camera-session audio-output path. This is Machine-observed application-binary provenance and supports, but does not prove, a path difference. |
| Software-tested | FFmpeg `ffprobe`/`astats` and separate native probes counted and hashed exact-zero samples in the synchronized ignored artifacts `artifacts/stage-a0/observed/audio-probe/audible-sonic-ffmpeg-named-s16.wav` (SHA-256 `fb1706c47cc8fe8638cbdcf77043e2dfa71a2ae8f63642baf22480048ba4cdc3`), `artifacts/stage-a0/observed/audio-probe/audible-sonic-coreaudio-named-s16le.raw` (SHA-256 `d29d8adcc1060cbf994094e65915066467079630cef3c3961d6a0464b5b90d8e`), and the post-click direct-IOProc pair documented above. Earlier `active-sonic-*` artifacts above belong to the prior active-target run and are retained separately. |
| Software-tested comparator | Genki's built-in recordings decoded to non-zero PCM in `GenkiArcade-20260810-102521-audio-s16.wav`, `GenkiArcade-20260810-112459-audio-s16.wav`, and `GenkiArcade-20260810-112804-audio-s16.wav`; the corrected explicit named AVFoundation graph also produced non-zero PCM as detailed above. These are application-path comparators, not raw-gate acceptance. |
| Machine-observed host state | AVFoundation, CoreAudio, USB registry, Genki logs, application metadata, and system volume supplied the host inventory and process-state values. |
| Inference | Permission, application-rendered-path, transient monitor/device-state, and driver-negotiation explanations remain hypotheses; none is Accepted evidence. |

The safe consolidated ignored receipt is
`artifacts/stage-a0/observed/audio-probe/diagnostic-session-receipt-20260810.txt`,
SHA-256
`08c0a8cb7e677e3d4a88f7aafcfdfa67f70b9a0a78cc29ad00f8b1018ced5415`.
It records the repository base and branch, capture timestamps, start/end target
state, health result, exact inventory/capture/analysis commands, safe command
outputs, probe/log/file hashes, endpoint serialization, and classifications.
The FFmpeg log is retained at
`artifacts/stage-a0/observed/audio-probe/active-sonic-ffmpeg-named-s16.log`
(SHA-256
`b59d0bdd7afdcd09d00cbf04c164709b95134fff14fc6a5037a35145b4420761`),
and the native result at
`artifacts/stage-a0/observed/audio-probe/active-sonic-coreaudio-named.txt`
(SHA-256
`fb60748141948fa23d210511da872eb8b6b805165a93966deb0e6a794ebb9973`).
These artifacts remain local and ignored, consistent with the existing Stage
A0 evidence handling; their published hashes bind this receipt without
publishing private target or device identity.

The final signed-worker integration receipt is retained locally as the ignored
`artifacts/stage-a0/observed/audio-probe/diagnostic-session-receipt-20260810-final.txt`
with SHA-256
`2a84f42be2d0e391f5ff0a8b8d61ea20a1975996df24b2b0b3fbd0f04c9abe09`.
It is hash-chained to the earlier receipt and records the post-Genki
Codex/Terminal worker reports, both mode-controlled zero-frame results, the
concurrent and post-release non-zero PCM artifacts, endpoint binding, launcher
CDHashes, active target context, and the resulting authorization hypothesis.

## Conclusion and strongest remaining hypothesis

The exercised raw host endpoint is conclusively `ShadowCast 3`, not an
accidental `none:0` selection. It is a live, unmuted, non-hogged, shared 48 kHz
stereo CoreAudio input. The Codex app-server shell saw exact-zero payloads from
FFmpeg, AudioQueue, and AudioDeviceIOProc, but the same named FFmpeg and
AudioQueue captures launched through signed Terminal were repeatedly non-zero.
The endpoint is therefore not intrinsically silent, and the raw UAC evidence
criterion is met under the hash-bound Terminal launcher.

The strongest remaining host hypothesis is a responsible-process/TCC context
difference in FogCast's current launcher: the Homebrew FFmpeg binary is
ad-hoc-signed, and the Codex shell launch path does not receive the same
effective audio authorization as Terminal. Genki's OBS/AVFoundation graph and
built-in recordings remain valid non-zero application comparators, but they are
no longer needed to establish that the ShadowCast UAC payload can contain
audio. The Mac mini speaker-muted runs further show that host output mute is
not required for capture. The real FogCast video worker is video-only; both
overlap orders recorded zero PCM while that worker was configured but received
zero video frames with Genki present. Composite UVC/UAC ownership is therefore
an historical hypothesis, not a MiSTer source-silence conclusion. The final
post-release discriminator shows that composite ownership is not required to
explain the zero: the same worker receives frames and the same raw endpoint
returns non-zero PCM from signed Terminal, while the Codex-shell worker still
receives no frames.

## Exact next experiment for FogCast launcher integration — completed

The named raw UAC criterion was already satisfied for Terminal. The remaining
experiment was to release Genki, run the real video worker, and compare named
audio captures under the same stable launcher. The final discriminator above
completed all three steps without target-code or Stage A changes:

1. Genki was fully released and the cache-launched Sonic 2 target status was
   recorded as active.
2. The real `remote-play-spike` worker received zero frames from the Codex
   app-server shell but 367/367 frames from signed Terminal at the same
   exact-name, sole-inventory AVFoundation video device; the concurrently
   measured raw audio endpoint is separately bound by its CoreAudio stable-UID
   digest.
3. Named FFmpeg, AudioQueue, and AudioDeviceIOProc captures were non-zero both
   concurrently with the signed worker and after its release.

The remaining host integration action is therefore authorization, not another
capture permutation: run the FogCast worker from a signed/authorized
application or dedicated helper with the Terminal-equivalent responsible
process context. Keep the raw-evidence acceptance explicitly bound to that
launcher until FogCast has such a context. A known-good HDMI audio analyzer is
not warranted by the current results; it becomes relevant only if an
authorized worker with non-zero video and the signed Terminal comparator both
regress to zero. Do not reopen or downgrade Stage A.

The application path is named by the Genki and diagnostic bundles, and the raw
endpoint is named and hash-bound to the Terminal launcher, FFmpeg binary,
native probe sources, output artifacts, and target-active context. The Codex
shell remains an exact-zero negative context; it is not evidence of MiSTer
source silence.

## Historical independent review record — prior diagnostic revision

- Base: `93fedc8dd30bcc9b9eaf63170ee5d823a6553ed9`; branch:
  `codex/genki-shadowcast-zero-pcm`; worktree:
  `.worktrees/genki-shadowcast-zero-pcm`.
- Governing decisions: the accepted bounded Stage A scope in
  [the technical acceptance addendum](technical-acceptance-addendum-2026-08-10.md),
  the [audio-path limitation](audio-path-limitation-2026-08-09.md), and the
  operator-authoritative [audible HIL observation](audio-hil-observation-2026-08-10.md).
- Reviewer: Vega; model: `gpt-5.6-sol`; fallback: none; evidence class:
  independent read-only documentation review plus machine verification of Git
  state, ignore coverage, links, and published artifact hashes; no new HIL
  execution.
- Exact reviewed diff command:
  `git diff --no-ext-diff --binary 93fedc8dd30bcc9b9eaf63170ee5d823a6553ed9 -- docs/stage-a0/audio-capture-follow-up-2026-08-10.md`.
  The initial diff SHA-256 was
  `57605c464121162757fd6e58b6454d50968d997fe53f7a8e48d6ffcf8d9d0945`;
  it had four Important findings and one Minor finding. The corrected re-review
  diff SHA-256 was
  `a8bc7ecb35cc10b7bc5737c1a20d1b162ce7e04d508a66f7a93adb0e6e9075ff`,
  with reviewed file SHA-256
  `55512fa3283f22039c676d713b8af319d0028a0186da57484588b4c96f6f4419`.
- Disposition: all Important findings were resolved by retaining permission as
  unresolved, narrowing exclusions, adding canonical provenance and evidence
  classes, and making causal/closure language conditional. The Minor UVC-lock
  wording was narrowed. Re-review found no Critical or Important findings and
  approved the diagnostic revision for commit.
- Verification at handoff: focused audio-evidence tests and `go test ./...`
  passed; `git diff --check` passed; full-file no-index whitespace checks for
  the ignored Swift probes and consolidated receipt passed; published artifact
  hashes matched.
- Unresolved risk and next safe action: the host-side cause remains unresolved.
  This record applies to the prior committed revision. Perform the
  process-authorized, operator-audible concurrent Genki/FFmpeg/Genki-Record
  experiment above for the current revision; do not change Stage A acceptance
  or the raw ShadowCast gate without the stated evidence.

## Historical independent review record — synchronized comparator update

- Base: `4ec2e466c692376176d7c9da01710b83a768cf92`; branch:
  `codex/genki-shadowcast-zero-pcm`; worktree:
  `.worktrees/genki-shadowcast-zero-pcm`.
- Reviewer: Vega; model: `gpt-5.6-sol`; fallback: none; evidence class:
  independent read-only documentation review plus machine verification of Git
  state, ignore coverage, and published artifact hashes; no new HIL execution.
- Exact reviewed diff command:
  `git diff --no-ext-diff --binary 4ec2e466c692376176d7c9da01710b83a768cf92 -- docs/stage-a0/audio-capture-follow-up-2026-08-10.md`.
  Diff SHA-256:
  `71ea1e410952953a7b71b526b30cdb6dd40cdc6cb04bc131cc03b5dd6933e375`;
  reviewed file SHA-256:
  `09957eae17057544d5b83924a2089eebadaa66564671065355c0bab5eee9fed4`.
- Disposition: no Critical, Important, or Minor findings. The review confirmed
  the historical-review labeling, comparator provenance and scope, separated
  evidence classes, co-leading permission risk, fresh ignored bracketing
  capture procedure, and preserved Stage A acceptance.
- Unresolved risks and next safe action: responsible-process authorization is
  still unverified, and no sample-exact Genki/raw overlap exists. Run the
  timestamped overlapping Genki-Record/raw capture above; keep the raw
  ShadowCast gate blocked unless its own named-endpoint criterion passes.

## Historical independent review record — speaker-muted comparator update

- Base: `b44ae21b567675e709debdc91f4509c4a4942803`; branch:
  `codex/genki-shadowcast-zero-pcm`; worktree:
  `.worktrees/genki-shadowcast-zero-pcm`.
- Governing decisions: the accepted bounded Stage A scope in
  [the technical acceptance addendum](technical-acceptance-addendum-2026-08-10.md),
  the [audio-path limitation](audio-path-limitation-2026-08-09.md), and the
  operator-authoritative [audible HIL observation](audio-hil-observation-2026-08-10.md).
- Reviewer: Vega; model: `gpt-5.6-sol`; fallback: none; evidence class:
  independent read-only documentation review with machine verification; no
  new HIL execution.
- Exact reviewed diff command:
  `git diff --no-ext-diff --binary b44ae21b567675e709debdc91f4509c4a4942803 -- docs/stage-a0/audio-capture-follow-up-2026-08-10.md`.
  Diff SHA-256:
  `1afc561c3e82054f3ab26ac47a36063bf6c945959a8e2f26f218cff234c7f109`;
  reviewed document SHA-256 before this review record:
  `03604e7aa8c359ba37b3f20f930c6ebe0059a28a69b541a64d99fb27445487a6`;
  consolidated receipt SHA-256:
  `483ef7ed8adece0e47693a326df6ba62461eb1a4d9ec9f2c811755bcb8a45568`.
- Disposition: approved with no Critical, Important, or Minor findings. Vega
  verified the non-interleaved `0x29` converter replay and hashes, the third
  Genki recording statistics, the sequential speaker-muted/restored state, the
  process-specific TCC wording, privacy patterns, ignored receipt coverage,
  and the preservation of Stage A acceptance.
- Verification at handoff: focused and full `go test` suites passed;
  `git diff --check`, full-file no-index whitespace checks, artifact SHA-256,
  `ffprobe`, `ffmpeg astats`, converter replay/byte comparison, and final Git
  status checks passed.
- Unresolved risk and next safe action: standalone named FFmpeg/AudioQueue/
  AudioDeviceIOProc capture remains exact-zero and the responsible launcher’s
  TCC status is unknown. Run the signed-identity experiment in the exact-next
  section; do not change the raw UAC gate or Stage A acceptance without new
  evidence.

## Current independent review record — Terminal launcher raw-UAC closure

- Base: `b44ae21b567675e709debdc91f4509c4a4942803`; HEAD at review:
  `78c773747672b1452d93996532e51fc636e72c90`; branch:
  `codex/genki-shadowcast-zero-pcm`; worktree:
  `.worktrees/genki-shadowcast-zero-pcm`.
- Reviewer: Vega; model: `gpt-5.6-sol`; fallback: none; evidence class:
  independent read-only documentation and artifact review; no new HIL
  execution.
- Exact reviewed diff command:
  `git diff --no-ext-diff --binary b44ae21b567675e709debdc91f4509c4a4942803 -- docs/stage-a0/audio-capture-follow-up-2026-08-10.md`.
  Diff SHA-256:
  `ab075881b3c3402471fcd6527e9baac394892005e1eecfc0cfa36940ccbd275d`;
  reviewed document SHA-256 before this record:
  `dd3ba9b7e93e2347cea66a84b1432a5a7418535ddcf932101306ea77e143b2ed`;
  consolidated receipt SHA-256:
  `8870f9920eb4c7ecb04bf2b00b7e168ab07b12c33e3578752f236928d4e60ffa`.
- Disposition: approved with no Critical, Important, or Minor findings. Vega
  verified both Terminal FFmpeg runs and the Terminal AudioQueue run, exact
  endpoint selection, all published hashes/statistics, signed identities,
  active-target metadata, muted-state restoration, privacy/ignore coverage,
  and preservation of Stage A acceptance.
- Verification at handoff: `go test ./...`, `git diff --check`, full-file
  no-index whitespace checks, artifact SHA-256, `ffprobe`, `ffmpeg astats`,
  independent non-zero counts, raw/WAV comparison, live code-signature checks,
  output-state query, privacy scan, and final Git status all passed.
- Unresolved risk and next safe action: the Codex-shell/FogCast launcher still
  returns zero and its responsible-process authorization is not established.
  Run the signed-worker integration experiment in the exact-next section; the
  Terminal raw-UAC evidence criterion is already satisfied and Stage A remains
  unchanged.

## Current independent review record — FogCast video-worker overlap

- Base: `21059633b3c757009391f4162d07addeb0ec3609`; branch:
  `codex/genki-shadowcast-zero-pcm`; worktree:
  `.worktrees/genki-shadowcast-zero-pcm`.
- Governing decisions: the accepted bounded Stage A0 scope in
  [the technical acceptance addendum](technical-acceptance-addendum-2026-08-10.md),
  the [audio-path limitation](audio-path-limitation-2026-08-09.md), and the
  operator-authoritative [audible HIL observation](audio-hil-observation-2026-08-10.md).
- Reviewer: Vega; model: `gpt-5.6-sol`; fallback: none; evidence class:
  independent read-only documentation, source, artifact, and machine-check
  review; no new target mutation.
- Exact reviewed diff command:
  `git diff --no-ext-diff --binary 21059633b3c757009391f4162d07addeb0ec3609 -- docs/stage-a0/audio-capture-follow-up-2026-08-10.md`.
  Diff SHA-256:
  `dc56bdc728092a4696f04b112b4e78573dfd145f44e2954565e455dc38d06a6b`;
  reviewed document SHA-256 before this review record:
  `a803887aa3b857a2455c34705d3b636ed818cb80d862c07f23cc21b64b0efc58`;
  consolidated receipt SHA-256:
  `08c0a8cb7e677e3d4a88f7aafcfdfa67f70b9a0a78cc29ad00f8b1018ced5415`.
- Disposition: approved with no Critical, Important, or Minor findings. Vega
  verified both overlap WAV/log/worker-report hashes, exact-zero statistics,
  worker resolution/FPS and zero-frame reports, Terminal start-order traces,
  muted-state restoration, reproducible worker binary hash/CDHash, source
  inspection showing no production raw-audio worker, privacy coverage, and
  preservation of target code, Stage A acceptance, and `rtmp-services/`.
- Verification at handoff: focused and full `go test ./...`, `git diff --check`,
  receipt/metrics/worker-report full-file whitespace checks, artifact hashes,
  and the exact named-endpoint overlap review passed.
- Unresolved risk and next safe action: the overlaps establish concurrent
  zero PCM with a zero-frame video worker but not worker causation, source
  silence, or target-active evidence. Fully release Genki's UVC owner, prove
  the video worker receives frames, then repeat named FFmpeg/AudioQueue checks
  under that stable authorized launcher without changing Stage A scope.
