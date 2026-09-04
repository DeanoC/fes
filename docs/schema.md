# Schema `mister-packages.v1`

Every package file starts with:

```yaml
schema: mister-packages.v1
kind: platform | board | soc | cpu | register_bank | system | core_source
id: unique.dot.or.slash.free.id
```

Paths in a platform file are relative to `packages/`.

Hex values are YAML scalars such as `0xff706000`. The loader accepts
`0x` hex or decimal.

## platform

Selects one board, one SoC, and one CPU. There is no instance graph and
no connection list.

```yaml
kind: platform
id: de10_nano
board: board/de10_nano.yaml
soc: soc/cyclone_v.yaml
cpu: cpu/arm_cyclone_v.yaml
```

## board

Vendor, FPGA part, clocks, and pins. Pins are descriptive; this schema
does not generate XDC or Quartus constraints.

## soc

Physical windows (base and size), AXI-style buses, and named register
banks. A bank has a base address and a `registers:` file path.

Windows exist because register banks only expose a base. Main_MiSTer and
the native runtime map whole `/dev/mem` apertures.

## cpu

Target triple, architecture, width, and core count. This is emit context,
not a compiler.

## register_bank

A list of registers with offsets, optional `address_symbols`, optional
whole-register `writes`, and fields.

```yaml
kind: register_bank
id: cyclone_v.fpga_manager
registers:
  - name: STAT
    offset: 0x0
    address_symbols: [kFpgaStatusAddress]
    fields:
      - name: MODE
        bits: "2:0"
        mask_symbol: kFpgaModeMask
        values:
          - symbol: kFpgaModeReset
            value: 0x1
      - name: MSEL
        bits: "7:3"
        mask_symbol: kFpgaMselMask
        shift_symbol: kFpgaMselShift
```

`bits` is `hi:lo` or a single bit. The emitter computes mask and shift
from that range.

Field `values` default to **field** placement (the unshifted number, as
`kFpgaModeReset = 0x1`). `placement: register` shifts the value into the
register (`kFpgaCoreReset = 0x40000000`).

`writes` are named constants for whole-register values that are not a
bitfield encoding (`kSdrFpgaPortsEnabled = 0x3fff`).

`address_symbols` may list more than one name when two runtime headers
share an address (`kFpgaGpoAddress` and `kSpiGpoAddress`).

## system

A runtime profile: expected core identity, RBF role and artifact name,
media rules, reset/status words, and input masks. It is not a FogCast
product row and it does not store an image install path.

```yaml
kind: system
id: megadrive
expected_core: MegaDrive
rbf:
  role: core
  artifact: megadrive.rbf
media:
  - role: cartridge
    index: 1
    required: true
    extensions: [.md, .gen, .bin]
    maximum_size: 0x2000000
core:
  reset_assert_word: 0x0001
  initial_status_word: 0x0001
  reset_release_word: 0x0000
  file_wire: little_endian_byte_pairs
input:
  player_count: 1
  player_command: 0x02
  up: 0x0008
  down: 0x0004
  left: 0x0002
  right: 0x0001
  a: 0x0010
  b: 0x0020
  c: 0x0040
  start: 0x0080
```

`file_wire` is currently only `little_endian_byte_pairs`. Input masks
must be unique powers of two. `player_count` is 1 because that is what
the runtime profile accepts today.

The image prefix (`/usr/share/mister-runtime/cores/`) is not a package
field. FogCast aliases, covers, and library roots stay in FogCast.

A system `rbf.source` path may point at a `core_source` file. That pin is
data, not a clone action.

## core_source

Pinned git identity of a buildable FPGA core tree, plus the known-good
release RBF hash. `validate` does not clone or build.

```yaml
kind: core_source
id: megadrive_mister
repository: https://github.com/MiSTer-devel/MegaDrive_MiSTer
commit: 7365a137cfd8fa6f041e964d8b953159c0ec42d9
rbf_path: releases/MegaDrive_20260603.rbf
rbf_sha256: 0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839
rbf_size: 4296864
project: MegaDrive.qpf
```

The hashed `rbf_path` is the upstream known-good RBF for that git commit.
misteross also compiles a Quartus Lite rebuild from the same pin; that
rebuild has been loaded on real MiSTer hardware. misteross selects the
rebuild by default; the official RBF is the fallback. They are not
required to bit-match. Fetch and compile belong in misteross.

Emitted C++ is C++14 for the ARMv7 Linux HPS target, the dialect the
runtime's Arm GNU toolchain uses. Namespace-scope `constexpr` integers
and `static constexpr` tables; no inline variables. The emitter runs on
a development host. The target image build does not compile this tree
and does not use the host compiler.

`emit-go` writes FogCast launch constants for a `system` package:
expected core identity and the cartridge media index. It does not emit
aliases, covers, Main RBF paths, or library roots.

## What is intentionally missing

- Connections, prefabs, wildcards, and bus address allocation.
- SNES and further system packages.
- Actions, templates, and git clones. `core_source` is a pin, not a
  clone recipe. Fetch and Quartus rebuild belong in misteross.
