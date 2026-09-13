# ColecoVision current-pin integration and exact-artifact acceptance

This is the follow-up to the [ColecoVision Graphics II sprite
record](2026-09-12-coleco-sprites.md). It selects the reviewed `misteross`
build revision in an isolated FES integration branch, rebuilds the parent
development image, and validates the current OSS and Quartus Coleco packages
on the designated kit. It does not add ColecoVision to the native image
profile.

## Selected revisions and parent result

The parent integration base is `ef05aff33d4716ca2dc1a87bc86c4387aeeb7c02`,
with only `sources/misteross` advanced from `e10284bdd1e8e442a39842a599fbb2c126300ad3`
to `7a3556216857b23860d287f4f91a08aef0b3f09a`. The selected component commit
contains the Coleco-local toolchain lane, authenticated HIP routing, the
synchronized current-main merge, toolchain configuration attestation, the
passing seed selection and the synchronous Yosys memory mapping recipe. Other
component pins and shared definitions are unchanged.

Parent checks and the final Quartus-backed development build passed:

```text
consistency: package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match
```

The image profile still contains Mega Drive, Pong, SNES and NES. The
structurally verified diagnostic image was produced at
`out/native-integration-dev/development/linux.img` with SHA-256
`5775b0d6c50585f1e26a5b2b8806de8ee9c52027e56613c2e0750e97decf8da6`.
The development receipt is
`out/native-integration-dev/development/development.json` with SHA-256
`844ef086ea57c8863ea34f80ce9ae7e561f9233fc31c9798414b736d3fd06958`.
The receipt records the selected `misteross` revision and Quartus 17.0.2.

## Current exact producer artifacts

Both recipes used device `5CSEBA6U23I7` and source revision
`7a3556216857b23860d287f4f91a08aef0b3f09a`.

| Lane | Package ID | Build ID | RBF SHA-256 | RBF bytes |
| --- | --- | --- | --- | ---: |
| OSS | `e7c15ad4fa1a691a87daea8756b3067893415f3fded878a860d2efd628dac622` | `13e0d9313b4e7e412b32c523a49a86d1` | `5fe67495bf04cc6bbb9de4d4382e8ab970275e2d8c84bc308e7c302dfb7b04f8` | 2,544,123 |
| Quartus | `a6aa5d419bd311812d951e68c93b45876e151fe5b2d050525dae13d24499dc3a` | `a7db31f0e71439fcfdc0165f78784fea` | `fbe47de877cafcff45be2b3685b925af3f66430817c4ca5baa4458c8d6e80705` | 2,391,464 |

The OSS route passed with no unrouted nets and the authenticated GPU marker
`backend hip:AMD Radeon RX 7900 XTX ready`. The sealed seed 5 achieved
`clk_sys` 54.8667 MHz against 52.0 MHz and `pixel_clk` 102.8172 MHz against
74.25 MHz. The route configuration was
`gpu-router=HIP; hip-architectures=gfx1100;gfx1201`.
Its Coleco-local lock records Yosys `da6373c0d7565f36036051efc7895fb0d9ac13c3`,
nextpnr-mistral `2d3c216afb7051d2e2070cbf678a50f274b3f786` and Mistral
`b28e30a36b5139aaed5a5d361a30b542e6b7c758`; the authenticated executable
digests are recorded in the package build-inputs sidecar.

Quartus Prime Lite `17.0.2 Build 602` completed analysis, fitting, assembly
and TimeQuest with zero errors and 38 warnings. The reported worst positive
slacks were setup 3.378 ns, hold 0.164 ns, recovery 11.653 ns, removal
1.403 ns and minimum pulse width 0.961 ns, with zero TNS.

## Exact-kit acceptance

The designated FogCast host and target-agent path loaded each exact `.fcore`
archive. For each lane, compact diagnostic media, 16 KiB padded media and a
compact reload all passed `check_capture.py`; the expected package identity
and build identity were returned by `core-load` and `core-media`.

| Lane | Capture PNG SHA-256 | Result |
| --- | --- | --- |
| OSS | `7d692ecd411efcfc10858cbb14f43056ed8c7a7e0128abc9f92bd76d5eb75c1c` | compact, 16 KiB, compact reload: pass |
| Quartus | `dfdf27d565797d891a3f313faf5a5cbef692f52ac77e00d5e9da1819e82d7664` | compact, 16 KiB, compact reload: pass |

The shared diagnostic inputs were compact ROM SHA-256
`b3aa3558e702272cdbd019d5f4553cc5e885e754ac7c29648137f2b6ce6a831c` and
16 KiB ROM SHA-256
`5bb58354ff5c49100aae1769270fe03d32524816464dcc99ee619cd09e5a054d`.
The target was left ready/idle and the kit lease was released. No direct JTAG,
Main FIFO or runtime-socket programming was used.

## Downstream handoff

The sprite renderer, four-way VRAM replication, registered request/wait
schedule, packed 4-bit line banks, sequential clear, publication interlock,
serial FSM and TV80/T80pa CPU frontend are deliberate RTL accommodations
shared by both compiler lanes. The OSS-only accommodations are the inferred
synchronous M10K implementation, the Coleco-local Yosys/nextpnr pairing,
HIP GPU-router selection, configuration attestation and sealed seed 5. The
Quartus-only details are the literal `altsyncram` instantiations,
`NEW_DATA_NO_NBE_READ`, MIF initialization and the vendor HPS/I2C primitive
boundary. OSS must preserve the registered schedule and semantic read timing,
not the vendor literal. No missing nextpnr BEL or pack feature was identified.

The exact per-lane evidence is retained in the ignored worktree directories
`out/dev/fes-coleco/evidence/sprite-oss-7a35562-oss-final2` and
`out/dev/fes-coleco/evidence/sprite-quartus-7a35562-quartus-final2`. Any later
backend, component, runtime or image change needs a new exact-package
hardware gate.
