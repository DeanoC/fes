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

## Core bundle manifest format 3 and ROM map format 1

[`schema/core-bundle-v3.json`](../schema/core-bundle-v3.json) extends the same
closed fields and semantic restrictions as format 2 with `format = 3` and a
required `[rom]` table. Format 2 rejects that table and retains its original
identity domain. Format 3 requires exactly these ROM keys:

| Key | Meaning and constraints |
| --- | --- |
| `id` | Normal identifier `[a-z][a-z0-9_.-]{0,95}` |
| `role` | `firmware` or `cartridge` |
| `source_size` | Integer, 1024 through 262144 inclusive, divisible by 1024 |
| `file` | Exactly `rom-map.json` |
| `size` | Exact map byte length, integer 1 through 33554432 |
| `sha256` | Exact map digest, 64 lowercase hexadecimal characters |

A format-3 directory contains exactly `manifest.toml`, `core.rbf`, and
`rom-map.json`, all regular non-symlink files. Its uncompressed restricted ustar
archive is at most 65 MiB and contains exactly those three regular members in
that order. Headers use mode 0644, UID/GID/mtime zero, empty owner/group/link
names, canonical octal sizes, and no extensions or prefixes. Header bytes must
match canonical POSIX ustar encoding. Member padding is zero, and the archive
ends with exactly two zero blocks. Format 2 retains its two members and 33 MiB
archive bound.

The identity is lowercase SHA-256 of `FES-CORE-PACKAGE-3\n`, followed by
LE64(manifest length), exact manifest bytes, LE64(RBF length), exact RBF bytes,
LE64(map length), and exact map bytes. No member is normalized before hashing.
The ROM map is trusted producer metadata sealed into this identity; uploaded
ROM content must never supply or replace it.

[`schema/rom-map-v1.json`](../schema/rom-map-v1.json) defines the map structure.
Its exact root keys are `format` (integer 1), `device` (`5CSEBA6U23I7`),
`encoding` (`m10k-1024x10-v1`), `base_sha256` (the manifest payload digest),
`source_size` (the manifest ROM source size), and `blocks`. UTF-8 JSON must
have unique keys, no trailing document, and exact integer scalars: Boolean,
floating-point and nonfinite numbers are rejected. Unknown or missing fields
and nulls are rejected at every structured level.

There are exactly `source_size / 1024` blocks, from 1 through 256. A block has
exactly `bel`, `source_offset`, and `word_bits`. BEL labels are unique, nonempty
strings of at most 64 UTF-8 bytes without control characters. Source offsets
are unique, nonnegative multiples of 1024; each covers 1024 bytes within the
source. Together the blocks cover the complete source exactly once.
`word_bits` contains exactly 256 arrays, each with exactly 40 integer physical
destinations in Mistral stored-bit order. Each destination lies in
`[32 * 7605, 7605 * 7024)` and is globally unique across the map. These are
linear CRAM bit addresses, not compressed RBF offsets. Python and Go host
inspectors validate map length, digest, structure and bindings without decoding
frames. The physical C++ runtime inspector validates only the sealed member
length/digest, manifest metadata and package identity; it does not parse map
semantics and refuses activation of format-3 packages. The linker separately
validates frame encoding and blank ROM destinations before patching.

[`testdata/core-bundle-v3/cases.json`](../testdata/core-bundle-v3/cases.json)
indexes synthetic conformance data: `manifest`, `payload`, and `rom_map` are
relative file paths; positives carry `package_id`, negatives carry `reason`
and `validation_layer`. `package` failures concern member/manifest/digest
validation; `rom_map` failures concern JSON map semantics after package hashes
pass. A target reader that only validates sealed bytes must distinguish those
layers and must not claim JSON semantic validation. Full host readers reject
both. The synthetic RBF is not hardware-valid. Its map has one synthetic block
(two for cross-block overlap tests); the payload digest remains bound correctly.
Generate with `python3 scripts/core_bundle_v3_fixtures.py`; `--check` verifies
exact checked-in bytes. The format-2 corpus and its identities are unchanged.

## Package schema `mister-packages.v1`

Every package file starts with:

```yaml
schema: mister-packages.v1
kind: platform | board | soc | cpu | register_bank | abi | programming_profiles
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

Media begin/data/commit do not require execution reset held. Holding reset
through the transfer is launch-time runtime policy for the primary `.p` bind.
A mid-session replace on an active generation leaves execution released so
RAM, expansion composition and the spliced machine ROM stay intact. Media
begin with `FesSimpleComputerMediaEjectIndex` and argument 0 clears
committed readiness (eject) so the next empty `LOAD ""` reports `0/0`.
Begin on the control index with argument 0 remains invalid argument (golden
wire fixture). While the ZX81 tape-loader is copying (`media_busy`), begin
and eject reject with error 4 rather than aborting the copy. See
FES [`docs/zx81-tape-media.md`](../../../docs/zx81-tape-media.md).

## programming_profiles

`packages/abi/fes_application.yaml` adds ABI `fes.application` 1.0, tag 3,
on `fes-gp-v1`. It uses generic emitters with the `FesApplication` prefix.
See [application I/O](application-io.md) for composable interface admission,
wire semantics and lifecycle. This adds one registry pair without changing
the legacy ABI definitions or the manifest schema.

Application audio `fes.audio.pcm-s16-stereo-48k` 1.0 is an additive required-when-
present interface using capability bit 4. It needs no manifest-schema change;
older runtimes reject its required declaration through normal registry admission.

Application firmware `fes.firmware.blob` 1.0 is capability bit 7 with opcodes
15–17 and exact length `FesApplicationFirmwareBytes` (8192). It may be required
or optional. See [application I/O](application-io.md). The in-tree runtime
consumer revision is recorded in `testdata/oracles/fes-application-firmware.json`.

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
contains `fes-gp-v1` paired with
`fes.simple-game` major 1 and `fes.simple-computer` major 1.
`development-contained-v1` is explicitly diagnostic-only and has no ABI
fallback. MiSTer ABI packages are not activatable.

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
