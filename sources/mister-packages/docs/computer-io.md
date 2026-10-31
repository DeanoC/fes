# Home-computer I/O 1.1

`fes.computer` ABI 1.1, live tag 4, is approved on `fes-gp-v1`.
[The YAML](../packages/abi/fes_computer.yaml) owns constants, emitted as
`FesComputer*` and `FES_COMPUTER_*` Verilog macros. It is a separate ABI from
`fes.simple-computer` (tag 2) and `fes.application` (tag 3); both remain
unchanged. The consumers are the FES Apple II, the FES ZX Spectrum and the
FES Commodore 64 and Atari 520ST, which declare ABI 1.0, and the FES pc386, which
declares 1.1. These definitions and the synthetic fixtures do not establish
consumer or hardware acceptance.

Minor 1 adds a second capability word, identity word 16, because bits 0..15
of word 7 are all assigned. It also adds the ATA hard disk, persistence words,
the wall clock and the pc386 machine layout, all on bits 16..19. A 1.0
endpoint and manifest keep their exact bytes and behavior.

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
| fes.media.spectrum-tape 2.0 | 5 | Media unit 0: one ZX Spectrum tape as a PZX 1.0 stream, 10..16777216 bytes |
| fes.media.c64-disk | 6 | Media unit 0: one Commodore 1541 D64 disk image |
| fes.media.atari-st-floppy | 7 | Media unit 0: one read-only 720 KiB raw ST disk image |
| fes.mouse.relative | 8 | Relative signed movement and two mouse buttons |
| fes.media.atari-st-floppy-write | 9 | Writable extension to the ST disk interface, with frozen image capture |
| fes.media.atari-st-floppy-geometry | 10 | Required opt-in for bounded alternate raw ST disk geometries |
| fes.media.zx81-tape | 11 | Media unit 0: one ZX81 `.p` image, 1..16384 bytes |
| fes.memory.hps-ddr | 12 | Reserved DDR window for launcher frames. Advertised only when the boot DDR layout and menu adapters exist |
| fes.video.session-display | 13 | In-session HDMI plane. Reuses menu opcodes 18–21. Advertised with the DDR window, and only on a computer that implements the plane |
| fes.media.zx81-tape-write | 14 | Writable extension to the ZX81 tape. Captures the BASIC SAVE image; it does not require a loaded cassette |
| fes.media.spectrum-disk | 15 | Media unit 1, beside the Spectrum tape: one raw track disk image, 8192..2834432 bytes |
| fes.media.ata-disk | 16 | Unit 0: one hard-disk image of 1..128 MiB that the runtime writes into the DDR window ([ATA disk](#ata-hard-disk)) |
| fes.persistence.words | 17 | Durable words of one registered layout, restored while held and captured at Stop ([persistence](#persistence-words)) |
| fes.rtc.wall-clock | 18 | The kit's local time, seeded while held ([wall clock](#wall-clock)) |
| fes.pc386.machine | 19 | The 72-word pc386 settings and CMOS layout of the persistence words |

A session display is also declared with the manifest-only
`fes.fabric.video.display-plane` 1.0, which has no capability bit; see
[session display](session-display.md).

Admission requires video. Every other interface is independently composable.
The writable extension requires the base ST floppy interface; it changes its
write-protection behavior while preserving its size and upload contract.
Each recognized operational interface a core implements must be declared
required by the manifest and advertised in live identity; live capabilities
must equal the declared registered set. Unknown required interfaces and
unsupported versions fail admission; unknown optional interfaces are ignored
and grant nothing. A shell declares at most one unit-0 media interface, and
the Spectrum disk on unit 1 only beside the Spectrum tape and HPS DDR.
Bits 16..31 exist only in ABI 1.1: a manifest that declares one of their
interfaces declares ABI 1.1, and a 1.0 manifest declares none. The ATA disk is
a unit-0 medium and requires `fes.memory.hps-ddr`. `fes.persistence.words`
requires exactly one registered layout, and the only one in this registry is
`fes.pc386.machine`; each requires the other.
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
0x4546/0x3153; 2/3 transport 1/0; 4 ABI tag 4; 5/6 ABI major 1 and the
endpoint's minor; 7 capabilities 0..15; 8..15 build ID, two bytes per word,
first byte low. An endpoint that reports minor 1 also answers index 16 with
capabilities 16..31; one that reports minor 0 rejects index 16 as an invalid
index. The host reads word 16 only when the manifest declares minor 1, and a
host that supports 1.1 still admits 1.0 manifests. Compare identity with the
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
| 16 | DiskInfo | unit*8+field | 0 |
| 17 | DiskControl | unit | 0 Prepare, 1 Commit, 2 Eject, 3 Freeze, 4 Resume, 5 Saved |
| 18..21 | Menu display | as in [session display](session-display.md) | |
| 22 | DiskCommit | word 0..7 | geometry and CRC word |
| 23 | DiskDirty | word 0..127 | 0; response is 16 chunk bits |
| 24 | DataControl | 0 | 0 Freeze, 1 Begin, 2 Commit, 3 Resume |
| 25 | DataRead | word | 0; response is the snapshot word |
| 26 | DataWrite | word | staged word |
| 27 | DataInfo | 0..3 | 0 |
| 28 | WallClock | word 0..3 | clock word |

## Execution

Execution opcode 2 uses index 0 (else invalid index); an argument above 1 is
invalid. Argument 0 holds execution reset and neutralizes all keyboard rows and
both controller ports and mouse buttons. Argument 1 releases execution. Release is always valid:
a home computer starts with its drives empty, and media is inserted and
removed while it runs. Hold does not touch media units, transfers or staged
headers. Programming starts held with neutral input and every implemented unit
empty. Stop/reprogram establishes a fresh identity and clears all state.

Hold also abandons a persistence snapshot, and every accepted Execution
request closes persistence restore staging and discards a partly staged wall
clock. Neither Hold nor release touches the ATA disk.

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
The argument contains signed two's-complement 8-bit dx and dy. Positive dx
moves right and positive dy moves down the screen, as Linux `REL_Y` and the
browser's `movementY` count; a core whose machine counts up (a PS/2 mouse)
negates dy. It delivers one atomic movement/button packet. A zero movement packet can change buttons.
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

### fes.media.spectrum-tape 2.0

Unit 0. The unit accepts 10..16,777,216 bytes: a
[PZX 1.0](http://zxds.raxoft.cz/docs/pzx.txt) stream, starting with its
PZXT block. The core keeps the stream in its HPS DDR window and plays it into
the EAR bit while the machine runs. PULS, DATA, PAUS and STOP blocks play;
every other block is skipped. Durations are T states of the machine, as PZX
allows for the 128K. A pulse holds its level for its duration and the next
pulse starts toggled. PULS starts low; DATA and PAUS start at the level in
bit 31 of their first word. Zero-length pulses only move the level, so EAR
never shows an intermediate level. STOP stops the tape (flag 1 only on a 48K)
and leaves EAR at the level the next pulse would start at; the end of the
stream does the same and rewinds to the first block. Ejecting drops EAR low.

The tape starts and stops itself, as Fuse 1.6.0's loader detection does.
After each read of a ULA port the core compares the T states since the
previous read and the change in register B. While the tape is stopped, 10
successive reads at most 500 T apart, each with B one up or one down, start
it. While it plays, 2 successive reads more than 1000 T apart, or with B
changed by more than one, stop it. Version 2.0 does not return MIC writes to
the host.

Hosts convert `.tzx`, `.tap`, `.csw` and `.pzx` images to this stream so that
it plays as libspectrum 1.5 plays the original: the same levels for the same T
states, and the same stops. FogCast's `internal/spectrumtapes` package does
this and is checked against libspectrum on fixtures of every block type.
Version 1.0 (a `.tap` image of at most 64 KiB held in M10K) is withdrawn; a
1.0 package is refused at compatibility.

A Spectrum package may also declare the optional interfaces
`fes.media.spectrum-snapshot` 1.0, `fes.media.spectrum-snapshot-128` 1.0 and
`fes.media.spectrum-snapshot-plus3` 1.0. They add no unit and no capability
bit: they say that this unit also runs restore streams for 48K, 128K and
+2A/+3 snapshots. A restore stream starts with `ZXRS`, version 1, its class
(0, 1 or 2) and two zero bytes; the core resets the machine and serves every
CPU memory read from the stream's following bytes until the last one, then
fetches from real memory. FogCast's `internal/spectrumsnapshots` writes it
from `.z80`, `.sna` and `.szx` snapshots; the FES Spectrum README describes the code.

A shell declares this unit, the Apple II floppy, the C64 disk, the Atari ST
floppy, or the ZX81 tape, and only one unit-0 media interface.

### fes.media.spectrum-disk 1.0

Unit 1, beside a `fes.media.spectrum-tape` cassette on unit 0; the shell
must also declare `fes.memory.hps-ddr`, and keeps the image there. The unit
accepts 8,192..2,834,432 bytes: a raw track image of 8 or 16 KiB slots, read
by the core's floppy controller as a drive reads the disk under its head.
Slot 0 is the header: `ZXDK`, version 1, cylinders (1..86), sides (1 or 2), a
flags byte (bit 0 write protect, bit 1 boot) and the slot size as a power of
two (13 or 14), zero to the end of the slot. Cylinder `c`, side `s` is slot
`1 + 2c + s`. A track slot holds the track's length in bytes (little endian;
1..6912 in an 8 KiB slot, 1..15104 in a 16 KiB one; 0 is unformatted) in
bytes 0-1, a flags byte 2 (bit 0: an FM track), a mark bitmap from byte 16
(bit `i & 7` of byte `16 + i / 8` flags track byte `i`: in MFM the A1 sync
bytes of every address mark, which lack a clock; in FM every mark byte, which
has its special clock) and the track's bytes from an eighth of the slot,
starting at the index hole. Just below the bytes, at an eighth of the slot
less 144, are up to eight weak ranges, each a little-endian start and length
of track bytes that read back differently each time (length 0 ends the list).
An image may end after its last formatted slot; a slot past the end is
unformatted.

A host sets the boot flag on the image it inserts for a launch: a core that
finds it when the disk becomes ready resets the machine into its disk system
and boots the disk. A disk inserted while the machine runs leaves it clear and
is only a new medium. FogCast's `internal/spectrumdisks` writes the image
from `.trd` and `.scl` TR-DOS disks and standard and extended `.dsk` disks,
building their tracks as Fuse 1.6.0's `disk.c` does.

The interface says nothing about the controller. A package declares its
controller with an optional interface that adds no unit and no capability
bit: `fes.media.spectrum-disk-trdos` 1.0 for a Beta 128 running TR-DOS,
which hosts feed `.trd` and `.scl` images, or `fes.media.spectrum-disk-plus3`
1.0 for the +3's uPD765A, which hosts feed `.dsk` images. A package declares
exactly one.

Begin, Chunk, Commit and Eject name the unit as for unit 0, and Info index
`8..13` reports it. One transfer runs at a time: Begin is refused while a
transfer to either unit is open, Chunk and Commit must name the unit of the
open transfer, and Begin words may not change unit. Ejecting one unit leaves
the other unit's image and open transfer alone.

### fes.media.zx81-tape 1.0

Unit 0. The unit accepts 1..16,384 bytes: a ZX81 `.p` memory image. The core
copies a ready image into RAM when BASIC reaches `LOAD` at `$0347`. An empty
unit makes that load return `0/0` instead of waiting on an analog cassette.
Version 1.0 does not play audio tape. Saves return to the host only when the
shell also implements `fes.media.zx81-tape-write` 1.0 (capability bit 14,
`0x4000`).

### fes.media.zx81-tape-write 1.0

This extension occupies the same unit 0 as `fes.media.zx81-tape` and reuses
snapshot opcodes 12–15. The captured image is the program the ROM SAVE
routine emits, from `$4009` through the byte before `E_LINE`. It is
independent of the cassette loaded for `LOAD`: Freeze is ready when that
save image is ready, including when unit 0 is empty. Begin and Eject of the
load cassette do not clear the save image or its dirty bit.

SnapshotInfo layout tag is 2, major 1, minor 0. Size is the captured byte
count, 1..16384. Chunks are 2..512 bytes and even, except a final chunk of
length 1 when the image size is odd. That chunk's data word is the byte in
the low half and zero in the high half. The change epoch advances once per
completed SAVE. Flags use the same ready, dirty, frozen and busy bits as the
ST snapshot. Busy covers the SAVE emitter as well as an in-progress `LOAD`
copy. Dirty clears on Saved, not on cassette replacement or eject.

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

## ATA hard disk

`fes.media.ata-disk` 1.0 (bit 16) is one fixed hard disk on unit 0. The image
is too large for the media units, which move two bytes per transaction and
stop at 32 MiB, so the mailbox moves no disk data. The runtime writes the
image directly into the shell's HPS DDR window at offset `0x08000000`
(`FesComputerAtaDiskWindowOffset`) while execution is held; the mailbox
carries control, geometry, a CRC-32 the core checks, and the dirty map. The
legacy media opcodes 5..15 stay absent unless a legacy media interface is
also declared.

The image is 1,048,576..134,217,728 bytes (1..128 MiB), a whole number of
512-byte sectors (2,048..262,144). Chunks are 65,536 bytes (128 sectors), so
the dirty map has 2,048 bits; the last chunk of an image that is not a whole
number of chunks is short. A unit is empty, loading, verifying (4: the core
is checking the CRC) or ready, with the media unit state numbers for the
others. The machine sees a drive only while the unit is ready. The disk is
fixed while the machine runs: Prepare, DiskCommit words, Commit and Eject are
valid only while held (invalid state otherwise).

**DiskInfo** (16): unit = index>>3 and field = index&7. A unit of 8 or more
is an invalid index and a nonzero argument invalid; every field of units
1..7 reads 0. Fields: 0/1 minimum bytes low/high, 2/3 maximum bytes
low/high, 4 chunk size in sectors (128), 5 state, 6 flags, 7 a wrapping
change epoch that advances once for every guest sector write that reaches
the image. Flag bits: 0 dirty (a dirty bit is set), 1 frozen, 2 writer busy (an
accepted sector write has not yet reached the image), 3 CRC error. Other bits
read 0. Writer busy clears only when that accepted write reaches the image.

**DiskControl** (17): index = unit; a unit other than 0 is an invalid index
and an argument above 5 is invalid.

- **Prepare** (0): held only; refused while frozen unless Saved was
  acknowledged. The unit becomes loading: the drive disappears from the
  machine, staged commit words, the dirty map, the error flag and the freeze
  are cleared. Prepare also restarts DiskCommit staging.
- **Commit** (1): held only, the unit loading and all eight DiskCommit words
  staged (invalid state otherwise). The staging is consumed, the unit becomes
  verifying and the core reads the size it was given from the window,
  computing CRC-32/IEEE as the media units do. A match makes the unit ready
  with that geometry; a mismatch leaves it loading with the error flag set,
  and the host may stage the words again and commit, or prepare. The ACK comes
  before the check ends: the host polls DiskInfo field 5 until it reads
  something other than verifying (about 0.2 s for 128 MiB on port 2).
- **Eject** (2): held only, refused while frozen unless Saved was
  acknowledged. The unit becomes empty and everything Prepare clears is
  cleared.
- **Freeze** (3): requires ready (invalid state otherwise), held or
  released. It fences new guest sector writes (a write command waits with BSY
  set), waits until an accepted write has reached the image and then ACKs.
  Every successful Freeze clears an earlier Saved acknowledgement, including
  when the disk is already frozen. Repeating Freeze otherwise leaves the
  existing fence in place.
- **Resume** (4): always valid and idempotent; it releases the fence and
  forgets Saved.
- **Saved** (5): requires frozen (invalid state otherwise) and no busy
  writer. It clears the dirty map, keeps the fence and permits Prepare or
  Eject. The host sends it only after publishing the saved chunks.

**DiskCommit** (22): index = word 0..7 (else invalid index). The unit must be
held and loading (invalid state), and the word must be the next one staged
(invalid index); a rejected word is not consumed. Words: 0/1 size in sectors
low/high, 2 cylinders, 3 heads, 4 sectors per track, 5/6 CRC-32 low/high of
the image as written, 7 zero. Checks (invalid argument): the size at word 1 is
2,048..262,144 sectors; cylinders 1..16,383; heads 1..16; sectors per track
1..63, and cylinders × heads × sectors per track must not exceed the size;
word 7 is 0. The geometry is what IDENTIFY DEVICE reports and what the
partition table was written with; the LBA capacity is the size.

**DiskDirty** (23): index = word 0..127 (else invalid index), argument 0
(else invalid). It requires frozen (invalid state) and returns chunks
16 × index .. 16 × index + 15, bit 0 first. A chunk's bit is set when a guest
sector write to it reaches the image. Bits past the image's last chunk read 0.

A guest sector write is staged whole before any byte reaches the image, so a
frozen image never holds a torn sector. The fence and drain follow the
[writable ST capture](#writable-st-image-capture); this disk adds no
snapshot opcode, because the host reads the dirty chunks from the window
after Freeze is acknowledged, invalidating its cached view of them first.

The host's sequence at launch is: Prepare; write the image into the window,
clean the CPU caches for it and revoke the grant; DiskCommit words 0..7;
Commit; poll DiskInfo until ready; release. At Stop it is: Freeze; DiskDirty
words; read the dirty chunks; publish the [chunk delta](media-data.md#ata-disk-delta);
Saved. The durable delta and its namespace are the media data record
`fes.ata-disk.delta` 1.0.

## Persistence words

`fes.persistence.words` 1.0 (bit 17) has the semantics it has on
`fes.simple-game` ([schema](schema.md#core-persistence-interfaces)), on
opcodes 24..27. DataControl (24) takes index 0 (else invalid index) and
argument 0 Freeze, 1 Begin, 2 Commit or 3 Resume (else invalid argument).
Freeze requires released execution and latches a snapshot of every word
before ACK; repeating it keeps the first snapshot. Begin requires held
execution and opens empty restore staging. Commit requires held execution
and every word staged, then checks the layout's values (invalid argument,
staging kept) and replaces the words atomically, closing staging. Resume
requires a snapshot and drops it. DataRead (25) checks index below the word
count (invalid index), argument 0 (invalid argument), then a snapshot
(invalid state). DataWrite (26) checks the index, then held execution with
staging open (invalid state). Words are opaque until Commit. DataInfo (27)
index 0..3 (else invalid index), argument 0, reports word count, layout tag,
major and minor.

The record is per library game: the host restores it before release and
captures it at Stop. A missing record starts the layout's defaults. Its
bytes on the kit are a `FESGAME1` record:

| Offset | Size | Bytes |
| --- | --- | --- |
| 0 | 8 | ASCII `FESGAME1` |
| 8 | 32 | SHA-256 of UTF-8 core ID |
| 40 | 32 | SHA-256 of UTF-8 library game ID |
| 72 | 2 | Little-endian layout ID byte length L (1..96) |
| 74 | 2 | Little-endian layout major |
| 76 | 2 | Little-endian layout minor |
| 78 | 2 | Little-endian word count N (1..256) |
| 80 | L | ASCII layout ID |
| 80+L | 2N | Little-endian words |
| 80+L+2N | 32 | SHA-256 of every preceding byte |

The size is exactly 112 + L + 2N bytes, at most 720. No padding or trailing
bytes are allowed. Its namespace is lowercase SHA-256 of `fes-core-game-data-v1`,
NUL, core ID, NUL, game ID; package, firmware and display names do not name
it. Revision is lowercase SHA-256 of the whole record and `absent` names a
missing one. A `FESDATA1` decoder rejects the record by its magic, and this
decoder rejects `FESDATA1`. A corrupt or incompatible record blocks launch
and is never replaced with defaults.

### fes.pc386.machine 1.0

Layout tag 3 (tags 1 and 2 are the snapshot layouts), 72 words. Words 0..7
are settings FogCast writes for the next launch; words 8..71 are CMOS bytes
00h..7Fh, two per word, low byte first, which the machine writes.

| Word | Content | Values | Default |
| --- | --- | --- | --- |
| 0 | Governor preset | 0 `max`, 1 286-12, 2 386SX-16, 3 386DX-25, 4 386DX-33, 5 386DX-40, 6 486DX-33, 7 486DX2-66 | 0 |
| 1 | Preset frequency override, 100 kHz units | 0 for the preset's own, or 10..1000 (1..100 MHz); 0 under `max` | 0 |
| 2 | Self-modifying-code profile | 0 `coherent`, 1 `queue16` | 0 |
| 3 | Turbo at launch | 0 the preset, 1 `max` | 0 |
| 4 | Sound Blaster base port | 0x220 or 0x240 | 0x220 |
| 5 | Sound Blaster IRQ | 2, 3, 5 or 7 | 7 |
| 6 | Sound Blaster DMA channel | 1 | 1 |
| 7 | Reserved | 0 | 0 |
| 8..71 | CMOS 00h..7Fh | any | 0 |

Commit rejects a value outside its set. Settings are read at the next release,
so the governor, CPU profile and Sound Blaster are set at reset. The clock
bytes are captured with the rest but the [wall clock](#wall-clock) overwrites
them at the next launch.

## Wall clock

`fes.rtc.wall-clock` 1.0 (bit 18) sets the machine's clock from the kit's
local time, on opcode 28, while held (invalid state otherwise). Index is the
word 0..3 (else invalid index). Word 0 always starts a new time; words 1..3
must follow in order (invalid index), and word 3 applies all four at once. A
rejected word is not consumed. Words are binary, first-named byte low:

| Word | Low byte | High byte | Checks (invalid argument) |
| --- | --- | --- | --- |
| 0 | Year, 16 bits | | 1980..2099 |
| 1 | Month | Day | month 1..12, day 1..the month's length in that year |
| 2 | Hour | Minute | hour 0..23, minute 0..59 |
| 3 | Second | Weekday | second 0..59, weekday 1 (Sunday)..7 |

The host has three duties. It sends the date's own weekday, counted as the
MC146818 counts it. It clamps a leap second to 59. It clamps a kit time before
1980 to 1980-01-01 00:00:00, a Tuesday (3). The machine does not compare the
weekday with the date. Like the MC146818, it stores the weekday it is given.
The machine converts to its guest's BCD or binary and 12- or 24-hour forms. A
guest that changes the time keeps it until Stop.

## Audio

`fes.audio.pcm-s16-stereo-48k` 1.0 has exactly the board transport, silence
and ownership rules of the [application audio contract](application-io.md#audio):
no opcode, 48 kHz signed 16-bit stereo I2S, silence while held, runtime-owned
ADV7513 packet enable.

## Evidence and ownership

[Golden exchanges](../testdata/fes-computer-v1/exchanges.json) are generated by
`scripts/fes_computer_fixtures.py`, whose `Endpoint` class is the reference
model of this document, and replayed against an independent Go model in
`internal/pack/fes_computer_test.go`. They describe synthetic 1.0 sessions:
keyboard and controllers without media, live media on a synthetic unit 0
that advertises 1..1030 bytes so multi-chunk, odd-length, CRC and eject cases
fit in a small file, and the relative mouse. The 1.1 additions are in
[`exchanges-1.1.json`](../testdata/fes-computer-v1/exchanges-1.1.json), from
the same script's `Endpoint11` and replayed in
`internal/pack/fes_computer_v11_test.go`: a 1.0 endpoint that rejects word 16
and the new opcodes; persistence restore, wall-clock seeding and the Stop
snapshot of a pc386 endpoint; and its ATA disk with a 2 MiB synthetic window
(`(offset * 7 + (offset >> 11)) & 255`). The fixture's endpoint finishes a CRC
check after one DiskInfo state read reports verifying, a scenario parameter;
`machine_events` names guest sector and CMOS writes between exchanges. The
pc386 record vectors are [`pc386-machine.json`](../testdata/core-persistence-v1/pc386-machine.json). Consumers own their implementations and must replay these
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
