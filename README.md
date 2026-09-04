# mister-packages

Board, SoC, MMIO, and system-profile packages for FogCast and
libmister-runtime. This repository is the source of truth for those
descriptions. A small Go emitter turns them into headers and reports.

It is not a runtime, not a game catalog, not a bitstream builder, and not
an Overlord port.

## What works now

- Schema `mister-packages.v1` for `platform`, `board`, `soc`, `cpu`,
  `register_bank`, `system`, and `core_source`.
- One platform: DE10-Nano / Cyclone V HPS.
- Register banks and bitfields for the MMIO the native runtime actually
  uses (FPGA manager, SYSMGR FPGA interface, SDR port reset, bridge reset,
  L3 remap, SPI GPO/GPI strobes).
- One system package: `megadrive` (expected core, RBF role/artifact, media
  rules, reset words, input masks). FogCast product fields stay in FogCast.
- Mega Drive `core_source` pin: MiSTer-devel git commit plus the hashed
  official release RBF (the upstream artifact). This tree does not clone
  or run Quartus. misteross fetches that pin, compiles a Quartus Lite
  rebuild that has been loaded on real MiSTer hardware, and selects the
  rebuild by default.
- `mister-packages validate`, `report`, `emit-cpp`, `emit-go`, and
  `diff-oracle` on platform or system YAML. `emit-go` writes FogCast
  launch fields (expected core, cartridge index) from a system package.
- Oracles extracted from libmister-runtime FPGA-manager/SPI headers and
  from the production Mega Drive profile. `make test` requires both to
  match.

libmister-runtime checks in generated C++14 headers under
`src/native/generated/`. This emitter is host software; those headers
are target text for the ARMv7 Linux HPS. The Pi / cross build does not
run Go and does not use the host compiler. FogCast is not patched.

## What this is not (yet)

- SNES or a second system package. Mega Drive is the format proof.
- ADV7513 I2C tables. Leave those in `adv7513.hpp` until the HPS map is
  consumed.
- Connection solving, Verilog tops, XDC, Edalize, SVD, git-clone actions,
  or a second board.
- A FogCast or libmister-runtime *build* dependency on Go. Generated
  headers are reviewed text copied into the runtime.

## Boundaries

| Repository | Owns |
| --- | --- |
| `mister-packages` | Package YAML and the emitter |
| `libmister-runtime` | Lifecycle, FPGA programming, profiles at runtime |
| `FogCast` | Host UI, game catalog, agent, images |
| `misteross` | RBF production |
| `overlord` / `ikuy_std_resources` | Frozen reference only |

**Package YAML is the source of truth.** libmister-runtime keeps generated
headers as reviewed text. If YAML and those headers disagree, regenerate
the headers from this tree. Do not hand-edit them.

Do not call this tree a catalog. FogCast already uses that word for games.

## Build and test

```sh
make test
make report
make emit-cpp
```

`make test` runs unit tests, validates `packages/platform/de10_nano.yaml`,
`packages/system/megadrive.yaml`, and
`packages/source/megadrive_mister.yaml`, and diffs them against the
matching files in `testdata/oracles/`.

Requires Go 1.22 or later.

## Plan

See [docs/PLAN.md](docs/PLAN.md) and [docs/schema.md](docs/schema.md).

1. **Milestone 1:** Cyclone V HPS map round-trips the runtime constants.
2. **Milestone 2:** `system.megadrive` as data.
3. **Milestone 3:** libmister-runtime checks in generated C++14 headers.
4. **Milestone 4:** Mega Drive expected core and file index as generated
   Go. SNES is later.
5. **Milestone 5 (this tree):** Mega Drive core git pin. Fetch/build in
   misteross.

## Source notes

The DE10-Nano / Cyclone V numbers started from
`ikuy_std_resources/catalog/fogcast`, then were completed from
libmister-runtime so bitfields and absolute addresses match the running
code. The old Overlord YAML used bank bases and `VALUE 31:0`; that is not
enough to replace `fpga_manager.hpp`.
