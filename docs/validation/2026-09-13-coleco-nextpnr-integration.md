# ColecoVision current-pin integration and exact-artifact acceptance

This is the follow-up to the [ColecoVision Graphics II sprite
record](2026-09-12-coleco-sprites.md). It selects the reviewed `misteross`
build revision in an isolated FES integration branch, rebuilds the parent
development image, and validates the current OSS and Quartus Coleco packages
on the designated kit. It does not add ColecoVision to the native image
profile.

## Selected revisions and parent result

The parent integration base is `746c0a69dc432dcbed5f70cf792edaf389d14001`,
with only `sources/misteross` advanced from `80c2e498d35a687c43b821f9e3c7b645acc3c4b9`
to `e10284bdd1e8e442a39842a599fbb2c126300ad3`. The selected component commit
contains the GPU-router recipe and the synchronous Yosys memory mapping
recipe. Other component pins and shared definitions are unchanged.

Parent checks and the cold Quartus-backed development build passed:

```text
consistency: package YAML valid; 14 generated consumers, 11 fixture copies and 4 copied source pins match
```

The image profile still contains Mega Drive, Pong, SNES and NES. The
structurally verified diagnostic image was produced at
`out/native-integration-dev/development/linux.img` with SHA-256
`18a03e394ecf8b3f91d951b807fc986cbba7939661d078e260a628581749aaa0`.
The development receipt is
`out/native-integration-dev/development/development.json` with SHA-256
`5c64ef348213958db54d2e2711501e764e26683711cf049e955edeab3cd08119`.
The receipt records the selected `misteross` revision and Quartus 17.0.2.

## Current exact producer artifacts

Both recipes used device `5CSEBA6U23I7` and source revision
`e10284bdd1e8e442a39842a599fbb2c126300ad3`.

| Lane | Package ID | Build ID | RBF SHA-256 | RBF bytes |
| --- | --- | --- | --- | ---: |
| OSS | `b0a5c64842dc79294b73785d493be666ecafb5005dc35e7f13a781031a176207` | `415f0d0f491ee6c8bd1f2d8760b7b2f7` | `3c2e240ce0195d48af33ac56d3751a1fc0a2d4ffbabc38b4a1db29118b5e6a94` | 2,537,099 |
| Quartus | `fb177f6cf529cc5638ebed60eff3d1ea781d03e5238ae74d5cfb333753144b4a` | `1e8f7bc7c78c3d54e69cee2d23ff612d` | `52c50d331379c03a59415f9c1909793b4e1cb5cd432bde655d046740a27d2643` | 2,396,724 |

The OSS route passed with no unrouted nets. `clk_sys` achieved 52.1213 MHz
against 52.0 MHz and `pixel_clk` achieved 90.1957 MHz against 74.25 MHz.
The package records Yosys `da6373c0d7565f36036051efc7895fb0d9ac13c3`,
nextpnr-mistral `2d3c216afb7051d2e2070cbf678a50f274b3f786` and Mistral
`b28e30a36b5139aaed5a5d361a30b542e6b7c758`.

Quartus Prime Lite `17.0.2 Build 602` completed analysis, fitting, assembly
and TimeQuest with zero errors and 38 warnings. The reported worst positive
slacks were setup 2.881 ns, hold 0.140 ns, recovery 12.199 ns, removal
1.125 ns and minimum pulse width 0.961 ns, with zero TNS.

## Exact-kit acceptance

The designated FogCast host and target-agent path loaded each exact `.fcore`
archive. For each lane, compact diagnostic media, 16 KiB padded media and a
compact reload all passed `check_capture.py`; the expected package identity
and build identity were returned by `core-load` and `core-media`.

| Lane | Capture PNG SHA-256 | Result |
| --- | --- | --- |
| OSS | `07fbd6ad3d6584bf96c17c1d7076aa586d4cb1bcdbd3f9faa1b33c36c7fa0c83` | compact, 16 KiB, compact reload: pass |
| Quartus | `dfdf27d565797d891a3f313faf5a5cbef692f52ac77e00d5e9da1819e82d7664` | compact, 16 KiB, compact reload: pass |

The shared diagnostic inputs were compact ROM SHA-256
`b3aa3558e702272cdbd019d5f4553cc5e885e754ac7c29648137f2b6ce6a831c` and
16 KiB ROM SHA-256
`5bb58354ff5c49100aae1769270fe03d32524816464dcc99ee619cd09e5a054d`.
The target was left ready/idle and the kit lease was released. No direct JTAG,
Main FIFO or runtime-socket programming was used.

## Downstream handoff

The sprite renderer, four-way VRAM replication, registered request/wait
schedule, packed 4-bit line banks, sequential clear, publication interlock
and serial FSM are deliberate RTL accommodations shared by both compiler
lanes. The OSS-only accommodations are the inferred synchronous M10K wrapper,
the pinned Yosys pairing, the nextpnr GPU-router selection and the seed. The
Quartus-only detail is the literal `altsyncram` configuration, including
`NEW_DATA_NO_NBE_READ`; OSS must preserve the registered schedule and semantic
read timing, not the vendor literal. No missing nextpnr BEL or pack feature was
identified.

The exact per-lane evidence is retained in the ignored worktree directories
`out/dev/fes-coleco/evidence/sprite-oss-e10284` and
`out/dev/fes-coleco/evidence/sprite-quartus-e10284`. Any later backend,
component, runtime or image change needs a new exact-package hardware gate.
