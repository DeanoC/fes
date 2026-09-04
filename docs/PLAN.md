# Plan

This is the working plan, not a promise that later milestones are
implemented.

## Milestone 1 — Cyclone V HPS map

Encode the DE10-Nano / Cyclone V MMIO the native runtime uses, including
bitfields, not just bank bases. `make test` reproduces every symbol in
`testdata/oracles/libmister-runtime-fpga.yaml`.

In scope: platform, board, SoC, CPU, register banks, `validate`,
`report`, `emit-cpp`, `diff-oracle`.

Out of scope: consumer patches, ADV7513, a second board.

## Milestone 2 — Mega Drive as a system package

`packages/system/megadrive.yaml` holds expected core, RBF role/artifact,
media roles/indices/extensions, reset words, and input masks. The emitter
writes a C++14 table the runtime can compile. FogCast `systems/table.go`
stays the product table (aliases, covers, library roots). The image owns
the absolute RBF path.

Exit: `make test` diffs the package against
`testdata/oracles/libmister-runtime-megadrive.yaml`. SNES is not part of
this milestone.

## Milestone 3 — Consume in libmister-runtime

libmister-runtime checks in generated C++14 headers under
`src/native/generated/`. The emitter runs on a development host; the
headers are target text for the ARMv7 Linux HPS. The Pi / cross build
does not require Go and does not use the host compiler. Hand-written
FPGA-manager and SPI `constexpr`s and the production Mega Drive profile
literals are replaced. Runtime `ARCHITECTURE.md` records that. FogCast
stays untouched.

## Milestone 4 — FogCast launch fields, then SNES

FogCast keeps product fields (aliases, covers, Main RBF selector, library
roots). Mega Drive expected core and cartridge file index are generated
Go (`emit-go`) checked into FogCast. SNES is not part of this slice.

## Milestone 5 — Mega Drive core source pin (current)

`packages/source/megadrive_mister.yaml` pins
`MiSTer-devel/MegaDrive_MiSTer` at the Release 20260603 commit and the
official `releases/MegaDrive_20260603.rbf` hash. That pin is the upstream
artifact. `validate` does not clone. misteross fetches it and compiles a
separate Quartus Lite rebuild that has been loaded on real MiSTer
hardware. misteross selects the rebuild by default; upstream is the
fallback.

## Working rules

- Source of truth: package YAML, not Overlord YAML. Generated runtime
  headers are copies.
- Generated files are reviewed C++14 text, not a target-image build step.
- One board (`de10_nano`) until a second kit exists.
- Overlord stays frozen reference.
