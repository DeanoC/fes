# Home-computer I/O 1.0

`fes.computer` ABI 1.0, live tag 4, is approved on `fes-gp-v1`.
[The YAML](../packages/abi/fes_computer.yaml) owns constants, emitted as
`FesComputer*` and `FES_COMPUTER_*` Verilog macros. It is a separate ABI from
`fes.simple-computer` (tag 2) and `fes.application` (tag 3); both remain
unchanged. The consumers are the FES Apple II, the FES ZX Spectrum and the
FES Commodore 64 and Atari 520ST. These definitions and the synthetic fixtures do not
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
| fes.media.atari-st-floppy | 7 | Media unit 0: one read-only 720 KiB raw ST disk image |
| fes.mouse.relative | 8 | Relative signed movement and two mouse buttons |
| fes.media.atari-st-floppy-write | 9 | Writable extension to the ST disk interface, with frozen image capture |
| fes.media.atari-st-floppy-geometry | 10 | Required opt-in for bounded alternate raw ST disk geometries |

Admission requires video. Every other interface is independently composable.
The writable extension requires the base ST floppy interface; it changes its
write-protection behavior while preserving its size and upload contract.
Each recognized operational interface a core implements must be declared
required by the manifest and advertised in live identity; live capabilities
must equal the declared registered set. Unknown required interfaces and
unsupported versions fail admission; unknown optional interfaces are ignored
and grant nothing. A shell declares at most one unit-0 media interface.
Expansion interfaces such as `fes.expansion.apple2-bus`,
`fes.expansion.spectrum-bus`, `fes.expansion.c64-bus` and
`fes.expansion.atari-st-bus` are manifest-only
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
| 11 | MouseRelative | button state 0..3 | signed 8-bit dx in low byte, dy in high byte |
| 12 | MediaSnapshotInfo | unit*8+field | 0 |
| 13 | MediaSnapshotControl | unit | 0 Freeze, 1 Resume, 2 Saved |
| 14 | MediaSnapshotChunk | unit*4+word | offset low, offset high, even byte length |
| 15 | MediaSnapshotData | word ordinal 0..255 | 0; response is a byte pair |

## Execution

Execution opcode 2 uses index 0 (else invalid index); an argument above 1 is
invalid. Argument 0 holds execution reset and neutralizes all keyboard rows and
both controller ports and mouse buttons. Argument 1 releases execution. Release is always valid:
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

## Relative mouse

`fes.mouse.relative` 1.0 uses opcode 11. Index bits 0 and 1 are complete
active-high left and right button state; all other index bits must be zero.
The argument contains signed two's-complement 8-bit dx and dy. It delivers
one atomic movement/button packet. A zero movement packet can change buttons.
The endpoint checks index, then held execution (invalid state), before waiting
for its machine consumer. ACK means that consumer accepted the packet once;
held GPO fields cannot deliver it again. An absent interface rejects the opcode.

Hosts split larger signed deltas into ordered bounded packets, preserving the
exact sum. Never replay a movement after an ambiguous ACK, disconnection or
reconnection. Duplicate remote sequence numbers do not repeat movement. Button
state is merged across input sources and may be restored with zero deltas;
removing a source releases only its buttons. Mouse events have no controller
port or player assignment. The existing 48-byte remote frame appends mouse as
a device and relative as an event kind without changing earlier values.

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
one unit-0 media interface, including the Atari ST floppy below.

### fes.media.c64-disk 1.0

Unit 0, the same unit number as the Apple II floppy and the Spectrum tape. A
shell declares one unit-0 medium. The unit accepts exactly 174,848 bytes
(Info minimum = maximum = 174,848): a 35-track Commodore D64 image, 683
sectors of 256 bytes with the standard track lengths (21, 19, 18, then 17
sectors). Byte offset of track 1 sector 0 is 0. Version 1.0 is read only:
the built-in 1541-compatible device serves LOAD and does not return writes
to the host. G64, D71 and D81 images are different formats. Capability bit 6
(`0x40`) is this disk. Bit 5 (`0x20`) is the Spectrum tape.

### fes.media.atari-st-floppy 1.0

Unit 0. Without the geometry extension, the unit accepts exactly 737,280 bytes (Info minimum = maximum =
737,280): 80 cylinders, two sides, nine sectors per track and 512 bytes per
sector, with the plain `.st` sector layout. Byte offset is
`((cylinder * 2 + side) * 9 + sector - 1) * 512`; cylinders are 0..79,
sides 0..1, and sectors 1..9. Drive A is read only and reports write protect;
version 1.0 does not return machine writes to the host. Drive B is absent.
MSA compressed files, STX flux images and hard disks are different formats.
Alternate raw geometry requires the separate extension documented below.
Capability bit 7 (`0x80`) identifies this interface. It shares unit 0 with
Apple II, Spectrum and C64 media, so a shell declares exactly one such medium.

