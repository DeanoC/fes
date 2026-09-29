# SMS shared-audio kit diagnostic

On 2026-09-29, designated kit 1 ran the exact `fes.sms` 1.4.0 package and an
open 32 KiB diagnostic ROM through a FogCast library launch. The target linked
the ROM before download and reported `fes.audio.pcm-s16-stereo-48k` active.
ShadowCast captured the Mode 4 checkerboard and an approximately 399 Hz tone.
Stop returned the kit to idle and muted HDMI audio. This qualifies only the
named package, ROM, image and host/target revision for this diagnostic; it does
not establish commercial-game compatibility, subjective speaker quality or kit 2.

## Exact artifacts

- FES producer source revision: `b773f75675443f6dbca81259e873e46e891af385`.
- Package ID: `3e9eb09d946138e90f5e3861612c5d16851b7771de6a91e7a82f9ff428b958ec`;
  `.fcore` SHA-256: `8e58bb0ccfa983f37ad6f995e366fda3c859da012b2c204139c9c7caf8742a2e`;
  build ID: `dd676058e28e241d096fc8ac86972515`.
- Open `mode4-hil-32k.rom` SHA-256:
  `e35aa1b43844bd51b3573cfa53ba24bc8976e7a895e4aa6b6a4d43b5f2fc34b9`.
  The `cartridge-rom` link returned map SHA-256
  `0a9310ec5dc5fa1f5674af4c5948c0bc6499f553a6f863f1f80b4d8da6c123f0`
  and programmed RBF SHA-256
  `6fec230d7b6a30b9fab46fd9acf955b91182f444f9909cc346034c93ce7ac7e8`.
- Installed image SHA-256:
  `ab5a76613bbe6946849fcf83ae1e17c820fa98d3f1ad4baafbc1ad6720d1d456`.
  Host, target agent and runtime revision:
  `59f22ec9336230eab147f4ea4477de699360390d`.
- Kit 1 target ID `73dc9f5f-1a12-4a95-a820-a9b4e600769a`, boot ID
  `3ccaaedf-4e8e-49e9-b461-7950e6686c56`.

The clean-source HIP route on GPU 0 passed signoff at 54.54 MHz system versus
52.224 MHz requested, 99.87 MHz pixel versus 74.25 MHz and 232.72 MHz audio
versus 12.288 MHz. Its output was the package imported into the private host.

## Hardware observations

The private FogCast host imported the package and ROM, created a title, bound
its required named ROM, and launched it. The library session reported the
package/build IDs above, a volatile `fes.simple-computer` session, and the
audio, keyboard and fixed-720p60 video interfaces. [The captured frame](sms-shared-audio-2026-09-29/sms.png)
shows the ROM's Mode 4 checkerboard. [The HDMI audio capture](sms-shared-audio-2026-09-29/sms.flac)
is 48 kHz stereo; its full-second positive-crossing counts are 398–399 per
second after startup, matching the ROM's programmed PSG tone-0 period 256.
The left-channel level after startup has RMS and peak 8191 in 16-bit samples.

Stop returned the session to idle. In [the four-second idle capture](sms-shared-audio-2026-09-29/idle.flac),
only 102 samples in the first second are nonzero; every sample in the remaining
three seconds is zero. The kit lease was free afterward and the normal FogCast
host service was restored. ShadowCast logged one malformed MJPEG packet while
capturing, but the retained frame decoded correctly. No speakers were connected.
This was a leased exact-artifact hardware diagnostic, not general factory-image
acceptance.
