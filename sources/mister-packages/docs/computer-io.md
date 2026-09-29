# Home-computer I/O 1.0

`fes.computer` ABI 1.0, live tag 4, is approved on `fes-gp-v1`.
[The YAML](../packages/abi/fes_computer.yaml) owns constants, emitted as
`FesComputer*` and `FES_COMPUTER_*` Verilog macros. It is a separate ABI from
`fes.simple-computer` (tag 2) and `fes.application` (tag 3); both remain
unchanged. The consumers are the FES Apple II, the FES ZX Spectrum and the
FES Commodore 64. These definitions and the synthetic fixtures do not
establish consumer or hardware acceptance.

A home computer differs from the existing ABIs in three ways: it has a full
keyboard, it keeps running while media is changed, and it may have several
media drives. This ABI therefore carries USB HID key state instead of a
machine-shaped matrix, has no media gate on execution release, and addresses
removable media by unit.

## Admission

| Interface 1.0 | Capability bit | Meaning |
| --- | --- | --- |
| fes.video.fixed-720p60 | 0 | Existing fixed 720p60 output contract |
| fes.keyboard.hid | 1 | USB HID keyboard key state (Keyboard/Keypad page and modifiers) |
| fes.gamepad.ports | 2 | Two eight-button controller ports, as in `fes.application` |
| fes.audio.pcm-s16-stereo-48k | 3 | Fixed 48 kHz stereo PCM over I2S, as in `fes.application` |
| fes.media.apple2-floppy | 4 | Media unit 0: one Apple II 5.25-inch DOS 3.3 order disk image |
| fes.media.spectrum-tape | 5 | Media unit 0: one ZX Spectrum `.tap` image, 1..65536 bytes |
| fes.media.c64-disk | 6 | Media unit 0: one Commodore 1541 D64 disk image |

Admission requires video. Every other interface is independently composable.
Each recognized operational interface a core implements must be declared
required by the manifest and advertised in live identity; live capabilities
must equal the declared registered set. Unknown required interfaces and
unsupported versions fail admission; unknown optional interfaces are ignored
and grant nothing. A shell declares at most one unit-0 media interface.
Expansion interfaces such as `fes.expansion.apple2-bus`,
`fes.expansion.spectrum-bus` and `fes.expansion.c64-bus` are manifest-only
optional declarations, not capability bits. Launch-time firmware is supplied
by package ROM linking (format 3), not by this mailbox.

## Mailbox framing and discovery

Framing is the existing FES GP transport. GPO bit 31 is a request toggle;
bits 30:24 opcode, 23:16 index, 15:0 argument. Write the fields with the old
toggle, then with the toggle inverted, and wait for GPI ACK bit 23 to equal the
new toggle. GPI bits 31:24 are signature 0xf5, bit 22 is error, bits 15:0 the
response. Successful mutating requests return zero.

Identity is opcode 1 with index 0..15 and argument 0. The words are: 0/1 magic
0x4546/0x3153; 2/3 transport 1/0; 4 ABI tag 4; 5/6 ABI 1/0; 7 capabilities;
8..15 build ID, two bytes per word, first byte low. Compare identity with the
exact manifest before release or any input or media delivery.

Errors are invalid opcode=1, invalid index=2, invalid argument=3, invalid
state=4. An opcode whose capability is absent returns invalid opcode. Every
rejected request acknowledges the toggle and changes no state. Checks are made
in the order written below; the first failing check determines the error.

| Opcode | Name | Index | Argument |
| --- | --- | --- | --- |
| 1 | Identity | word 0..15 | 0 |
| 2 | Execution | 0 | 0 hold reset, 1 release |
| 3 | KeyboardHid | row 0..8 | 16-bit row state |
| 4 | ControllerButtons | port 0..1 | 8-bit button mask |
| 5 | MediaInfo | unit*8+field | 0 |
| 6 | MediaBegin | unit*4+word | header word |
| 7 | MediaChunk | unit*4+word | header word |
| 8 | MediaData | word ordinal 0..255 | byte pair, low byte first |
| 9 | MediaCommit | unit | 0 |
| 10 | MediaEject | unit | 0 |

## Execution

