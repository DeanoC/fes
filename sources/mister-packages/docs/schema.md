# Schemas

## Core bundle manifest format 2

[`schema/core-bundle-v2.json`](../schema/core-bundle-v2.json) is the Draft
2020-12 structural schema for a parsed format-2 `manifest.toml`. It requires
only the declared fields and rejects unknown fields. ABI and interface IDs are
validated as well-formed IDs; compatibility with a runtime registry is a
separate consumer decision, so an unknown but well-formed ABI remains
inspectable.

The root contains `format = 2` and the tables `core`, `target`, `payload`,
`abi`, `interfaces`, and `build`. `core.system` is the only optional field. A
non-MiSTer package may omit it, and `interfaces` may be empty.

- Core, ABI, interface, platform, profile, and optional system IDs match
  `[a-z][a-z0-9_.-]{0,95}`.
- Core names contain 1 through 128 UTF-8 bytes. Descriptions contain at most
  2,048 UTF-8 bytes and may be empty. Full SemVer 2.0.0 syntax is accepted for
  `core.version`, including prerelease and build metadata.
- ABI and interface majors are 1 through 65,535; minors are 0 through 65,535.
  The TOML values must be integers, not integral floats or Booleans.
- Payloads are named `core.rbf`, contain 1 through 33,554,432 bytes, and carry
  their exact size and lowercase SHA-256 digest.
- Build IDs are 32 lowercase hex digits. Revisions are full 40-hex Git commits,
  recipe hashes are 64 lowercase hex digits, repositories are HTTPS URLs without
  embedded credentials, and toolchain descriptions contain 1 through 1,024
  UTF-8 bytes.
- Every string excludes control characters. A manifest contains 1 through
  65,536 bytes and must be valid UTF-8 and TOML.

JSON Schema counts Unicode code points rather than UTF-8 bytes. The schema
records byte bounds with `x-fes-maxUtf8Bytes`; readers enforce those bounds on
the decoded TOML strings. Readers also reject duplicate TOML keys, duplicate
interface IDs, non-integer TOML scalars where integers are required, and payload
size or digest mismatches. These are semantic checks rather than TOML
reserialization rules.

The package identity is lowercase SHA-256 over
`FES-CORE-PACKAGE-2\n`, then a little-endian 64-bit manifest length and the
exact manifest bytes, then a little-endian 64-bit payload length and the exact
payload bytes. Manifest bytes are never normalized before hashing.

[`testdata/core-bundle-v2/cases.json`](../testdata/core-bundle-v2/cases.json)
indexes positive and negative fixtures. Paths are relative to that directory.
Valid entries include their package identity; invalid entries include a reason.
The fixture payload is synthetic test-only data and must never be deployed.
Positive fixtures include literal strings, dotted keys, inline tables, full
SemVer, and exact multibyte bounds so consumers test TOML meaning rather than a
single canonical spelling.

## Package schema `mister-packages.v1`

Every package file starts with:

```yaml
schema: mister-packages.v1
kind: platform | board | soc | cpu | register_bank | system | core_source | abi | programming_profiles
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

## abi

An ABI describes a versioned wire contract. It contains no MMIO addresses,
bridge settings, reset scripts, or executable programming instructions.

```yaml
kind: abi
id: fes.simple-game
major: 1
minor: 0
tag: 1
constants:
  - name: FesGpSignature
    value: 0xf5000000
interfaces:
  - id: fes.gamepad
    major: 1
    minor: 0
    capability_bit: 0
```

ABI and interface IDs use `[a-z][a-z0-9_.-]{0,95}`. ABI and interface majors
are 1 through 65,535, minors are 0 through 65,535, capability bits are 0
through 31, and constant values are unsigned 32-bit integers. Constant names,
interface IDs, and capability bits are unique within an ABI. `tag` is optional
and reads as zero when absent: `mister` 1.0 has no fabricated FES GP
live-identity tag, while `fes.simple-game` has tag 1.

`packages/abi/fes_simple_game.yaml` owns the FES GP signature,
request/ACK/error masks, identity words and indices, opcodes, error replies,
button masks, and interface capability assignments. The C++ and Go emitters
preserve its `FesGp` names; the Verilog emitter converts them to guarded
`FES_GP_*` macros.

`packages/abi/fes_simple_computer.yaml` is a second FES GP ABI on the same
mailbox layout: tag 2, required interfaces `fes.keyboard` 1.0 (bit 0),
`fes.video.fixed-720p60` 1.0 (bit 1), and `fes.media.blob` 1.0 (bit 2).
Opcodes 2–6 are execution hold/release, keyboard-row writes, and a 1..16384
byte media blob (begin/data/commit). Error 4 is invalid state. Constant names
use the `FesSimpleComputer` prefix so generated consumers can include both
ABIs in one translation unit. `testdata/fes-simple-computer-v1/exchanges.json`
is the contiguous golden mailbox sequence for that ABI.

## programming_profiles

`packages/abi/fes_application.yaml` adds ABI `fes.application` 1.0, tag 3,
on `fes-gp-v1`. It uses generic emitters with the `FesApplication` prefix.
See [application I/O](application-io.md) for composable interface admission,
wire semantics and lifecycle. This adds one registry pair without changing
the legacy ABI definitions or the manifest schema.

Application audio `fes.audio.pcm-s16-stereo-48k` 1.0 is an additive required-when-
present interface using capability bit 4. It needs no manifest-schema change;
older runtimes reject its required declaration through normal registry admission.

The optional registry extension `fes.media.blob-stream` 1.0 uses capability
bit 3 and opcodes 7..12; new SMS packages declare it required alongside the
legacy required interfaces. See [the authoritative stream contract](media-stream.md).
The base ABI and format-2 manifest schema remain unchanged.

A programming-profile registry names the platform/device and approved
profile/ABI-major pairs. It remains descriptive: runtime owns each profile's
electrical setup, containment, reset ordering, and bridge release.

```yaml
kind: programming_profiles
id: de10_nano
platform: de10_nano
device: 5CSEBA6U23I7
profiles:
  - id: fes-gp-v1
    diagnostic_only: false
    abis:
      - id: fes.simple-game
        major: 1
      - id: fes.simple-computer
        major: 1
  - id: development-contained-v1
    diagnostic_only: true
    abis: []
