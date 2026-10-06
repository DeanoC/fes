# FES documentation

Read one of these. The rest of `docs/` is either a procedure for that job or
a dated record. A dated record is not the schedule.

## Start here

| You want to… | Read |
| --- | --- |
| See which cores exist, their ABI, interfaces and standing | [Core status](core-status.md) |
| Set up a checkout and run the host | [Getting started](getting-started.md) |
| Change code: worktree, builds, handoff | [Development](development.md), then [agent workflow](agent-workflow.md) |
| Coordinate cross-vendor tasks and milestone acceptance | [Agent workflow](agent-workflow.md#github-delivery-coordination) |
| Plan PR hardware-test overlays and record hashes | [Overlay HIL](hil-overlay.md) |
| See which module owns a change | [Project map](project-map.md), [component boundaries](component-boundaries.md) |
| Build, provision or verify a flashable disk | [Bootable media](bootable-media.md) |
| Publish a versioned appliance image | [Appliance releases](appliance-releases.md) |
| Share the kit | [Kit sharing](kit-sharing.md) |
| Work in misteross | [misteross README](../sources/misteross/README.md): OSS place-and-route experiments, or a described core |

The factory image is the ordered closed package set `fes.menu`, `fes.pong`,
`fes.zx81`, `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.spectrum`, `fes.ramtest`,
selected by the [default profile](../profiles/native-integration-dev.toml)
and built with HIP/nextpnr. Menu (`fes.menu`) provides the idle
display and is not a playable library entry. Other registered packages are not
in that image. The selector also supports `fes.c64`; admission does not establish
a passing seal or hardware acceptance. The matrix is [core status](core-status.md).

Shell examples in the parent guides start at the FES repository root.
A component Makefile is a different command set. `make` inside
`sources/misteross` is not parent `make`.

## Procedures

Use these when the start-here page names the job and you need the steps.

| Job | Guide |
| --- | --- |
| Install and select a described package | [Core packages](core-packages.md) |
| Prepare one core without an image rebuild | [Core developer workflow](core-development.md) |
| Run an isolated package lifecycle check | [Package acceptance](package-acceptance.md) |
| Core settings, progress and writable ST disks | [Core persistence](core-persistence.md) |
| Blob versus blob-stream capacity | [Media capacity](core-media-evolution.md) |
| Use or test the SoC DDR3 from a core | [HPS DDR](hps-ddr.md) |
| ZX81 package, expansion cart, tape design | [FES ZX81](fes-zx81.md), [expansion bus](zx81-expansion-bus.md), [tape media](zx81-tape-media.md) |
| Freeze-scaffold cartridge experiments | [FPGA expansion](fpga-expansion.md) |
| Who assembles `linux.img` | [Image assembly](image-assembly.md) |
| Which outputs are versioned | [Artifact identities](artifacts.md) |
| Read source, CI, build and hardware evidence | [Status](status.md) |
| Run only the tests a change affects | [Focused tests](test-changed.md) |
| Rooms / Stop-idle design that is not built yet | [Idle MENU → rooms](idle-menu-rooms.md), [phase 2](idle-menu-rooms-phase2.md) |
| Mesh LAN product, node protocol, and phase execution | [Mesh LAN](mesh-lan.md), [node protocol](mesh-node-protocol.md), [phase 1](mesh-phase1.md), [phase 2](mesh-phase2.md), [phase 3](mesh-phase3.md), [v-next plan](mesh-vnext.md) |
| See when a soft restart asks for a board reboot | [Soft-restart Path B](soft-restart-path-b.md). The kit check in that note is on hold. |

## Not current instructions

[validation/](validation/) records what a named artifact did on a named day.
The [C64 functional diagnostic](validation/2026-10-06-c64-functional-closure.md)
records both cartridge probes, input, SID, read-only D64 LOAD and CIA checks;
it does not promote C64 into a factory image or add disk writes.
[superpowers/](superpowers/) holds old design and task plans. Do not treat
either as the way to build or accept the tree you have open.

The [native video parts Kit 2 record](validation/2026-10-03-native-video-parts-kit2.md)
binds its sealed shell, linked Direct/Scanlines parts and capture evidence.
The [native factory host record](validation/2026-10-03-native-video-factory-build.md)
binds the later native/SGM compositions and reproducible local release. The
[factory Kit 2 record](validation/2026-10-03-native-video-factory-kit2.md)
records exact-image acceptance of normal library Direct/Scanlines launches,
optional SGM, saved preferences and captured video/audio for the named open ROMs.
Current build and selection behavior remains documented by the component
guides and [core status](core-status.md).

The [Atari ST interaction record](validation/2026-10-04-atari-st-interaction.md)
binds mouse and writable-disk software checks, the original GEMDOS guest
diagnostic, sealed video parts, and the unresolved physical byte-write failure.

Ownership that has already landed is [component boundaries](component-boundaries.md)
and the [project map](project-map.md). [Structure](fes-structure.md) only
points at those. It is not a second roadmap.