Execution opcode 2 uses index 0 (else invalid index); an argument above 1 is
invalid. Argument 0 holds execution reset and neutralizes all keyboard rows and
both controller ports. Argument 1 releases execution. Release is always valid:
a home computer starts with its drives empty, and media is inserted and
removed while it runs. Hold does not touch media units, transfers or staged
headers. Programming starts held with neutral input and every implemented unit
empty. Stop/reprogram establishes a fresh identity and clears all state.

## Keyboard

`fes.keyboard.hid` 1.0 carries the state of a USB HID boot-class keyboard as
nine 16-bit rows. Row r in 0..7 holds Keyboard/Keypad page (0x07) usages
16r..16r+15 in bits 0..15; a set bit means the key is down. Usages 0..3 are
the HID error codes and bits 0..3 of row 0 must be zero (invalid argument).
Row 8 bits 0..7 hold the modifier usages 0xe0..0xe7 (left control, left shift,
left alt, left GUI, right control, right shift, right alt, right GUI); bits
8..15 must be zero. A row above 8 is an invalid index.

Each write replaces one row and is valid while held or released. The host
sends every row whose state changed; a multi-row change is several ordered
transactions, not an atomic update. A core must therefore treat a burst of row
writes as one change, for example by interpreting key state only after the
rows have been unchanged for about a millisecond, so a key and the modifiers
sent in the same update are seen together. Hosts must not send a press and
its release closer together than that. Initial reset and Hold release every key.
The core owns the mapping from HID usages to its native keyboard, including
shifted symbols, repeat and machine-specific reset chords; the host forwards
physical key state and never translates characters. Usages the core does not
map are ignored.

## Controller ports

`fes.gamepad.ports` 1.0 has the semantics of the `fes.application` interface
of the same name ([application I/O](application-io.md)), carried by opcode 4.
The index is port 0 or 1 (else invalid index) and the argument the complete
active-high Up/Down/Left/Right/A/B/Select/Start mask (above 0xff is invalid).
Each write replaces one port and is valid while held or released. Initial
reset and Hold clear both ports. There is no keypad on this ABI.

## Media units

A media unit is one removable-media drive. A core implements a fixed set of
units; each media interface names the unit it occupies and the exact format
the unit accepts. A unit is **empty**, **loading** (a transfer or header is
staged) or **ready** (a complete image is committed). Units that the endpoint
does not implement are **absent**. The core presents a unit's image to the
machine only while the unit is ready; while empty or loading the machine sees
an empty drive. Media opcodes return invalid opcode unless at least one media
interface is advertised. All media opcodes are valid while held or released.

**MediaInfo** (5): unit = index>>3 and field = index&7. A unit of 8 or more or
a field above 5 is an invalid index; a nonzero argument is invalid. Fields are
0 minimum bytes low, 1 minimum high, 2 maximum low, 3 maximum high, 4 maximum
chunk bytes (512), 5 state (0 absent, 1 empty, 2 loading, 3 ready). Every field
of an absent unit reads 0. Info never changes state.

**MediaBegin** (6): unit = index>>2 and word = index&3; words are 0 total low,
1 total high, 2 expected CRC32 low, 3 CRC32 high. An absent unit is an invalid
index. Any active transfer (on any unit) is invalid state. With no header
staged the word must be 0; with a header staged the unit must match and the
word must be the next word; otherwise invalid index. Word 0 stages the header
for its unit and makes that unit loading at once, so the machine sees the drive
empty from the first accepted word. Word 3 validates the assembled total
against the unit's advertised minimum and maximum (invalid argument if out of
range; the word is not consumed and staging does not advance) and then arms
the transfer with zero bytes received. A staged header cannot be restarted by
sending word 0 again: supply the correct next word or eject the unit.

**MediaChunk** (7): unit = index>>2 and word = index&3; words are 0 offset low,
1 offset high, 2 byte length. No armed transfer is invalid state. A unit other
than the transfer's, a word other than the next chunk word, or a word above 2
is an invalid index. An open chunk (not fully received) is invalid state.
Word 2 validates offset equal to bytes received, length 1..512 and length no
larger than total minus received (invalid argument, not consumed) and opens
the chunk at word ordinal 0.

**MediaData** (8): no open chunk is invalid state. The index must equal the
next word ordinal (invalid index). The low byte is stored first. The last word
of an odd-length chunk carries one byte and its high byte must be zero
(invalid argument); the padding is not stored or included in the CRC. A valid
word updates received count and CRC exactly once. The chunk closes after
ceil(length/2) words.

