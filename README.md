# mister-packages

Board, SoC, MMIO, and system-profile packages for FogCast and
libmister-runtime. This repository is the source of truth for those
descriptions. A small Go emitter turns them into headers and reports.

It is not a runtime, not a game catalog, not a bitstream builder, and not
an Overlord port.

## What works now

- Schema `mister-packages.v1` for `platform`, `board`, `soc`, `cpu`,
  `register_bank`, and `system`.
- One platform: DE10-Nano / Cyclone V HPS.
- Register banks and bitfields for the MMIO the native runtime actually
  uses (FPGA manager, SYSMGR FPGA interface, SDR port reset, bridge reset,
  L3 remap, SPI GPO/GPI strobes).
- One system package: `megadrive` (expected core, RBF role/artifact, media
  rules, reset words, input masks). FogCast product fields stay in FogCast.
- `mister-packages validate`, `report`, `emit-cpp`, and `diff-oracle` on
  platform or system YAML.
- Oracles extracted from libmister-runtime FPGA-manager/SPI headers and
  from the production Mega Drive profile. `make test` requires both to
  match.

No consumer repository is patched. Generated C++ is not checked into
libmister-runtime yet.

## What this is not (yet)

- SNES or a second system package. Mega Drive is the format proof.
- ADV7513 I2C tables. Leave those in `adv7513.hpp` until the HPS map is
  consumed.
- Connection solving, Verilog tops, XDC, Edalize, SVD, git-clone actions,
  or a second board.
- A FogCast or libmister-runtime build dependency.

## Boundaries

| Repository | Owns |
| --- | --- |
| `mister-packages` | Package YAML and the emitter |
| `libmister-runtime` | Lifecycle, FPGA programming, profiles at runtime |
| `FogCast` | Host UI, game catalog, agent, images |
| `misteross` | RBF production |
| `overlord` / `ikuy_std_resources` | Frozen reference only |

Until a consumer switches, **libmister-runtime source is the oracle**. If
YAML and the runtime disagree, the runtime wins and the package is wrong.

Do not call this tree a catalog. FogCast already uses that word for games.

## Build and test

```sh
make test
make report
make emit-cpp
```

`make test` runs unit tests, validates `packages/platform/de10_nano.yaml`
and `packages/system/megadrive.yaml`, and diffs them against
`testdata/oracles/libmister-runtime-fpga.yaml` and
`testdata/oracles/libmister-runtime-megadrive.yaml`.

Requires Go 1.22 or later.

## Plan

See [docs/PLAN.md](docs/PLAN.md) and [docs/schema.md](docs/schema.md).

1. **Milestone 1:** Cyclone V HPS map round-trips the runtime constants.
2. **Milestone 2 (this tree):** `system.megadrive` as data. Still no
   consumer patch.
3. **Milestone 3:** libmister-runtime checks in generated headers.
4. **Milestone 4:** FogCast launch fields, then SNES as data.

## Source notes

The DE10-Nano / Cyclone V numbers started from
`ikuy_std_resources/catalog/fogcast`, then were completed from
libmister-runtime so bitfields and absolute addresses match the running
code. The old Overlord YAML used bank bases and `VALUE 31:0`; that is not
enough to replace `fpga_manager.hpp`.
