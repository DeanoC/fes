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
to `92f96a43e611aab4336a054e53134af36bd43ad1`. The selected component commit
contains the Coleco-local toolchain lane, authenticated HIP routing, the
synchronized current-main merge, toolchain configuration attestation, the
passing seed selection and the synchronous Yosys memory mapping recipe. Other
component pins and shared definitions are unchanged.

Parent `make check`, `make test` and the final Quartus-backed `make dev` passed:

```text
consistency: package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match
make test: Ran 209 tests in 50.524s; OK (skipped=36)
```

The image profile still contains Mega Drive, Pong, SNES and NES. The
structurally verified diagnostic image was produced at
`out/native-integration-dev/development/linux.img` with SHA-256
`ce1a97c1229541a341d365c401946b3dd66fb086b020bb29748dfef77e25120e`.
The development receipt is
`out/native-integration-dev/development/development.json` with SHA-256
`27323b14ae2a6fc7bb3804dffb8efa2bf8b3d2e4d528a7ba3ced944e2d43a93c`.
The receipt records FogCast `c761cff0d9e7878d90eb3dee24ba96010acb46de`,
libmister-runtime `2629c6e1a896663b3e06688462624c3fac67ba67`,
mister-packages `4e36ca9e4832588d6b8f1f9fae6cc68eafba8aec`, selected
`misteross` `92f96a43e611aab4336a054e53134af36bd43ad1` and Quartus 17.0.2.
The native dependency rebuild completed Mega Drive, Pong, SNES and NES; the
fresh FES Pong receipt reported 0.204 ns worst setup slack and zero TNS. The
existing Mega Drive dependency's TimeQuest report remained seed/timing
sensitive (`-3.114 ns` worst setup slack), but the parent recipe accepted its
source-built bundle and the image's structural verification passed.

## Current exact producer artifacts

Both recipes used device `5CSEBA6U23I7` and source revision
`92f96a43e611aab4336a054e53134af36bd43ad1`.

| Lane | Package ID | Build ID | RBF SHA-256 | RBF bytes |
| --- | --- | --- | --- | ---: |
| OSS | `48850ea4f492164aa00fd016ff18c9dc5fe262804703dea639358ee9f81db29a` | `ecc5c0dfac9863450928f08d5f27d103` | `2f5090b2d0210507fe7f30e6a8c815b6fd0db86d20f23e4f2378859eb876f31d` | 2,568,688 |
| Quartus | `f4b35c4b30cdf660a67f8894bddbc5a3bbfad8ee8c942fe42274753159cb2696` | `a0e740429c6c2663ff14673f1084cade` | `670f4353ef3eab0c4c554f446ce4b3f04bbd5dabfb8a9f83e2ad4bb341de9d1f` | 2,375,760 |

The OSS route passed with no unrouted nets and the authenticated GPU marker
`backend hip:AMD Radeon RX 7900 XTX ready`. The sealed seed 5 achieved
`clk_sys` 55.36485290527344 MHz against 52.00208 MHz and `pixel_clk`
99.55201721191406 MHz against 74.250068 MHz. The route configuration was
`gpu-router=HIP; hip-architectures=gfx1100;gfx1201`.
Its Coleco-local lock records Yosys `da6373c0d7565f36036051efc7895fb0d9ac13c3`,
nextpnr-mistral `2d3c216afb7051d2e2070cbf678a50f274b3f786` and Mistral
`b28e30a36b5139aaed5a5d361a30b542e6b7c758`; the authenticated executable
digests are recorded in the package build-inputs sidecar. The manifest SHA-256
is `d3747265bf309fba6f8b82ffb2ea6c082d2db3ab8a14b157a95ce32cd28fc718`.

Quartus Prime Lite `17.0.2 Build 602` completed analysis, fitting, assembly
and TimeQuest with zero errors and 38 warnings. The reported worst positive
slacks were setup 2.870 ns, hold 0.154 ns, recovery 11.324 ns, removal
1.707 ns and minimum pulse width 0.961 ns, with zero TNS. The manifest SHA-256
is `a0bd6e007ee9bc4a91f126bae1931889c87f290ac46cc7e09ed696a53b399ca0`.

## Exact-kit acceptance

The designated FogCast host and target-agent path loaded each exact `.fcore`
archive. For each lane, compact diagnostic media, 16 KiB padded media and a
compact reload all passed `check_capture.py`; the expected package identity
and build identity were returned by `core-load` and `core-media`.

| Lane | Capture PNG SHA-256 | Result |
| --- | --- | --- |
| OSS | `765a9a85a42668f17efb09af45d192d0bd498690d56ce8f2fb819a2633adf653` | compact, 16 KiB, compact reload: pass |
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
`out/dev/fes-coleco/evidence/sprite-oss-92f96a4-oss-final3` and
`out/dev/fes-coleco/evidence/sprite-quartus-92f96a4-quartus-final3`. Any later
backend, component, runtime or image change needs a new exact-package
hardware gate.