**MediaCommit** (9): index = unit. An absent unit is an invalid index; a
nonzero argument is invalid. No armed transfer, or a header still staged, is
invalid state; a unit other than the transfer's is an invalid index. An open
chunk or received not equal to total is invalid state; a CRC mismatch is an
invalid argument and leaves the unit loading. Success makes the unit ready and
closes the transfer.

**MediaEject** (10): index = unit. An absent unit is an invalid index; a
nonzero argument is invalid. Eject cancels a staged header or transfer that
belongs to the unit (a transfer on another unit is untouched) and leaves the
unit empty. It is idempotent and is the only way to abandon a transfer.

CRC32 is CRC-32/IEEE exactly as in [stream media](media-stream.md): reflected,
initial `0xffffffff`, polynomial `0xedb88320`, final XOR `0xffffffff`, over
payload bytes only, carried across chunks. Integrity CRC does not replace the
host's immutable SHA-256 media identity. Staging memory is not rolled back by
eject or a failed transfer; a unit simply is not ready until a later commit.

One owned GP/session serializes each transaction. Do not replay an ambiguous
mutation: eject the unit and start again. Transport length and offsets are
32-bit; the ABI admits at most `FesComputerMediaMaxBytes` (32 MiB) per unit,
and a unit's own limits come only from its interface and live Info.

### fes.media.apple2-floppy 1.0

Unit 0. The unit accepts exactly 143,360 bytes (Info minimum = maximum =
143,360): 35 tracks of 16 sectors of 256 bytes in DOS 3.3 logical order, the
`.dsk`/`.do` layout. Byte offset = (track × 16 + logical sector) × 256. The
core synthesises the 16-sector nibble stream, including the physical to DOS
logical interleave, volume 254 and 6-and-2 data encoding. Version 1.0 is read
only: the drive reports write protect and writes are not returned to the host.
ProDOS order (`.po`) and nibble (`.nib`) images are different formats; a host
may convert `.po` to DOS order before transfer, but must not send it as is.

### fes.media.spectrum-tape 1.0

Unit 0. The unit accepts 1..65,536 bytes: a `.tap` image, a sequence of
blocks each stored as a little-endian uint16 length followed by that many
payload bytes. The core plays complete blocks into the EAR bit while the
machine runs (header pilot 8063 edges, data pilot 3223, sync 667/735, bit
pulses 855 or 1710 T-states, then a one-second pause). A trailing partial
block is not played. Version 1.0 does not return MIC writes to the host.
A shell declares this unit, the Apple II floppy, or the C64 disk, and only
one of those unit-0 media interfaces.

### fes.media.c64-disk 1.0

Unit 0, the same unit number as the Apple II floppy and the Spectrum tape. A
shell declares one unit-0 medium. The unit accepts exactly 174,848 bytes
(Info minimum = maximum = 174,848): a 35-track Commodore D64 image, 683
sectors of 256 bytes with the standard track lengths (21, 19, 18, then 17
sectors). Byte offset of track 1 sector 0 is 0. Version 1.0 is read only:
the built-in 1541-compatible device serves LOAD and does not return writes
to the host. G64, D71 and D81 images are different formats. Capability bit 6
(`0x40`) is this disk. Bit 5 (`0x20`) is the Spectrum tape.

## Audio

`fes.audio.pcm-s16-stereo-48k` 1.0 has exactly the board transport, silence
and ownership rules of the [application audio contract](application-io.md#audio):
no opcode, 48 kHz signed 16-bit stereo I2S, silence while held, runtime-owned
ADV7513 packet enable.

## Evidence and ownership

[Golden exchanges](../testdata/fes-computer-v1/exchanges.json) are generated by
`scripts/fes_computer_fixtures.py`, whose `Endpoint` class is the reference
model of this document, and replayed against an independent Go model in
`internal/pack/fes_computer_test.go`. They describe two synthetic sessions:
keyboard and controllers without media, and live media on a synthetic unit 0
that advertises 1..1030 bytes so multi-chunk, odd-length, CRC and eject cases
fit in a small file. Consumers own their implementations and must replay these
requests against their own code; the endpoint capacity is a synthetic fixture
parameter, not the Apple II floppy size. FES selects reviewed component
revisions before integration or exact-artifact hardware acceptance.
