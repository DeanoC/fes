# mister-packages

Board, SoC, MMIO, and system-profile packages for FogCast and
libmister-runtime. This repository is the source of truth for those
descriptions. A small Go emitter turns them into headers and reports.

It is not a runtime, not a game catalog, not a bitstream builder, and not
an Overlord port.

## What works now

- Schema `mister-packages.v1` for `platform`, `board`, `soc`, `cpu`, and
  `register_bank`.
- One platform: DE10-Nano / Cyclone V HPS.
- Register banks and bitfields for the MMIO the native runtime actually
  uses (FPGA manager, SYSMGR FPGA interface, SDR port reset, bridge reset,
  L3 remap, SPI GPO/GPI strobes).
- `mister-packages validate`, `report`, `emit-cpp`, and `diff-oracle`.
- An oracle extracted from libmister-runtime `fpga_manager.hpp` and
  `spi.hpp`. `make test` requires the emitter to reproduce those constants.

No consumer repository is patched. Generated C++ is not checked into
libmister-runtime yet.

## What this is not (yet)

- System profiles (`megadrive`, SNES, …). That is milestone 2.
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

`make test` runs unit tests, validates `packages/platform/de10_nano.yaml`,
and diffs emitted symbols against
`testdata/oracles/libmister-runtime-fpga.yaml`.

Requires Go 1.22 or later.

## Plan

See [docs/PLAN.md](docs/PLAN.md) and [docs/schema.md](docs/schema.md).

1. **Milestone 1 (this tree):** Cyclone V HPS map round-trips the runtime
   constants. Done when `make test` is green.
2. **Milestone 2:** `system.megadrive` as data. Still no consumer patch.
3. **Milestone 3:** libmister-runtime checks in generated headers.
4. **Milestone 4:** FogCast launch fields, then SNES as data.

## Source notes

The DE10-Nano / Cyclone V numbers started from
`ikuy_std_resources/catalog/fogcast`, then were completed from
libmister-runtime so bitfields and absolute addresses match the running
code. The old Overlord YAML used bank bases and `VALUE 31:0`; that is not
enough to replace `fpga_manager.hpp`.
