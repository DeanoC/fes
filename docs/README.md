# FES documentation

FES is the common starting point for FogCast and the native MiSTer components.
It selects compatible revisions, checks their shared definitions and builds the
host software and target root filesystem.

## Start here

| You want to… | Read |
| --- | --- |
| Set up a checkout, build outputs and run the host | [Getting started](getting-started.md) |
| Understand the parts, directories and terminology | [Project map](project-map.md) |
| Review landed refactors and remaining ownership decisions | [Structure and refactor status](fes-structure.md) |
| See which binaries, images and cores are versioned artifacts | [Artifact identities](artifacts.md) |
| See who owns native image assembly | [Image assembly ownership](image-assembly.md) |
| Assign work to agents and integrate their results | [Agent workflow](agent-workflow.md), then [root AGENTS.md](../AGENTS.md) |
| Create component worktrees and use incremental builds | [Development guide](development.md) |
| Build, provision or verify a flashable native disk image | [Bootable media](bootable-media.md) |
| Build versioned appliance releases and use automatic update fallback | [Appliance releases](appliance-releases.md) |
| Build, install and select described FPGA core packages | [Described FPGA core packages](core-packages.md) |
| Decide where a change belongs or crosses a boundary | [Component boundaries](component-boundaries.md) |
| Review the historical Pong, SNES and NES milestone | [Multi-system development](multi-system-development.md) |
| Develop and validate SNES cartridge saves | [SNES saves](snes-saves.md) |
| Use described-core settings and progress | [Core persistence](core-persistence.md) |
| Use the FES ZX81 computer package | [FES ZX81](fes-zx81.md) |
| Share the kit between game and FPGA development sessions | [Kit sharing](kit-sharing.md) |

All shell examples in the parent guides start at the FES repository root unless
specified otherwise. Commands inside a component use that component's Makefile
and instructions; the same target name can mean different things there.

The current integration profile is package-only and installs the ordered closed
format-2 set `fes.pong`, `fes.zx81` and `fes.coleco` through HIP/nextpnr. The historical Mega Drive, Pong, SNES and NES
catalog, including exact NES video and native session lifecycle acceptance, is
recorded in [the narrow-wire acceptance record](validation/2026-09-08-native-nes-wire-acceptance.md)
and is not current-profile acceptance.

## What has been verified

- [FES SMS parent pin](validation/2026-09-17-fes-sms-parent-pin.md):
  misteross `#65` pin and `fes.sms` recipe registration for package-only
  acceptance; factory image closed set unchanged.
- [FES SMS package-only host-library acceptance](validation/2026-09-17-fes-sms-package-acceptance.md):
  sealed `fes.sms` inspect + loopback import/select on tip FES; no kit HIL.
- [FES SMS package-only kit launch/Stop HIL](validation/2026-09-17-fes-sms-kit-hil.md):
  leased mister launch+Stop for sealed `fes.sms`; temporary host; lease released.
- [Coleco VDP reads/NMI](validation/2026-09-12-coleco-vdp-io.md): buffered
  VRAM reads, held status reads and the BIOS-free VBlank interrupt diagnostic.
- [Coleco controllers](validation/2026-09-12-coleco-controllers.md): standard
  joystick/keypad mode selection, two fire buttons and encoded keypad diagnostics.
- [Coleco input diagnostic](validation/2026-09-12-coleco-input.md): two-player
  keyboard-driven cartridge and input validation.
- [Coleco cartridge diagnostic](validation/2026-09-12-coleco-media.md):
  open Z80 cartridge, reset/VDP fixes, paired Quartus/nextpnr build evidence
  and the exact status of leased media/graphics validation.
- [Coleco Graphics II sprites](validation/2026-09-12-coleco-sprites.md):
  bounded sprite rendering, sprite-status behavior, paired compiler evidence
  and the current exact-artifact hardware gate.
- [Coleco current-pin integration](validation/2026-09-13-coleco-nextpnr-integration.md):
  parent pin selection, current OSS/Quartus package identities and exact-kit
  acceptance for the selected nextpnr recipe.
