# FES documentation

Read one of these. The rest of `docs/` is either a procedure for that job or
a dated record. A dated record is not the schedule.

## Start here

| You want to… | Read |
| --- | --- |
| See which cores exist, their ABI, interfaces and standing | [Core status](core-status.md) |
| Set up a checkout and run the host | [Getting started](getting-started.md) |
| Change code: worktree, builds, handoff | [Development](development.md), then [agent workflow](agent-workflow.md) |
| See which module owns a change | [Project map](project-map.md), [component boundaries](component-boundaries.md) |
| Build, provision or verify a flashable disk | [Bootable media](bootable-media.md) |
| Publish a versioned appliance image | [Appliance releases](appliance-releases.md) |
| Share the kit | [Kit sharing](kit-sharing.md) |
| Work in misteross | [misteross README](../sources/misteross/README.md): OSS place-and-route experiments, or a described core |

The factory image is the ordered closed package set `fes.pong`, `fes.zx81`,
`fes.coleco`, built with HIP/nextpnr. Other registered packages are not in
that image. The matrix is [core status](core-status.md).

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
| Pong settings and best rally | [Core persistence](core-persistence.md) |
| Blob versus blob-stream capacity | [Media capacity](core-media-evolution.md) |
| ZX81 package, expansion cart, tape design | [FES ZX81](fes-zx81.md), [expansion bus](zx81-expansion-bus.md), [tape media](zx81-tape-media.md) |
| Freeze-scaffold cartridge experiments | [FPGA expansion](fpga-expansion.md) |
| Who assembles `linux.img` | [Image assembly](image-assembly.md) |
| Which outputs are versioned | [Artifact identities](artifacts.md) |
| Read source, CI, build and hardware evidence | [Status](status.md) |
| Run only the tests a change affects | [Focused tests](test-changed.md) |
| Rooms / Stop-idle design that is not built yet | [Idle MENU → rooms](idle-menu-rooms.md), [phase 2](idle-menu-rooms-phase2.md) |
| See when a soft restart asks for a board reboot | [Soft-restart Path B](soft-restart-path-b.md). The kit check in that note is on hold. |

## Not current instructions

[validation/](validation/) records what a named artifact did on a named day.
[superpowers/](superpowers/) holds old design and task plans. Do not treat
either as the way to build or accept the tree you have open.

Ownership that has already landed is [component boundaries](component-boundaries.md)
and the [project map](project-map.md). [Structure](fes-structure.md) only
points at those. It is not a second roadmap.
