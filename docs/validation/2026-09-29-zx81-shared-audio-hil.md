# ZX81 shared-audio kit diagnostic: acceptance blocked

This historical failure was superseded by the
[2026-09-30 exact-package diagnostic](2026-09-30-zx81-shared-audio-hil.md)
after the compiler repairs. The artifacts below remain failed candidates.

On 2026-09-29, designated kit 1 launched the sealed `fes.zx81` 1.3.0
development package through an isolated FogCast library with an exact 8 KiB
machine ROM and a vacant expansion socket. The normal ROM link, GP identity,
video and Stop paths worked for the selected OSS route, but the vacant socket
emitted nonzero HDMI audio throughout the active capture. This **fails** the
silent-path criterion; the package has no shared-audio hardware acceptance and
must not replace the installed factory ZX81 package. [Issue #311](https://github.com/DeanoC/fes/issues/311)
tracks the audio fault. A different OSS route lost a GP opcode bit despite
timing signoff; [issue #309](https://github.com/DeanoC/fes/issues/309) tracks
that independent failure. Neither issue establishes the precise compiler stage.

## Exact candidate and launch

- FES producer source revision: `1f60a1784307b516f31dea8fdb672fb3343bc62d`.
  Pinned Yosys/nextpnr/Mistral revisions: `ec34fcf3` / `a93fe013` /
  `7ed06e21`; GPU 0 route seed 12, timing weight 300.
- Sealed package ID:
  `989e5c3adb40ec64329bdaa5f6cd27108b660ff2dc774fe7b34ac6a86da65980`;
  archive SHA-256:
  `f3ff3453cac4bce8a14fbc870da3000ac062161f1e884dd4c4e6770639e78839`;
  build ID: `6811ed67f57927c8039e4999d58eecb9`.
- Shell signoff Fmax: system 52.604 MHz versus 52.224 MHz required, pixel
  111.359 versus 74.25 MHz, audio 223.314 versus 12.288 MHz. Timing closure
  alone did not predict functional audio behavior.
- Private `machine-rom` input SHA-256:
  `14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa`.
  The library launch reported ROM map SHA-256
  `df78d082f1baf60b80474a17a154e8271b814088c33afbd688f7fe5990cfbda4`
  and programmed RBF SHA-256
  `c5d28e8f65733a2d7d3187034e8287b761f5e480de565fdd3ffd79ad439c841a`.
- Kit 1 target ID: `73dc9f5f-1a12-4a95-a820-a9b4e600769a`. The active
  kit image SHA-256 was
  `ab5a76613bbe6946849fcf83ae1e17c820fa98d3f1ad4baafbc1ad6720d1d456`.
  The active
  session reported the expected package/build IDs, `fes.simple-computer`
  1.0, `fes.audio.pcm-s16-stereo-48k` 1.0, keyboard, media and video. No
  expansion archive was selected. The [steady ShadowCast frame](zx81-shared-audio-2026-09-29/seed12-active.png)
  showed the ZX81 `K` cursor. Stop returned to the native menu.

## Audio comparison and limits

Two active six-second ShadowCast 48 kHz stereo captures, including the
[retained seed-12 capture](zx81-shared-audio-2026-09-29/seed12-vacant-active.flac),
had a right-channel
sample value of 448 for almost all 288,000 frames, with intermittent left
channel peaks to 2560. After Stop, only 101 transition samples were nonzero in
five seconds in the [seed-12 idle capture](zx81-shared-audio-2026-09-29/seed12-idle.flac);
the remaining samples were zero. An independent staged OSS route
from the same source (seed 6, weight 1000) had greater system margin at
56.902 MHz but reproduced the failure: its six-second capture had 287,183
nonzero samples per channel out of 288,096 frames, with peak 512 in its
[active capture](zx81-shared-audio-2026-09-29/seed6-vacant-active.flac);
after Stop, only 97 transition samples were nonzero in the
[idle capture](zx81-shared-audio-2026-09-29/seed6-idle.flac).
That alternate package ID was
`91264439b79965aa6a4c3b50bac8256f8f134670189ea68442b4d87fb3a65909`,
archive SHA-256
`027df0768311115f67481be2b3dd4371df7f6eda3cba91b5ed9ba5bb26056a8f`.
The vacant expansion return is
constant zero at the RTL and synthesis inputs. The maintained Quartus 17.0
diagnostic compile of the same source family, loaded on the same kit and
capture card, produced zero nonzero samples in either channel throughout its
six-second active capture. The Quartus path is a control, not the product
package. No speakers were connected and no published Zon X cart was tested,
so audible Zon X behavior remains unqualified.

The seed-12 product shell's validation-cart replay failed to complete on this
route, so that package has no linked-cart qualification. The seed-6 alternate
shell did build and link the RAM validation cart: its CRAM comparison changed
179,332 bits inside the reserved slot and zero bits outside. This is a
containment result for that exact alternate shell, not a silent-path pass.
The earlier seed-10 shell also linked a cart wholly inside the socket, but it
failed GP identity. The installed factory image was not updated.
The isolated host was stopped, the normal FogCast host restored and kit 1's
lease released after the diagnostic.
