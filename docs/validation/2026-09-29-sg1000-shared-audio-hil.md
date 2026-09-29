# SG-1000 shared-audio kit diagnostic

On 2026-09-29, the designated kit 1 passed an exact-artifact SG-1000 audio
diagnostic. A normal FogCast library launch linked the open 16 KiB sound ROM
into the sealed `fes.sg1000` package. The kit monitor and ShadowCast capture
showed its Graphics I checkerboard. ShadowCast HDMI audio recorded the expected
alternating approximately 437 Hz tone and white noise. Stop returned to idle
and muted the capture; a subsequent Pong launch displayed correctly and stayed
silent. This qualifies the named SG-1000 package, ROM, image and host/target
revision for that diagnostic. It does not qualify subjective speaker quality,
commercial games, kit 2, or a later bitstream/image.

## Exact artifacts

- FES source, host, agent and runtime revision:
  `59f22ec9336230eab147f4ea4477de699360390d`.
- Installed image SHA-256:
  `ab5a76613bbe6946849fcf83ae1e17c820fa98d3f1ad4baafbc1ad6720d1d456`.
- SG-1000 package ID:
  `784dfd376f7ca1c83bbb28d6139438ef2860e229d171b806a2ab7a8ea1bf3e36`;
  archive SHA-256:
  `f50d4d0d14ab5148da2133e5417f1bf5eefd1c90d956ba750ecf8638e13cdd1d`;
  build ID: `b71429d228522abbb4c3c5b9e6f83316`.
- Open `sound-16k.rom` SHA-256:
  `c404ef26b05c89cab6e3212d13f1fdf649056b383e1dea3ca86177e78c32e6df`.
  Its `cartridge-rom` link returned map SHA-256
  `37857d41971e17e39d41fa77ad89384c2881a54355724bd7d7662f1df73ee342`
  and programmed RBF SHA-256
  `0ce8fcf80021ca0d0f6dbacc2496d1b259e1c9e840966d32316de4a495d4e4e8`.
- Silent comparison: Pong package
  `669bb0bdc9824e8da4a5dfd34d5a6e61dedad2282daba876ad9fcf0486db00b5`.
- Kit 1 target ID `73dc9f5f-1a12-4a95-a820-a9b4e600769a`, boot ID
  `3ccaaedf-4e8e-49e9-b461-7950e6686c56`.

The image came from a clean-source two-pass `make build`: both root filesystems
had the installed-image digest above. `make check`, `make doctor`, `make verify`
(including QEMU packaging), and `make release` passed before the network update.
The update verified that exact installed image after reboot. These build checks
are separate from the hardware observations below.

## Hardware observations

The private host imported the package and ROM, bound the required
`cartridge-rom`, and launched the library entry twice. Both launches returned
the same ROM-link receipt and reported active `fes.simple-computer` with
`fes.audio.pcm-s16-stereo-48k`, keyboard and fixed-720p60 video. The user saw
the checkerboard on kit 1's monitor; the [captured SG-1000 frame](sg1000-audio-2026-09-29/sg1000.png)
shows it as well. The [lossless HDMI audio capture](sg1000-audio-2026-09-29/sg1000.flac)
is 48 kHz stereo. In successive half-second windows, its 110-sample lag
correlation alternates near 0.99 for the approximately 437 Hz tone and near
zero for noise, repeating at roughly two-second periods. The recorded signal
therefore follows the diagnostic ROM's tone/noise schedule, rather than merely
showing a nonzero capture level.

Stop returned the kit to idle. After the first second of its five-second
ShadowCast recording, every remaining sample was zero. A subsequent Pong
library launch showed the [Pong frame](sg1000-audio-2026-09-29/pong.png);
the [Pong audio capture](sg1000-audio-2026-09-29/pong.flac) also contains only
zeros after its initial transition second. Pong Stop again returned idle. The
kit lease finished free, the normal FogCast host service was restored, and
the same boot and image remained ready.

The 4KPRO returned black video and flat audio even while the kit was at the
native menu. Resetting its USB device did not recover capture, so its silence
was not used to judge the SG-1000 core. ShadowCast audio worked before its
reset; resetting the ShadowCast USB device restored its video feed too. Its
MJPEG decoder logged occasional invalid frames, but the selected SG-1000 and
Pong frames above were intact. No speakers were connected, so there was no
subjective listening check. This was a leased hardware diagnostic, not general
factory-image or game-compatibility acceptance.
