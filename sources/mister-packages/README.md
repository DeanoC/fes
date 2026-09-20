# mister-packages

Board, SoC, MMIO, and system-profile packages for FogCast and
libmister-runtime. This repository is the source of truth for those
descriptions. A small Go emitter turns them into headers and reports.

It is not a runtime, not a game catalog, not a bitstream builder, and not
an Overlord port.

## What works now

- Schema `mister-packages.v1` for `platform`, `board`, `soc`, `cpu`,
  `register_bank`, `system`, `core_source`, `abi`, and `programming_profiles`.
- JSON Schema and language-neutral conformance fixtures for format-2 `.fcore`
  manifests. These describe package structure and reader semantics; they do not
  emit or load bitstreams.
- One platform: DE10-Nano / Cyclone V HPS.
- Register banks and bitfields for the MMIO the native runtime actually
  uses (FPGA manager, SYSMGR FPGA interface, SDR port reset, bridge reset,
  L3 remap, SPI GPO/GPI strobes).
- System packages: `megadrive`, ROM-less `pong`, and native `snes` (expected core, RBF
  role/artifact, media rules, reset words, input masks). Pong defines the
  agreed misteross wrapper contract; physical support is not established.
  FogCast product fields stay in FogCast.
- Mega Drive `core_source` pin: MiSTer-devel git commit plus the hashed
  official release RBF (the upstream artifact). This tree does not clone
  or run Quartus. misteross fetches that pin, compiles a Quartus Lite
  rebuild that has been loaded on real MiSTer hardware, and selects the
  rebuild by default.
- SNES `core_source` pin: MiSTer-devel Release20260823 git commit and
  verified official RBF hash/size. The native profile declares ordinary
  cartridges with a runtime-owned metadata prefix and twelve SNES controls.
  The profile does not establish hardware acceptance.
- `mister-packages validate`, `report`, `emit-cpp`, `emit-go`, `emit-verilog`, and
  `diff-oracle` on platform or system YAML. `emit-go` writes FogCast
  launch fields (expected core, cartridge index) from a system package.
- Declarative FES GP mailbox constants and the DE10-Nano programming
  profile/ABI-major registry. `fes.simple-game` 1.0 and `fes.simple-computer`
  1.0 are both approved on `fes-gp-v1`. Emitters produce reviewed C++14, Go,
  and guarded Verilog constant text; they do not produce modules, netlists,
  MMIO maps, or lifecycle recipes.
- Oracles extracted from libmister-runtime FPGA-manager/SPI headers and
  from the production Mega Drive profile. `make test` requires both to
  match.

libmister-runtime checks in generated C++14 headers under
`src/native/generated/`. This emitter is host software; those headers
are target text for the ARMv7 Linux HPS. The Pi / cross build does not
run Go and does not use the host compiler. FogCast is not patched.

## What this is not (yet)

- SNES enhancement chips, auxiliary firmware, expanded mappings, or
  persistent save support.
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
python3 -m venv .venv
.venv/bin/python -m pip install -r requirements-test.txt
make test PYTHON=.venv/bin/python
```

The Python dependencies are host/test-only. They validate the checked-in JSON
Schema with Draft 2020-12, including registered RFC 3986 URI format checking,
and are not used by generated consumers or target images.
Regenerate or check the synthetic, never-deployed cross-language fixtures with:

```sh
make fixtures PYTHON=.venv/bin/python
make check-fixtures PYTHON=.venv/bin/python
```

The package reports and emitters can be run separately with:

```sh
make report
make emit-cpp
make emit-go
make emit-verilog
```

`make test` validates the format-2 schema and fixtures, runs Go unit tests,
validates `packages/platform/de10_nano.yaml`,
`packages/system/megadrive.yaml`, `packages/system/pong.yaml`,
`packages/system/snes.yaml`, and
`packages/source/megadrive_mister.yaml`, and diffs them against the
matching Mega Drive/platform files in `testdata/oracles/`. Pong has no
historical hardware oracle. It also validates the real
`packages/source/snes_mister.yaml` source pin; this does not fetch or build it.
It also validates the ABI definitions (`fes.simple-game`,
`fes.simple-computer`, and `mister`) and the DE10-Nano programming profile
registry.

Requires Python 3.11 or later, Go 1.22 or later, and a C++14 compiler (`CXX`,
default `c++`) for generated-header syntax tests.

## Plan

The additive [application I/O contract](docs/application-io.md) defines
`fes.application` 1.0 (tag 3) on `fes-gp-v1`, composing fixed video with optional
gamepad, media, fixed 48 kHz stereo PCM audio, and an optional 8192-byte
firmware overlay. Audio uses capability bit 4 and no new mailbox command.
Firmware uses capability bit 7 and opcodes 15–17. Legacy ABI definitions remain
unchanged.
The additive `fes.gamepad.ports` and `fes.keypad.ports` interfaces define two
logical digital controller ports and optional twelve-key keypads, using bits
5/6 and opcodes 13/14. They preserve the existing one-player interface.

The canonical [media stream 1.0 contract](docs/media-stream.md) extends the
simple-computer ABI without widening legacy blob 1.0. YAML constants and
synthetic fixtures are shared definitions, not runtime or hardware acceptance.

See [docs/PLAN.md](docs/PLAN.md) and [docs/schema.md](docs/schema.md).

1. **Milestone 1:** Cyclone V HPS map round-trips the runtime constants.
2. **Milestone 2:** `system.megadrive` as data.
3. **Milestone 3:** libmister-runtime checks in generated C++14 headers.
4. **Milestone 4:** Mega Drive expected core and file index as generated
   Go. Native SNES uses its own index; legacy Main selectors stay in FogCast.
5. **Milestone 5 (this tree):** Mega Drive core git pin. Fetch/build in
   misteross.

## Source notes

The DE10-Nano / Cyclone V numbers started from
`ikuy_std_resources/catalog/fogcast`, then were completed from
libmister-runtime so bitfields and absolute addresses match the running
code. The old Overlord YAML used bank bases and `VALUE 31:0`; that is not
enough to replace `fpga_manager.hpp`.
