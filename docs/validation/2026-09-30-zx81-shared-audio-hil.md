# ZX81 shared-audio kit diagnostic after compiler repairs

On 2026-09-30, designated kit 1 passed the exact-package ZX81 shared-audio
silent-path diagnostic. A fresh OSS shell built with the fixes for
[#309](https://github.com/DeanoC/fes/issues/309) and
[#311](https://github.com/DeanoC/fes/issues/311) launched through an isolated
FogCast library, linked the private 8 KiB machine ROM, returned the expected
GP/package identity and PCM interface, and displayed the ZX81 `K` cursor.
Both channels stayed zero throughout the six-second active HDMI capture.
Stop returned to the native menu, with both channels also zero throughout
its six-second capture. The matching RAM validation cart passed timing and
changed zero CRAM bits outside the reserved socket.

This supersedes the [failed 2026-09-29 diagnostic](2026-09-29-zx81-shared-audio-hil.md)
for the exact candidate below. It qualifies the vacant-socket package on kit 1;
it does not qualify a factory image update, kit 2, tape/keyboard interaction,
speaker quality or audible Zon X output. No expansion was selected in the
hardware launch, and no installed image was changed.

## Exact artifacts

- Producer source: `8db8b494c158f1e6a1af9a4383d472429978c71c`, based on
  current main `b7596d2feb3a2c54bcd32536804b3d4c5253c4fb`.
- Pinned Yosys: `ec34fcf38986217af9b5558936044b7197d968a7`;
  Mistral: `7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`;
  nextpnr: `c2bb4363f4628cfbc5f304f3635e1f6eb88a47a7`.
  The nextpnr pin includes the dedicated inactive LAB-clear repair and the
  fresh locked-LAB legalisation/scaffold preservation repair. Its `lab_aclr`
  and `lab_legalise` regressions passed with these installed compiler bytes;
  [logs and compact build evidence](zx81-shared-audio-2026-09-30/result.json)
  are retained beside the captures.
- `fes.zx81` 1.3.0 package ID:
  `2c7fdeeae1317848a8ff75a11419e0e83ad48bab0e51d3e60853e36c179eeb9d`;
  archive SHA-256:
  `8c2d1c9417e6d269db5fd17720b1a0a4d2c259bbf91485488930c278dc8e0759`;
  build ID: `02a81d71d3c8bd964a8213e24ce218ab`.
- GPU 0, seed 10, timing weight 300. System/pixel/audio signoff:
  55.975 / 138.812 / 223.464 MHz against 52.224 / 74.25 / 12.288 MHz.
  The sealed shell's reserved socket was validated vacant.
- Private `machine-rom` SHA-256:
  `14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa`;
  source size 8192; ROM map SHA-256:
  `cb83a07ee980e058dbffbc67c0738c0a727ed31c7871831e3d0e543571f17f3d`;
  programmed RBF SHA-256:
  `a647b6b5ad3e9cf4b8ba0f5c7bef57bc24561fc7fb0645d7857e5f6c70c33f20`.
  The ROM itself is private and is not included in this evidence.
- Kit 1 target ID: `73dc9f5f-1a12-4a95-a820-a9b4e600769a`;
  boot ID: `f9e27b8f-f129-4ccf-b890-3a8fec0287f8`;
  installed image SHA-256:
  `bf23ccdedd63cb1a08384d2e50f1a949b2ba423d9113930df838021730203d4a`.
  Agent/runtime revision: `0ddb837cb8882f0f9bc4e517b5551196922f6850`.
  The existing compatible development host was
  `59f22ec9336230eab147f4ea4477de699360390d`, binary SHA-256
  `04b8edc9f8e759fbbcd819f625cdd4d015114673d9da3fe4614bac6f88591265`.
  This explicitly labelled host/agent revision difference is diagnostic;
  no platform software was replaced for the run.

## Hardware and capture

The isolated library imported the exact package and private ROM, bound the
named `machine-rom`, and launched its game ID using the normal owned session.
The [active receipt](zx81-shared-audio-2026-09-30/active.json) reports that exact
package/build identity, volatile persistence, `fes.simple-computer` 1.0 and
active PCM, expansion-bus, keyboard, media and video interfaces. The
[active frame](zx81-shared-audio-2026-09-30/active.png) shows the `K` cursor.
The [active lossless capture](zx81-shared-audio-2026-09-30/active.flac) is
48 kHz stereo: all 287,996 frames have zero samples in both channels,
including the first second. The
[idle capture](zx81-shared-audio-2026-09-30/idle.flac) after Stop likewise
contains only zeros throughout 288,066 frames; the
[idle frame](zx81-shared-audio-2026-09-30/idle.png) shows the native menu.

Before the ZX81 run, the same ShadowCast 3 recorded the previously qualified
SG-1000 sound package `784dfd376f7ca1c83bbb28d6139438ef2860e229d171b806a2ab7a8ea1bf3e36`
with its open diagnostic ROM `c404ef26b05c89cab6e3212d13f1fdf649056b383e1dea3ca86177e78c32e6df`.
Its [control capture](zx81-shared-audio-2026-09-30/sg1000-active.flac)
has signed samples ±3261, alternating tone-like lag-110 correlation near
0.995 and noise-like correlation near zero; Stop becomes all-zero after
settling. This confirms the capture was receiving audio rather than supplying
flat samples from a dead input. No speakers were connected.

The private host exited successfully, its temporary credential file was
removed, and kit 1's lease finished free. The normal host service remains
stopped and disabled, following the current kit-sharing guide. Both runs
retained the same boot and installed-image identity.

## Linked cart and source checks

The exact shell's RAM validation expansion ID is
`9acefeb7f31fb07f18d382576d48ffd2288390b457ac5bc49f7be3f51d0ff3d6`;
its recipe digest is
`c022da22060e03ee47517d64fbcb19ac02f2143a4d26b4ff8585076611ad51d8`.
Its CRAM comparison changes 179,979 bits inside the reserved region and **zero
outside**. The cart replay reports the same passing system/pixel/audio Fmax
as the frozen shell. This is compile/link containment and timing evidence;
the cart was not loaded on hardware in this run. Zon X publication and sound
acceptance remain a separate slice.

The producer's audio evidence check now identifies resettable serializer FFs
by retained RTL provenance. ABC had named two unreset CDC registers after the
serializer's `sample_tick` net; a name-prefix check incorrectly rejected them.
The regression accepts that rename while continuing to reject missing/wrong
serializer reset or clock. All 45 focused ZX81 producer tests, 20 expansion
producer tests and 8 ROM-map tests (one unavailable external-tool skip) passed,
as did parent `make check`.