```

Profile IDs are unique. A normal profile contains one or more unique ABI-major
pairs; a diagnostic-only profile contains no pair. The DE10-Nano registry
contains `mister-v1`/`mister` major 1 and `fes-gp-v1` paired with both
`fes.simple-game` major 1 and `fes.simple-computer` major 1.
`development-contained-v1` is explicitly diagnostic-only and has no ABI
fallback. `fes.simple-computer` is not approved with `mister-v1`.

`emit-cpp` represents every row as `GeneratedProgrammingProfilePair` with
`profile`, `abi`, `major`, and `diagnostic_only`; a diagnostic row uses
`nullptr`, zero, and `true`. `emit-go` emits the equivalent
`ProgrammingProfilePair`. Runtime consumers combine a matching profile/ABI
major row with the ABI file's declared ABI minor and interfaces; they do not
duplicate mailbox constants or infer an unlisted pairing.

`testdata/fes-gp-v1/exchanges.json` is the source-independent FES GP v1
fixture: it records a field write followed by a toggle for all 16 identity
words, the required initial `identity-word-zero` exchange, toggle-back,
invalid opcode/index/argument replies, gameplay reset, and neutral buttons.
Its top-level `initial_request_toggle` starts one contiguous sequence, so each
exchange begins with the preceding final toggle and its GPI ACK equals the new
final toggle. Its `build_id` is synthetic
`00112233445566778899aabbccddeeff`; the identity words encode adjacent ID byte
pairs with the first byte low. It is a wire fixture, not hardware acceptance
evidence.

## Core persistence interfaces

The optional extension to the `fes.simple-game` registry keeps the base ABI and
transport at 1.0. A persistent Pong package declares both `fes.persistence.words`
1.0 (live capability bit 2) and `fes.pong.progress` 1.0 (bit 3) as required.
Exactly one registered layout accompanies the persistence transport. These bits
fit the 16-bit live identity word; existing volatile packages retain bits 0/1.

The generated constants describe data-control opcode 4 (index 0, arguments
freeze=0, begin=1, commit=2, resume=3), read opcode 5, write opcode 6, and info
opcode 7. Info indexes 0–3 report word count, layout tag, major and minor.
Pong tag 1 maps to `fes.pong.progress` 1.0 with exactly two words: paddle-speed
enum 0/1/2 and best rally (u16). The transfer bound is 256 words. Error 4 means
invalid state; existing invalid-opcode/index/argument errors remain 1/2/3.

Freeze requires released gameplay and latches a complete immutable snapshot
before ACK; repeated freeze retains it. Reads require frozen state. Begin clears
restore staging while gameplay reset is held. Writes populate indexed staging;
commit requires all words and valid values, atomically updates persistent data,
then closes staging while reset stays held. Invalid speed is rejected at commit.
Resume requires frozen state and releases it without gameplay reset. Invalid
requests do not modify state. Runtime owns timeout ambiguity and recovery;
these declarations do not execute physical operations.

`testdata/core-persistence-v1/records.json` defines canonical synthetic disk
records for `fes.pong`: ASCII `FESDATA1`, 32-byte SHA-256(core ID), four LE u16
values (layout-ID byte length, major, minor, word count), ASCII layout ID,
LE u16 payload words, then SHA-256 of all preceding bytes. No padding or trailing
bytes is allowed. IDs follow the existing at-most-96-byte grammar and word count
is 1–256, bounding the record at 688 bytes. Pong admits exactly its layout 1.0,
two words, and speed 0–2. The API revision is lowercase SHA-256 of the complete
record; a missing record uses the string `absent`. Its default payload is [1,0].
The storage namespace/path and atomic durability operations belong to consumers.

`testdata/core-persistence-v1/exchanges.json` extends the synthetic identity
sequence to capabilities 15 and covers data admission, staging, commit, freeze,
read and resume, including invalid requests. It leaves the original volatile
`fes-gp-v1` vectors unchanged. Regenerate with
`python3 scripts/core_persistence_fixtures.py`, or use `--check` to compare.
These fixtures establish byte contracts, not hardware acceptance.
