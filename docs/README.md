# FES documentation

FES is the common starting point for FogCast and the native MiSTer components.
It selects compatible revisions, checks their shared definitions and builds the
host software and target root filesystem.

## Start here

| You want to… | Read |
| --- | --- |
| Set up a checkout, build outputs and run the host | [Getting started](getting-started.md) |
| Understand the parts, directories and terminology | [Project map](project-map.md) |
| Assign work to agents and integrate their results | [Agent workflow](agent-workflow.md), then [root AGENTS.md](../AGENTS.md) |
| Create component worktrees and use incremental builds | [Development guide](development.md) |
| Build, provision or verify a flashable native disk image | [Bootable media](bootable-media.md) |
| Build versioned appliance releases and use automatic update fallback | [Appliance releases](appliance-releases.md) |
| Decide where a change belongs or crosses a boundary | [Component boundaries](component-boundaries.md) |
| Continue the Pong, SNES and NES milestone | [Multi-system development](multi-system-development.md) |
| Develop and validate SNES cartridge saves | [SNES saves](snes-saves.md) |
| Share the kit between game and FPGA development sessions | [Kit sharing](kit-sharing.md) |

All shell examples in the parent guides start at the FES repository root unless
specified otherwise. Commands inside a component use that component's Makefile
and instructions; the same target name can mean different things there.

The current integration profile selects Mega Drive, Pong, SNES and NES. Exact
NES video and native session lifecycle acceptance for the selected image is
recorded in [the narrow-wire acceptance record](validation/2026-09-08-native-nes-wire-acceptance.md).

## What has been verified

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

## Earlier designs and plans

[Native parent design](native-parent-design.md),
[its implementation plan](implementation-plan.md), and the
[integration-entrypoint plan](superpowers/plans/2026-09-05-integration-entrypoint.md)
record earlier implementation stages. Use the guides above for current commands
and ownership. Future work in a design document is not an implemented feature.
