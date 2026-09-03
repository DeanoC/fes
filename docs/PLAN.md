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

## Milestone 2 — Mega Drive as a system package (current)

`packages/system/megadrive.yaml` holds expected core, RBF role/artifact,
media roles/indices/extensions, reset words, and input masks. The emitter
writes a C++ table the runtime could compile. FogCast `systems/table.go`
stays the product table (aliases, covers, library roots). The image owns
the absolute RBF path.

Exit: `make test` diffs the package against
`testdata/oracles/libmister-runtime-megadrive.yaml`. SNES is not part of
this milestone.

## Milestone 3 — Consume in libmister-runtime

Check generated headers into libmister-runtime (for example
`src/native/generated/`). The Pi build must not require Go. Replace the
hand-written `constexpr`s. Update runtime `ARCHITECTURE.md` in that same
commit. FogCast stays untouched.

## Milestone 4 — FogCast launch fields, then SNES

FogCast keeps product fields. File index and expected core come from the
system package (or from the runtime protocol). Add `system.snes` as data.

## Working rules

- Oracle for now: libmister-runtime source, not Overlord YAML.
- Generated files are reviewed text, not a target-image build step.
- One board (`de10_nano`) until a second kit exists.
- Overlord stays frozen reference.
