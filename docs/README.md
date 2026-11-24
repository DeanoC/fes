# FES documentation

Read one of these. The rest of `docs/` is either a procedure for that job or
a dated record. A dated record is not the schedule.

## Start here

| You want to… | Read |
| --- | --- |
| See the single roadmap, target architecture, quality tiers and phases | [Forward plan](plans/forward-plan.md) |
| See v1 DE10 release scope, first-boot setup, licensing and GTM | [v1 first-boot and setup](plans/v1-first-release-first-boot-setup.md), [GTM plan](plans/de10-first-release-gtm.md), [v1 firmware licensing](plans/v1-firmware-licensing.md) |
| Design or implement the FES user experience | [FES UX design](ux/README.md), [native references and Library target](ux/references/README.md), [concepts](ux/concepts/README.md), [acceptance](ux/acceptance.md) |
| See which cores exist, their ABI, interfaces and standing | [Core status](core-status.md) |
| See which boards and toolchains each core supports, and its support tier | [Core support matrix](support-matrix.md) |
| Read the accepted Quartus policy, front-end lanes, identity policies and host leases | [Quartus-built cores](design/quartus-cores.md), [RTL front-end lanes](design/rtl-frontend-lanes.md), [Quartus hosts and leases](design/quartus-hosts.md) |
| See whether cores match current FPGA choices, and the retirement order | [Core alignment](core-alignment.md) |
| Set up a checkout and run the host | [Getting started](getting-started.md) |
| Change code: worktree, builds, handoff | [Development](development.md), then [agent workflow](agent-workflow.md) |
| Coordinate PR lanes, review and milestone delivery | [Agent workflow](agent-workflow.md#github-delivery-workflow), [AGENTS.md](../AGENTS.md#pr-workflow-lanes-merge-gate-review-stacks-hil-ops) |
| Decide whether a PR needs a full image or DE25/DE10 kit HIL | [HIL plan](hil-plan.md), [DE25 kit HIL](de25-kit-hil.md), [kit sharing](kit-sharing.md) |
| See which module owns a change | [Project map](project-map.md), [component boundaries](component-boundaries.md) |
| Develop DE25-Nano Linux platform, board shells and kernel DMA ownership | [DE25 Linux platform](design/de25-linux-platform.md), [DE25-Nano Linux](de25-linux.md), [DE25 board shells](../sources/misteross/boards/de25nano/README.md), [DMA ownership](../image/kernel/de25-dma/README.md) |
| Develop DE25-Nano without Linux (opt-in TamaGo bare-metal stack) | [Embedded base](../platform/embedded/README.md), [architecture & flow diagrams](../platform/embedded/ARCHITECTURE.md), [TamaGo runtime and QEMU suites](../platform/embedded/tamago/README.md) |
| Build, provision or verify a flashable disk or appliance release | [Bootable media](bootable-media.md), [appliance releases](appliance-releases.md) |
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
| Build, install and activate persistent kit-software bundles | [Kit software releases](kit-software-releases.md), [kit software acceptance](kit-software-acceptance.md) |
| Stage and serve bundled firmware (Spectrum ROMs, EmuTOS, FreeDOS disk) | [Bundled firmware](bundled-firmware.md) |
| Core settings, progress and writable ST disks | [Core persistence](core-persistence.md) |
| Blob versus blob-stream capacity | [Media capacity](core-media-evolution.md) |
| Use or test the SoC DDR3 from a core | [HPS DDR](hps-ddr.md) |
| Prepare DE25 DDR, scanout and bring-up diagnostics | [DE25 DDR](../sources/mister-packages/docs/de25-ddr.md), [DE25 scanout](../sources/mister-packages/docs/de25-scanout.md), [combined diagnostic](../sources/misteross/boards/de25nano/combined/README.md), [scanout diagnostic](../sources/misteross/boards/de25nano/scanout/README.md), [DE25 ZX81](../sources/misteross/boards/de25nano/zx81/README.md), [HPS-controlled ZX81](../sources/misteross/boards/de25nano/zx81-hps/README.md) |
| Show launcher overlays or a surround over a running core | [Display pipeline](display-pipeline.md) |
| ZX81 package, expansion cart, tape design | [FES ZX81](fes-zx81.md), [expansion bus](zx81-expansion-bus.md), [tape media](zx81-tape-media.md) |
| Freeze-scaffold cartridge experiments | [FPGA expansion](fpga-expansion.md) |
| Who assembles `linux.img` and prebuilt image inputs | [Image assembly](image-assembly.md), [image artifacts](image-artifacts.md) |
| Which outputs are versioned | [Artifact identities](artifacts.md) |
| Read source, CI, build and hardware evidence | [Status](status.md) |
| Run only the tests a change affects (including the TamaGo CI lane) | [Focused tests](test-changed.md) |
| Rooms / Stop-idle design that is not built yet | [Idle MENU → rooms](idle-menu-rooms.md), [phase 2](idle-menu-rooms-phase2.md). Present tense on that page, outside “What is true now”, still describes the old MENU idle (`menu.rbf`, `mister_v1`). Current idle install paths are [image assembly](image-assembly.md) and `image/build/native-inputs.toml`. |
| The kit as its own host: sources, sessions, what the mesh drafts still mean | [Mesh under kit-as-host](mesh-kit-host.md) (22 Oct 2026 decisions, #863) |
| Earlier mesh drafts, historical on the kit side | [Mesh LAN](mesh-lan.md), [node protocol](mesh-node-protocol.md), [phase 1](mesh-phase1.md), [phase 2](mesh-phase2.md), [phase 3](mesh-phase3.md), [v-next plan](mesh-vnext.md) |
| See when a soft restart asks for a board reboot | [Soft-restart Path B](soft-restart-path-b.md). The kit check in that note is on hold. |

## Not current instructions

- **[validation/](validation/)** records what a named artifact did on a named
  day. Dated validation records accept only the exact artifact they name;
  current core standing and active evidence links live in
  [core status](core-status.md), [core support matrix](support-matrix.md) and
  [DE25 Linux platform](design/de25-linux-platform.md).
- **[superpowers/](superpowers/)** holds only the five historical September 2026
  plans and specs still linked from dated validation records or their transitive
  references; the other 62 `docs/superpowers/` files were archived in Git
  history (#2124, #2129). Do not treat them as instructions for the tree you
  have open. The root `.superpowers/sdd/` task working reports were removed from
  the active tree and remain in Git at
  `bb5f1bfc14026b55113202fbc88b730241da9ee1` (list with
  `git ls-tree -r --name-only bb5f1bfc -- .superpowers/sdd/`).
- **[Structure](fes-structure.md)** is a short pointer to
  [component boundaries](component-boundaries.md) and the
  [project map](project-map.md), not a second roadmap.
