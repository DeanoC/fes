# Coleco SGM v2: sealed expansion and kit diagnostic

On 2026-09-25, the designated MiSTer Pi ran an original BIOS-free Coleco
cartridge through the normal FogCast library launch with the sealed development
v2 shell and independently routed Opcode Super Game Module (SGM) expansion.
The probe reached its graphics pass frame, and the ShadowCast audio capture
contained the two programmed SN and AY tones. Stop and an identical relaunch
succeeded. This is **exact-artifact hardware diagnostic acceptance** for the
identities below, not qualification of a new appliance image or a retail game.

## Frozen artifacts and route

- FES base: `48461fda`. The clean-source FPGA producers were sealed from
  `8dfcc60f7a9a659dd626845c4d5317189bbbe88f`; the FogCast import fix and
  integrated ROM-probe simulation are in
  `b18e6e813457a5bd5c1a7b63f512614bb9c15c79`. No first-party package
  schema or shared wire contract changed.
- Shell package ID:
  `cb453b7aa985d8c756a4993981e76caf8b64c813e9a273314a6982b8b49f3741`;
  archive SHA-256
  `bbc5097749aecb41bc7f71daec8fa64a4af7f1e2900e75dd497ac14edfbff6ec`;
  shell RBF SHA-256
  `edd6f78fd24be9bed5afa22d1ab37eae0b72bb732566bb90691c08c37179fadd`;
  BUILD_ID `1daa29c9d9dbda6575846558bee751fc`.
- SGM expansion ID:
  `b28b2bca9b413c8062d3564884853fd9aea21a781537028894520e98e0f19c54`.
  The Python producer and independent Go launch-time composers emitted the
  same 2,814,484-byte linked RBF, SHA-256
  `e7d06f5136af8cf470a2cddb60f898d7a4a183130855b0d1536672231968823c`,
  under composition ID
  `e5d2963e1839212dc01902e8a3b757b141a53fa0f3d7c0aaa17a6b57fe153263`.
- The sealed shell and cart reported final system/pixel/audio clocks of
  **53.475933 / 94.795715 / 155.496811 MHz**, above their respective
  **52.224 / 74.25 / 12.288 MHz** targets. The HIP GPU router used seed 3 and
  weight 2000. The cart changed 37,107 non-ECC CRAM bits inside the v2 region
  `(1769,32,2806,1800)` and zero outside; `route_contract=passed` and
  `archive_published=true`. The reserved placement rectangle is `24 1 28 19`;
  the v1 diagnostic retains `24 1 28 11`.
- Quartus Prime Lite 17.0.2 compiled a diagnostic vacant shell with zero
  errors and 36 warnings. Its slow 100 C corner reported 49.54 MHz system,
  128.04 MHz pixel and 231.86 MHz audio, so the system clock missed its
  52.224 MHz target by 1.041 ns. The Quartus behavioral socket does not
  preserve nextpnr's exact BEL boundary; this is a comparison oracle, not
  signoff for the linked nextpnr artifact. The authenticated nextpnr route
  closed all three timing gates. The earlier
  [placement investigation](../../sources/misteross/docs/validation/2026-09-25-coleco-sgm-expanded-socket.md)
  records the unsealed candidate and oracle setup.

## Physical observations

The designated target was `73dc9f5f-1a12-4a95-a820-a9b4e600769a`, boot ID
`4448dc24-d655-468e-a810-6a4a43ecf0b8`. Its installed image SHA-256
`5ec228ad94ef0e01ef82d35737ca516eb817c41be519ca2857ad67801bd8984a`
remained selected. Under the kit lease, both the sealed shell and the linked
RBF returned active then idle in a programming/Stop smoke test.

The original 1,261-byte probe ROM, SHA-256
`33705344c6ae221a8ec3b9c862996b6136b9c236052c9c6517e9926eeb2a5317`,
checks console RAM preservation beneath the upper overlay, SGM upper RAM at
`0x2000`, `0x5fff`, `0x6000` and `0x7fff`, lower RAM at `0x0000` and `0x1fff`,
and AY register readback before jumping to the graphics checkerboard. Failure
loops before video setup. The integrated Verilator run reached the pass frame
after 11,416,597 cycles; a vacant-socket control stayed in the failure loop.
The probe then programs about 440 Hz on AY and 217 Hz on SN.

A private FogCast host imported the shell, SGM archive and ROM, selected the
expansion for library entry `fpga-coleco-sgm-v2-probe-158d797873ac`, and
launched it through `POST /api/v1/session/launch`. The response was HTTP 200,
`active`, with the exact package, expansion, composition, BUILD_ID and linked
payload digest above. The host binary SHA-256 was
`1b031998ae569f8c665926ced8b912889e4e2f815c7794a7accd35ae77bd42f4`;
the temporary ARM agent and runtime binary SHA-256 values were respectively
`65ddebcd3f4b2f24f9f40af4f58169d875a3d12f8994030d7f41feaea776d19f`
and `84ae0ee562097f5d6729c8743498db4403b6bfc1c4a2851d9ed09b60f6490aba`.

ShadowCast 3 captured the [pass frame](coleco-sgm-v2-2026-09-25/pass.png),
SHA-256
`ee43ff9b63c3cb6b70f80acc5907c450fb1157ca2609d5fb7eff31b34c37d2b6`.
It matches the independent graphics control frame byte for byte. A five-second
48 kHz stereo recording had RMS 11,485 and spectral peaks near the programmed
217 and 440 Hz tones, consistent with simultaneous SN and AY output. The WAV
SHA-256 is `04f1ceaceaf79aab3eba99dfb2a2ef859567dee5b78e4292ba9086fc537860c8`;
the local recording remains under `out/hardware/coleco-sgm-v2-20260925/`.
After the first successful Stop, the same library entry relaunched with the
same composition and linked digest; its pass frame was byte-identical. The
second Stop returned idle and freed the lease.

The private host and temporary target mounts were removed, the installed
services restarted, and original agent/runtime binary hashes and target health
were checked. No temporary executable mount remained; the target lease was
free. The installed image was not rebuilt or replaced.

This probe verifies the listed RAM and AY register behavior and observes
combined audio, but does not isolate the two audio sources electrically or
establish compatibility with a retail SGM game. `config/core-recipes.toml`
still selects the v1 Coleco producer for the factory image; moving factory
selection to v2 is a separate integration decision.
