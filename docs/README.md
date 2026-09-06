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
| Decide where a change belongs or crosses a boundary | [Component boundaries](component-boundaries.md) |
| Continue the Pong and SNES milestone | [Multi-system development](multi-system-development.md) |
| Develop and validate SNES cartridge saves | [SNES saves](snes-saves.md) |
| Share the kit between game and FPGA development sessions | [Kit sharing](kit-sharing.md) |

All shell examples in the parent guides start at the FES repository root unless
specified otherwise. Commands inside a component use that component's Makefile
and instructions; the same target name can mean different things there.

## What has been verified

- [Dual-PLL native diagnostic](dual-pll-native-diagnostic.md): current gitlinks
  including misteross `cb89517`, diagnostic `make dev` image and bounded kit
  Pong/Mega Drive/SNES checks.
- [Integration validation](integration-validation.md): earlier selected
  revisions, clean two-pass image, QEMU and bounded physical-kit checks.
- [Incremental build validation](incremental-build-validation.md): cache reuse,
  development image checks, timing and interruption recovery.
- [Source build validation](source-build-validation.md): earlier source-built
  FPGA/image combination and its hardware observations.
- [Historical parent validation](historical-parent-validation.md): original
  parent build and provenance caveats.

Evidence describes the exact artifacts tested. It does not automatically apply
to later source edits, another profile or a different device.

## Earlier designs and plans

[Native parent design](native-parent-design.md),
[its implementation plan](implementation-plan.md), and the
[integration-entrypoint plan](superpowers/plans/2026-09-05-integration-entrypoint.md)
record earlier implementation stages. Use the guides above for current commands
and ownership. Future work in a design document is not an implemented feature.