Backend wait states do not alter mailbox framing: a successful MediaData
acknowledgement follows its complete storage write, including a byte pair
split across two memory words at an odd chunk boundary. Rejected requests
acknowledge normally without writing. CPU Hold does not cancel media traffic.

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

## Writable ST image capture

`fes.media.atari-st-floppy-write` 1.0 occupies unit 0 and extends the base
floppy interface. Without the write extension the drive remains read-only.
The machine owns normal sector writes; this interface captures the whole raw
image for durable storage. It does not describe flux, formatting, deleted
sectors or a FAT transaction. Capture length is the committed image size:
legacy endpoints remain exactly 80-track, two-side, nine-sector, 512-byte
`.st` (737280 bytes, layout 1.0). With `fes.media.atari-st-floppy-geometry`
1.0, a nonlegacy admitted length uses layout 1.1 and must match its bound
immutable-base size; 737280-byte records stay layout 1.0.

SnapshotInfo (12) requires unit 0 and argument 0. Fields 0..7 are flags,
layout tag (1), layout major (1), layout minor, current byte size low/high,
maximum chunk bytes (512), and a wrapping 16-bit change epoch. Layout minor
is 0 for a 737280-byte image and 1 for any other admitted geometry length.
Flags bits 0..3 mean ready, dirty, frozen, writer busy; other bits are zero.
The epoch advances when a complete accepted sector commits. Dirty clears on
replacement, eject or Saved. Reading Info cannot establish a consistent image
by itself.

SnapshotControl (13) requires index 0 and argument 0..2. Freeze requires a
ready image, fences new writers, and waits until the machine has observed the
fence and an already accepted writer has drained. ACK leaves the image frozen
without holding or resetting the CPU. Capture restarts at byte offset zero.
Resume is idempotent, abandons capture and releases the writer fence. Saved
requires frozen storage with no busy writer or simultaneous change; it clears
dirty and authorizes replacement/eject while keeping the fence. The runtime
sends Saved only after publishing and syncing the complete disk record.

SnapshotChunk (14) requires a frozen ready image and no unfinished chunk.
The unit is index>>2; words 0, 1, 2 stage offset low, offset high and length
in that order. Unit 0 only; word 3 or out-of-order words reject. The offset
must equal the bytes already captured and the length must be even, 2..512,
and fit the remaining image. SnapshotData (15) requires argument 0 and the
next ordinal. ACK waits for both physical byte reads and returns the first
byte low, second high. A low-request rearm edge separates reads. Ordinals
restart at zero for each chunk; capture continues through the committed
image length (737280 bytes for layout 1.0, or the exact bound-base length
for layout 1.1).

While frozen, ordinary Begin/Eject reject unless Saved authorized destruction
and the writer is idle. A failed save retains the frozen captured generation
and its runtime owner. Resume may explicitly recover that same generation;
an unconfirmed Resume does not authorize stopping, replacing or reprogramming.
Raw development inserts remain volatile. Library inserts explicitly bind core,
game, unit and immutable base-media identity to the durable record defined in
[media data](media-data.md); a display name or package path grants no binding.

### fes.media.atari-st-floppy-geometry 1.0

This required operational extension adds capability bit 10 (`0x400`) and
requires the base floppy 1.0 interface. Legacy endpoints retain exact
737280-byte arbitrary-byte admission. Unit 0 advertises exact minimum 368640
and maximum 839680 bytes, but only these twelve lengths are admitted:

| Tracks | Heads | Sectors/track | Bytes |
| --- | --- | --- | --- |
| 80 | 1 | 9 | 368640 |
| 80 | 1 | 10 | 409600 |
| 80 | 2 | 9 | 737280 |
| 80 | 2 | 10 | 819200 |
| 81 | 1 | 9 | 373248 |
| 81 | 1 | 10 | 414720 |
| 81 | 2 | 9 | 746496 |
| 81 | 2 | 10 | 829440 |
| 82 | 1 | 9 | 377856 |
| 82 | 1 | 10 | 419840 |
| 82 | 2 | 9 | 755712 |
| 82 | 2 | 10 | 839680 |

Size uniquely determines physical geometry; CHS sectors are contiguous in
cylinder/head/sector order. Compressed MSA bytes are never sent to the core.
Nonlegacy immutable inputs require little-endian BPB sector bytes (offset 11)
equal 512, total sectors (19) equal size/512, and sectors per track (24) and
heads (26) equal the inferred geometry. Guest-modified saved BPBs are retained;
restored bytes keep the exact immutable-base length as physical geometry.
Snapshot tag and major remain 1: 737280 bytes uses minor 0 and other admitted
sizes use minor 1. Existing chunks, freeze, CRC, and epochs are unchanged.