- [Appliance first-boot expand implement](validation/2026-09-11-appliance-first-boot-expand-implement.md):
  host expander and tests landed; assembly stays the fixed 1 GiB image.
- [Appliance first-boot expand spare HIL](validation/2026-09-10-appliance-first-boot-expand-hil.md):
  exact USB spare p3 format and read-back; live `.4` untouched.
- [Appliance first-boot expand spike](validation/2026-09-10-appliance-first-boot-expand-spike.md):
  extra-p3 recommendation and host dry-run.
- [Persistence merge reconciliation](validation/2026-09-10-core-persistence-merge.md):
  combined-source checks after integrating newer component main branches.
- [Core persistence](validation/2026-09-09-core-persistence.md): settings, progress,
  version compatibility and save-failure recovery; see the record for acceptance status.
- [Core package library](validation/2026-09-09-core-package-library.md):
  installed versions, normal library launch, checked selection and rollback.
- [RBF ABI acceptance](validation/2026-09-09-rbf-abi-acceptance.md):
  described-core identity, standalone Pong, input and recovery checks.

- [Dual-PLL native diagnostic](dual-pll-native-diagnostic.md): FES `f34c84c`
  selecting misteross `cb89517`, diagnostic `make dev` image and bounded kit
  Pong/Mega Drive/SNES checks. The current parent gitlink is later than that
  record.
- [Integration validation](integration-validation.md): earlier selected
  revisions, clean two-pass image, QEMU and bounded physical-kit checks.
- [Incremental build validation](incremental-build-validation.md): cache reuse,
  development image checks, timing and interruption recovery.
- [Source build validation](source-build-validation.md): earlier source-built
  FPGA/image combination and its hardware observations.
- [Historical parent validation](historical-parent-validation.md): original
  parent build and provenance caveats.
- [Native NES software integration](validation/2026-09-08-native-nes-software.md):
  exact NES provenance, four-system contract checks and the earlier pending
  hardware gate.
- [Native NES narrow-wire acceptance](validation/2026-09-08-native-nes-wire-acceptance.md):
  the corrected byte transfer, reproducible image and exact-kit colour captures.

Evidence describes the exact artifacts tested. It does not automatically apply
to later source edits, another profile or a different device.

## Described-core design

[Described FPGA cores and ABI dispatch](superpowers/specs/2026-09-08-rbf-abi-design.md)
defines format-2 core packages, raw-RBF MiSTer compatibility and the standalone
Mistral Pong milestone. The [implementation plan](superpowers/plans/2026-09-08-rbf-abi.md)
retains its task history and acceptance checks. The software and image-selection
path is implemented; physical acceptance still requires evidence for the exact
assembled image.

[Installed package library design](superpowers/specs/2026-09-09-core-package-library-design.md)
and its [implementation plan](superpowers/plans/2026-09-09-core-package-library.md)
describe immutable host installation, compatibility inspection and explicit
ROM-less library selections. The operator commands and UI-facing API contract
are linked from the [core package guide](core-packages.md).

[Persistent core settings and progress](superpowers/specs/2026-09-09-core-persistence-design.md)
is implemented: stable target-local data across compatible package versions,
starting with standalone Pong. See the [implementation plan](superpowers/plans/2026-09-09-core-persistence.md)
and [acceptance record](validation/2026-09-09-core-persistence.md).

[FES ZX81](superpowers/specs/2026-09-10-fes-zx81-design.md) is one of the
selected described-core packages: a custom GP ABI derived from the MiSTer
Quartus ZX81 implementation, with Quartus bring-up and nextpnr/mistral
evidence kept separate from the normal package-only build. See the
[implementation plan](superpowers/plans/2026-09-10-fes-zx81.md) and the
[ZX81 working page](fes-zx81.md).

## Earlier designs and plans

[Native parent design](native-parent-design.md),
[its implementation plan](implementation-plan.md), and the
[integration-entrypoint plan](superpowers/plans/2026-09-05-integration-entrypoint.md)
record earlier implementation stages. Use the guides above for current commands
and ownership. Future work in a design document is not an implemented feature.
