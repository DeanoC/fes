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

`file_wire` selects how the runtime places media bytes in the 16-bit SPI
transport. `little_endian_byte_pairs` sends two adjacent bytes in each word;
`little_endian_bytes` sends one byte in the low eight bits of each word. The
NES core uses the latter because its `hps_io` instance has `WIDE=0`. Input masks
must be unique powers of two when nonzero. Up/Down/Left/Right/A/B/Start
are required. C and X/Y/L/R/Select default to zero (unsupported); zero
optional masks do not participate in overlap checks. `player_count` is 1 because that is what
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

Generated system headers share declarations protected by
`MISTER_PACKAGES_GENERATED_SYSTEM_TYPES_V2`, so distinct system headers can
be included together. Regenerate all consumer system headers together when
adopting this format; older unguarded headers cannot be mixed with it. The V2 layout appends media `transform` and input `x/y/l/r/select` fields;
regenerate all headers together. Existing Mega Drive values are unchanged.

A system may omit `media` or supply an empty list. Its C++ table contains
`nullptr` and a zero media count, without a zero-length array. This describes
media requirements; it does not establish a core's hardware compatibility.
Emitter tests syntax-check generated text with a development-host C++ compiler
(`CXX`, default `c++`) using `-std=c++14 -pedantic-errors`; they do not compile
the runtime or produce target objects.

`emit-go` writes FogCast launch constants for a `system` package:
system/expected core identity and, when a cartridge role exists, its media
index. Without that role no `CartridgeIndex` symbol is emitted. Existing
cartridge-bearing output is unchanged. It does not emit
aliases, covers, Main RBF paths, or library roots.

The NES source package `packages/source/nes_mister.yaml` pins upstream
Release20260823 and its verified official RBF. The matching
`system/nes.yaml` declares the first `FS,NESFDSNSF` entry using native
filetype index `0x40`: bits 7:6 select an NES cartridge and bits 5:0 select
the filesystem slot. This differs from the product library's zero-based
selector and is required by the core's `filetype` decoder.

The SNES source package `packages/source/snes_mister.yaml` pins upstream
Release20260823 and its verified official RBF. The matching `system/snes.yaml` declares native cartridge index 1 and
the `snes_cartridge` transform; legacy Main index 0 remains a FogCast
product selector. Neither package establishes hardware acceptance.

## What is intentionally missing

- Connections, prefabs, wildcards, and bus address allocation.
- SNES enhancement-chip/auxiliary-firmware profiles.
- Actions, templates, and git clones. `core_source` is a pin, not a
  clone recipe. Fetch and Quartus rebuild belong in misteross.

## Pong contract

`packages/system/pong.yaml` describes the ROM-less misteross Pong wrapper:
identity `Pong`, artifact `pong.rbf`, no media, reset assert/initial status 1
and release 0. Joystick command 0x02 uses Up 0x08, Down 0x04 and Start 0x80.
Left/Right/A/B/C preserve their existing packet bits as reserved inputs ignored
by Pong. This is a software integration contract; wrapper build and physical
video/input/audio acceptance remain separate. No guessed upstream core source
is attached.

## Native SNES transfer

Media `transform` defaults to `raw`; the only other accepted value is
`snes_cartridge`. It is a runtime transfer recipe, not a host-side ROM
rewrite. The runtime validates ordinary LoROM/HiROM, strips a validated
copier header, and sends one synthesized 512-byte metadata prefix followed
by the original normalized ROM. The `maximum_size` of `0x400200` permits
up to 4 MiB payload plus a 512-byte copier header; the runtime applies
stricter format and mapping limits before hardware mutation. Unrecognized
transform names fail validation. The oracle comparison includes transforms
and optional input masks, with omitted transform interpreted as `raw`.

SNES command 0x02 uses Right/Left/Down/Up bits 0/1/2/3, A/B/X/Y bits
4/5/6/7, L/R bits 8/9, Select bit 10, and Start bit 11. C is absent. Reset
assert and initial status are 1, release is 0: ROM mapping and region stay
in Auto mode, using the synthesized metadata. Ordinary cartridges require
no external coprocessor firmware. Enhancement chips, interleaved dumps,
ExHiROM, BS-X/Sufami, and persistent saves remain outside this profile's
initial runtime scope.
