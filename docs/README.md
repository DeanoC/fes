# FES documentation

Read one of these. The rest of `docs/` is either a procedure for that job or
a dated record. A dated record is not the schedule.

## Start here

| You want to… | Read |
| --- | --- |
| Design or implement the FES user experience | [FES UX design](ux/README.md), [native references and Library target](ux/references/README.md), [concepts](ux/concepts/README.md), [acceptance](ux/acceptance.md) |
| See which cores exist, their ABI, interfaces and standing | [Core status](core-status.md) |
| Read the accepted Quartus-built core policy and its implementation status (#1163) | [Quartus-built cores](design/quartus-cores.md) |
| See whether cores match current FPGA choices, and the retirement order | [Core alignment](core-alignment.md) |
| Set up a checkout and run the host | [Getting started](getting-started.md) |
| Change code: worktree, builds, handoff | [Development](development.md), then [agent workflow](agent-workflow.md) |
| Reduce build/review time and decouple installation | [Development-speed redesign](development-speed.md) |
| Coordinate cross-vendor tasks and milestone acceptance | [Agent workflow](agent-workflow.md#github-delivery-coordination) |
| Decide whether a PR needs a full image for hardware testing | [HIL plan](hil-plan.md) |
| See which module owns a change | [Project map](project-map.md), [component boundaries](component-boundaries.md) |
| Build, provision or verify a flashable disk | [Bootable media](bootable-media.md) |
| Publish a versioned appliance image | [Appliance releases](appliance-releases.md) |
| Share the kit | [Kit sharing](kit-sharing.md) |
| Work in misteross | [misteross README](../sources/misteross/README.md): OSS place-and-route experiments, or a described core |

The default image contains the locked idle RBF and `fes.menu` display firmware.
Playable cores are installed separately, so their builds do not hold up OS or
software work. See [modular updates](modular-updates.md) for update boundaries
and [core status](core-status.md) for individual package standing.

Shell examples in the parent guides start at the FES repository root.
A component Makefile is a different command set. `make` inside
`sources/misteross` is not parent `make`.

## Procedures

Use these when the start-here page names the job and you need the steps.

| Job | Guide |
| --- | --- |
| Install and select a described package | [Core packages](core-packages.md) |
| Install core versions durably on an existing kit volume | [Durable kit core library](kit-core-library.md) |
| Prepare one core without an image rebuild | [Core developer workflow](core-development.md) |
| Audit or refresh core-package cache keys | [Core-key narrowing](core-key-narrowing.md) |
| Run an isolated package lifecycle check | [Package acceptance](package-acceptance.md) |
| Independent host, kit software and core updates | [Modular updates](modular-updates.md) |
| Core settings, progress and writable ST disks | [Core persistence](core-persistence.md) |
| Blob versus blob-stream capacity | [Media capacity](core-media-evolution.md) |
| Use or test the SoC DDR3 from a core | [HPS DDR](hps-ddr.md) |
| Show launcher overlays or a surround over a running core | [Display pipeline](display-pipeline.md) |
| ZX81 package, expansion cart, tape design | [FES ZX81](fes-zx81.md), [expansion bus](zx81-expansion-bus.md), [tape media](zx81-tape-media.md) |
| Freeze-scaffold cartridge experiments | [FPGA expansion](fpga-expansion.md) |
| Who assembles `linux.img` | [Image assembly](image-assembly.md) |
| Which outputs are versioned | [Artifact identities](artifacts.md) |
| Read source, CI, build and hardware evidence | [Status](status.md) |
| Run only the tests a change affects | [Focused tests](test-changed.md) |
| Rooms / Stop-idle design that is not built yet | [Idle MENU → rooms](idle-menu-rooms.md), [phase 2](idle-menu-rooms-phase2.md). Present tense on that page, outside “What is true now”, still describes the old MENU idle (`menu.rbf`, `mister_v1`). Current idle install paths are [image assembly](image-assembly.md) and `image/build/native-inputs.toml`. |
| The kit as its own host: sources, sessions, what the mesh drafts still mean | [Mesh under kit-as-host](mesh-kit-host.md) (22 Oct 2026 decisions, #863) |
| Earlier mesh drafts, historical on the kit side | [Mesh LAN](mesh-lan.md), [node protocol](mesh-node-protocol.md), [phase 1](mesh-phase1.md), [phase 2](mesh-phase2.md), [phase 3](mesh-phase3.md), [v-next plan](mesh-vnext.md) |
| See when a soft restart asks for a board reboot | [Soft-restart Path B](soft-restart-path-b.md). The kit check in that note is on hold. |

## Not current instructions

[validation/](validation/) records what a named artifact did on a named day.
The [C64 functional diagnostic](validation/2026-10-06-c64-functional-closure.md)
records both cartridge probes, input, SID, read-only D64 LOAD and CIA checks;
it does not promote C64 into a factory image or add disk writes.
[superpowers/](superpowers/) holds old design and task plans. Do not treat
either as the way to build or accept the tree you have open.

The approved [cores-as-apps plan](cores-as-apps-plan.md) and its
[per-core recipe option audit](recipe-option-audit.md) are design context for
issue #843. This delivery covers migration steps 1–2; later steps remain
proposals, not current build instructions.

The [native video parts Kit 2 record](validation/2026-10-03-native-video-parts-kit2.md)
binds its sealed shell, linked Direct/Scanlines parts and capture evidence.
The [native factory host record](validation/2026-10-03-native-video-factory-build.md)
binds the later native/SGM compositions and reproducible local release. The
[factory Kit 2 record](validation/2026-10-03-native-video-factory-kit2.md)
records exact-image acceptance of normal library Direct/Scanlines launches,
optional SGM, saved preferences and captured video/audio for the named open ROMs.
Current build and selection behavior remains documented by the component
guides and [core status](core-status.md).

The [Atari ST native image record](validation/2026-10-07-atari-st-native-image.md) binds the verified nine-package/four-part image, diskless GEM, cold-host saved-disk restore, full payload comparisons and final populated-menu restoration.
The [extended floppy geometry record](validation/2026-10-07-atari-st-floppy-geometry.md) binds all twelve supported shapes, original BIG/Cuddly loader captures, complete physical snapshots and cold restore, the separate nextpnr routing blocker, and final menu restoration. It is a derived-image diagnostic rather than factory or demo acceptance.
The [BIG loader diagnosis](validation/2026-10-08-atari-st-big-loader.md) records the corrected empty-memory acknowledgement, a CPU regression that rejects the old decoder, and the same-ROM/same-disk Hatari failure. It is host-only evidence, with BIG display/audio acceptance still outstanding.


The [Atari ST interaction record](validation/2026-10-04-atari-st-interaction.md)
binds mouse and writable-disk software checks, the original GEMDOS guest
diagnostic, sealed video parts, and the unresolved physical byte-write failure.

The [pc386 M2 record](validation/2026-10-26-pc386-m2-vga/summary.md) binds the
RTL VGA, capture, 720p presenter and display plane on DDR port 2 in host
simulation (VGA BIOS text, mode 13h and mode Y at 720p, the VGA trace replay
against Bochs's VGA) and the first machine-only synthesis figures. Like the
[M1 record](validation/2026-10-26-pc386-m1-freedos-boot/summary.md), it is
host-only evidence: no routed result, package or kit.
The [pc386 AT core M10K record](validation/2026-10-27-pc386-at-core-m10k/summary.md)
replaces the AT core's synthesis figures (its memories in M10K, inside its
budget) with a FreeDOS boot identical to M2's, clock for clock.

The [pc386 M3 audio record](validation/2026-10-26-pc386-m3-audio/summary.md)
checks the OPL2 sample for sample against Nuked-OPL3, the Sound Blaster DSP's
DMA and IRQ sequences, and start-up audio from the sound exercise and games
on the oracle machine. It is host-only simulation evidence.

Ownership that has already landed is [component boundaries](component-boundaries.md)
and the [project map](project-map.md). [Structure](fes-structure.md) only
points at those. It is not a second roadmap.

- [Kit software releases](kit-software-releases.md): persistent userspace bundles, queued activation and scoped recovery.
