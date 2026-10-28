# Home-computer I/O

The `fes.computer` 1.0 and 1.1 ABIs use identity tag 4 on the existing `fes-gp-v1`
programming profile and GP transport. The authoritative wire contract is
mister-packages [home-computer I/O](../../mister-packages/docs/computer-io.md);
the constants are generated from `packages/abi/fes_computer.yaml` into
`src/native/generated/fes_computer.hpp`. The consumers are the FES Apple II
([pathfinder contract](../../../docs/superpowers/specs/2026-09-26-apple2-pathfinder-design.md)),
the FES ZX Spectrum
([pathfinder contract](../../../docs/superpowers/specs/2026-09-28-spectrum-pathfinder-design.md))
the FES Commodore 64 and the FES Atari 520ST, all 1.0, and the FES pc386 on 1.1.
Existing `fes.simple-game`, `fes.simple-computer` and `fes.application`
packages keep their requirements, wire identities and startup behavior.

The common GP transport samples responses without reissuing requests. Once
ACK matches the outstanding request, it holds GPO, allows a one-microsecond
settling guard and requires two identical complete valid GPI reads. Changes
in the response/error payload may settle within the original exchange
deadline, capped at 100 ms. ACK reversion, malformed signature/reserved bits,
MMIO error or instability beyond that bound fails and poisons the link; no
partial response is returned. Re-alignment applies the same read-only check.
Host coverage of this policy is separate from physical diagnostic results
and exact-artifact acceptance.

## Admission

A package declares `fes.computer` major 1 with `fes-gp-v1`. Minor versions
above 1 and any `core.system` value are rejected. Minor 1 adds identity word
16, capability bits 16..31, and their interfaces: `fes.media.ata-disk` 1.0
(a unit-0 medium that requires `fes.memory.hps-ddr`), `fes.persistence.words`
1.0 with its one layout `fes.pc386.machine` 1.0, and `fes.rtc.wall-clock` 1.0.
A 1.0 package that declares any of them is refused as an unsupported ABI. `fes.video.fixed-720p60` 1.0
is required. The recognized operational set is `fes.video.fixed-720p60`,
`fes.keyboard.hid`, `fes.gamepad.ports`, `fes.audio.pcm-s16-stereo-48k`,
`fes.media.apple2-floppy`, `fes.media.zx81-tape`, `fes.media.c64-disk`
`fes.media.atari-st-floppy` and `fes.media.spectrum-disk`, each 1.0, and
`fes.media.spectrum-tape` 2.0; each must be declared required when present. A
shell declares at most one unit-0 medium, and `fes.media.spectrum-disk` only
beside `fes.media.spectrum-tape` and `fes.memory.hps-ddr`. `fes.expansion.apple2-bus` 1.0,
`fes.expansion.spectrum-bus` 1.0, `fes.expansion.c64-bus` 1.0 and
`fes.expansion.atari-st-bus` 1.0 are manifest-only buses and must be optional.
Unknown or unsupported-version optional declarations are ignored and grant nothing;
unknown or unsupported required declarations fail admission.

`fes.media.spectrum-snapshot`, `fes.media.spectrum-snapshot-128` and
`fes.media.spectrum-snapshot-plus3` 1.0 are recognized optional package metadata.
The runtime reports a declared matching version as active for the host media
picker; these interfaces add no runtime operation, media unit or capability bit.

Format 2 and format 3 with a `firmware` ROM are admitted. Format 3 with a
`cartridge` ROM and format 4 are rejected: this ABI releases execution right
after identity and has no media gate that could hold a linked cartridge.
Firmware ROM packages activate through `load_rom_core`,
`load_rom_library_core` or `load_rom_composed_core` with the existing
`rom_link` receipt. The ABI has no core-scoped persistence interface; an
explicit initial library disk or later library insertion supplies disk
persistence and changes the active persistence mode.

Native capabilities advertise each minor as its own ABI entry, so a 1.0
package keeps matching its exact ABI. The 1.0 entry has every recognized 1.0
interface:

```json
{"id":"fes.computer","major":1,"minor":0,"interfaces":[{"id":"fes.audio.pcm-s16-stereo-48k","major":1,"minor":0},{"id":"fes.expansion.apple2-bus","major":1,"minor":0},{"id":"fes.expansion.atari-st-bus","major":1,"minor":0},{"id":"fes.expansion.c64-bus","major":1,"minor":0},{"id":"fes.expansion.spectrum-bus","major":1,"minor":0},{"id":"fes.gamepad.ports","major":1,"minor":0},{"id":"fes.keyboard.hid","major":1,"minor":0},{"id":"fes.media.apple2-floppy","major":1,"minor":0},{"id":"fes.media.atari-st-floppy","major":1,"minor":0},{"id":"fes.media.c64-disk","major":1,"minor":0},{"id":"fes.media.spectrum-disk","major":1,"minor":0},{"id":"fes.media.spectrum-snapshot","major":1,"minor":0},{"id":"fes.media.spectrum-snapshot-128","major":1,"minor":0},{"id":"fes.media.spectrum-snapshot-plus3","major":1,"minor":0},{"id":"fes.media.spectrum-tape","major":2,"minor":0},{"id":"fes.media.zx81-tape","major":1,"minor":0},{"id":"fes.video.fixed-720p60","major":1,"minor":0}]}
```

`fes.memory.hps-ddr` 1.0 is added, and sorted with the rest, when the boot
core latched the port layout, including a kit without presentation adapters.
`fes.video.session-display` 1.0 is added only when that layout and both
presentation adapters are present, together with its shared plane
`fes.fabric.video.display-plane` 1.0. That plane has no capability bit; a
package declaring either one without the other is refused at compatibility. A
computer that does not implement the plane leaves the session-display bit clear.
The 1.1 entry has the same interfaces plus `fes.pc386.machine`,
`fes.persistence.words` and `fes.rtc.wall-clock`, each 1.0, and
`fes.media.ata-disk` 1.0 when the DDR layout is latched and the window driver
is available.

## Lifecycle

Identity must equal the manifest: tag 4, the manifest's ABI minor, build ID,
and live capability bits in the registered mask equal to the declared required
recognized set. A 1.1 package's identity is 17 words; word 16 is compared like
word 7 with the registered bits 16..31. Bits outside that mask are ignored, as for applications. Discovery
then reads MediaInfo fields 0 through 5 for every declared media unit.
`fes.media.apple2-floppy`, `fes.media.spectrum-tape`, `fes.media.zx81-tape`,
`fes.media.c64-disk`
and `fes.media.atari-st-floppy`
are each unit 0, and a shell declares one of them; `fes.media.spectrum-disk`
is unit 1. Each declared unit must be present
and empty, with 512-byte chunks and 1 <= minimum <= maximum <=
`FesComputerMediaMaxBytes` (32 MiB); otherwise activation fails as an identity
mismatch. The runtime does not substitute interface sizes for live Info: an
Apple II core reports 143,360 for both limits, a Spectrum core reports 1 and
65,536, and a Commodore 64 core reports 174,848 for both limits. The Atari ST
contract requires exactly 737,280 for both limits at discovery and before
each insertion; a changed limit is rejected before MediaBegin and follows
the same failure-eject behavior as other live transfer failures.

After verified identity the ADV7513 audio packets follow the audio
declaration exactly as for applications. A library launch of a package with
`fes.persistence.words` restores its game's 72-word `fes.pc386.machine`
record (DataControl Begin, 72 DataWrite on opcodes 24..27, Commit) while
held; a missing record restores the layout's defaults. A package with
`fes.rtc.wall-clock` then gets the kit's local time in four WallClock words;
a time before 1980 starts the clock at 1980-01-01, a leap second holds at 59,
and an unavailable local time fails the launch.

## ATA hard disk

A `load_library_core` of a package with `fes.media.ata-disk` may carry its
game's disk; it requires `game_id`:

```json
{"protocol":2,"operation":"load_library_core","package_path":"/tmp/fogcast-development/core-packages/example","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","data_root":"/media/fat/fogcast/core-data","game_id":"ultima-vi","ata_disk":{"path":"/media/fat/fogcast/disk-images/a.img","size":2098688,"base_media_id":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","data_root":"/media/fat/fogcast/core-data/media","cylinders":4,"heads":16,"sectors_per_track":63}}
```

The image is 1..128 MiB in whole sectors and its geometry must fit it as
DiskCommit requires. Before programming the runtime opens the image (no
symlinks; its size must equal `size`), opens the `fes.ata-disk.delta`
namespace of core, game and base digest under `ata_disk.data_root`, reads
the manifest and checks that the namespace is writable. A pc386 package
without `ata_disk` launches with its drive empty.

After identity and the record restore, while held, the runtime sends
Prepare and writes the image into the DDR window from `0x08000000` through
fes-window grants of 4 MiB in turn: each 64 KiB chunk is the base's bytes, or
the delta's chunk file when the manifest names it. It hashes the base as it
reads it and CRC-32s what it writes. Each grant is revoked before the next,
which writes back and invalidates its lines in L1 and the PL310. A base whose
SHA-256 differs from `base_media_id`, a missing or altered chunk file, or a
CRC the core does not match fails the launch with `corrupt_data`. Then
DiskCommit words 0..7 and Commit, and DiskInfo is polled until the core's CRC
pass ends. Status lists the disk as unit 0 with interface
`fes.media.ata-disk`, 65,536-byte chunks and its binding's revision. It is
fixed: media insert, eject and save refuse it.

Stop and fault cleanup save it before the record: Freeze, the 128 DiskDirty
words, then each dirty chunk read through a fresh grant (taken after Freeze
has fenced the core's writes, so no line cached before them can exist) and
written as a synced content-addressed file. The merged manifest is published
against the revision read under the namespace lock, chunk files it does not
name are deleted, and Saved is sent. A Stop with no dirty chunk publishes
nothing. A failed save leaves the disk frozen and returns `save_failed`; the
resumed game sends Resume first. An explicit initial library disk is
uploaded and confirmed ready before Start; otherwise the drive stays empty.
Start sends only Execution release, with no media gate and no neutral writes,
because programming starts held with neutral input. No evdev
input worker is opened. Stop and replacement send Execution hold before
reprogramming. Hold neutralizes every key row and both ports in the core, so no
neutral writes follow it; the runtime treats its own input state as neutral.

## Protocol 2 operations

Every field is required and no other field is accepted. Generations are
positive integers. Each operation binds the exact active package and
generation and follows the existing lifecycle busy boundary, so input and media
requests are serialized with each other, Stop and replacement.

### `set_keyboard_hid`

```json
{"protocol":2,"operation":"set_keyboard_hid","package_id":"<64 lowercase hex>","expected_generation":3,"rows":[16,0,0,0,0,0,0,0,2]}
```

`rows` is exactly nine integers 0..65535. Row 0 bits 0..3 (HID error usages)
and row 8 bits 8..15 must be zero. The active generation must be a
`fes.computer` package declaring required `fes.keyboard.hid` 1.0; otherwise the
result is `unsupported_interface` (phase `compatibility`). The request is a
complete snapshot. The driver writes KeyboardHid (opcode 3) only for rows that
differ from the last acknowledged state, in ascending row order; the first
snapshot after Start writes all nine rows. A multi-row change is several
ordered transactions, not an atomic update. A failed row exchange retires the
generation through the one-shot input-fault cleanup, as for `set_controller`,
and the next snapshot would write every row again.

### `set_controller`

The existing request shape is unchanged:

```json
{"protocol":2,"operation":"set_controller","package_id":"<64 lowercase hex>","expected_generation":3,"port":0,"buttons":17,"keypad":0}
```

A `fes.computer` package that declares `fes.gamepad.ports` accepts it on
ControllerButtons (opcode 4). There is no keypad on this ABI: a nonzero
`keypad` is `unsupported_interface` and reaches no hardware.

### `insert_media`

```json
{"protocol":2,"operation":"insert_media","path":"/absolute/disk.dsk","expected_package_id":"<64 lowercase hex>","expected_generation":3,"unit":0,"size":143360}
```

`unit` is 0..7 and must belong to a declared media interface of the active
`fes.computer` generation (`unsupported_interface` otherwise). `size` is
1..33554432 and must lie within the unit's observed `min_bytes..max_bytes`;
the file is snapshotted with its CRC32 and must be exactly `size` bytes. File
admission failures send nothing to the core.

The transfer never holds execution reset: MediaInfo fields 0..5 (state must
not be absent, 512-byte chunks, `size` within the live limits), MediaBegin
words 0..3 (total, CRC32), then for each 512-byte chunk MediaChunk words 0..2
and ceil(length/2) MediaData words, MediaCommit, and finally MediaInfo state
must be ready. A failed preflight or completed rejection of the first Begin
word preserves the current disk. ST insert/eject rejects while the guest writer
is busy, including collection and sector commit. Once Begin word 0 is accepted
or ambiguously issued, a transfer failure, including a rejected later request,
a CRC mismatch or an ambiguous handshake, ejects that unit once
(MediaEject) and reports the original error; an ambiguous mailbox is first
realigned from the live ACK and re-identified, and the ambiguous request is
never repeated. If that eject also fails the message gains
`"; media eject failed: <cause>"` and the unit is reported `loading`
(not known to be ready). Execution stays released and the generation stays
active in every case; `eject_media` or another `insert_media` re-establishes
the unit.

### `eject_media`

```json
{"protocol":2,"operation":"eject_media","expected_package_id":"<64 lowercase hex>","expected_generation":3,"unit":0}
```

It realigns and re-identifies an ambiguous mailbox first, sends MediaEject for
the unit, then confirms MediaInfo state empty. Eject is idempotent.
If the eject exchange becomes ambiguous after the command is sent, the runtime
returns its input-phase `io_failed` result after best-effort realignment; the
core may already have accepted the eject. A completed loader rejection remains
retryable `busy` and confirms that the media was not changed by that attempt.

### Status

While a `fes.computer` generation is active, `capabilities.media_units`
reports each declared unit in ascending unit order, with limits and state from
the unit's most recent live MediaInfo or acknowledged transition. The list is
empty for a computer without media interfaces and absent for other ABIs and
while idle:

```json
"media_units":[{"unit":0,"interface":{"id":"fes.media.apple2-floppy","major":1,"minor":0},"min_bytes":143360,"max_bytes":143360,"chunk_bytes":512,"state":"empty"}]
```

`state` is `empty`, `loading` or `ready`. Every operation response carries
this status, so a successful `insert_media` returns `ready`.
[`tests/fixtures/protocol-v2-computer-responses.jsonl`](../tests/fixtures/protocol-v2-computer-responses.jsonl)
holds serializer-emitted responses that FogCast checks byte for byte.

## Multi-slot composition

A `fes.computer` shell that declares optional `fes.expansion.apple2-bus` 1.0
is composed with one card per Apple II slot. `load_composed_core`,
`load_rom_composed_core` and `load_initialized_composed_core` accept, instead
of `expansion_path` and a single-socket tuple, an `expansions` array and a
multi-slot `composition` tuple; all other fields of each operation are
unchanged:

```json
{"protocol":2,"operation":"load_rom_composed_core","package_path":"/pkg","package_id":"<P>","expansions":[{"slot":4,"path":"/cards/4"},{"slot":6,"path":"/cards/6"}],"payload_path":"/composition/linked.rbf","composition":{"composition_id":"<C>","package_id":"<P>","expansions":[{"slot":4,"expansion_id":"<E4>"},{"slot":6,"expansion_id":"<E6>"}],"shell_sha256":"<S>","payload_sha256":"<L>","payload_size":1816338},"programmed_path":"/programmed.rbf","rom_link":{...}}
```

`expansions` lists one to seven entries with ascending unique slots 1..7; the
tuple lists the same slots in the same order. `payload_size` is
40408..33554432 and the tuple `package_id` must equal the request. Each
expansion directory holds exactly `manifest.json` and `cart.rbf`. The manifest
is the canonical compact JSON of the single-socket grammar with `slot_index`
between `slot` and `slot_major`:

```json
{"cart_sha256":"…","cart_size":40408,"device":"5CSEBA6U23I7","format":1,"map":"fes.apple2-bus.slots/1","recipe_sha256":"…","revision":"…","shell_build_id":"…","shell_package_id":"…","shell_sha256":"…","slot":"fes.expansion.apple2-bus","slot_index":6,"slot_major":1,"slot_minor":0}
```

`slot_index` must equal the request slot and belong to the map's socket set.
The physical socket set of `fes.apple2-bus.slots/1` is not final, so every slot
1..7 is admitted until the shell's socket table is sealed. Each manifest binds
the exact shell package, BUILD_ID and payload digest, its cart digest and size.
The expansion ID is SHA-256 of `fes-expansion-v1`, NUL and the manifest bytes.
The composition ID is SHA-256 of `fes-composition-v2`, NUL, the package ID,
NUL, then for each slot in ascending order the decimal slot, `:`, the
expansion ID and NUL, then the linked payload SHA-256.

As for single-socket compositions the target agent links with the shared
misteross implementation; the runtime verifies the result by hash, retains
every manifest, cart and the linked payload, and rehashes all of them
immediately before programming. It does not relink. Status reports the same
object under `active_package.composition`:

```json
"composition":{"composition_id":"<C>","package_id":"<P>","expansions":[{"slot":4,"expansion_id":"<E4>"},{"slot":6,"expansion_id":"<E6>"}],"shell_sha256":"<S>","payload_sha256":"<L>","payload_size":1816338}
```

`fes.expansion.c64-bus` 1.0 uses the same composition operations. Its map
`fes.c64-bus.sockets/1` admits only socket 1 (ROM window) and socket 2 (I/O
window), at most two cards. `fes.media.c64-disk` 1.0 is media unit 0, an
exact 174,848-byte read-only D64. `fes.expansion.spectrum-bus` 1.0 uses the
same operations. Its map `fes.spectrum-bus.sockets/2` admits the one edge socket, 1.
`fes.media.spectrum-tape` 2.0 is media unit 0, a 10..16,777,216-byte PZX
stream; the host converts `.tzx`, `.tap`, `.csw` and `.pzx` images to it.
`fes.media.zx81-tape` 1.0 is media unit 0, a 1..16,384-byte `.p`. Begin and
Eject wait while that core reports the loader busy.

`fes.expansion.atari-st-bus` 1.0 uses the same slot-composition operations
with map `fes.atari-st-bus.socket/1`, physical socket 1 and at most one card.
Its shared CRAM map reserves `[1769,32,2806,1722]` and the 56-bit request,
32-bit response boundary. `fes.media.atari-st-floppy` 1.0 is media unit 0,
an exact 737,280-byte read-only raw `.st` image. Host tests cover contract
admission, exact live limits, full insertion while execution is released,
ready confirmation, ejection and socket composition; hardware acceptance is
pending.

Single-socket ZX81 composition admits optional `fes.expansion.zx81-bus` 1.0
or 2.0 on `fes.simple-computer` 1.0 and `fes.computer` 1.0. Coleco requests
stay on `fes.application`. Neither shape composes the other bus.

## Evidence

Host tests replay the shared golden exchanges through the GP transport, check
them against an independent C++ reference endpoint, and drive the driver's
identity, HID deltas, controller ports, release, hold and live media
transfers against it, including the synthetic 1..1030-byte unit, CRC, lost and
rejected requests and failed cleanup. Admission, protocol, runtime binding,
multi-slot composition and a runtime lifecycle over native hardware are
covered. No hardware support is claimed.

Stop and core replacement save before programming. Successful programming
retires the previous disk's in-memory binding, including when Stop returns to
the configured menu or splash. Without explicit initial library media, the
next ST load is empty and volatile until `insert_library_media` restores its
record. A failed save retains
the current binding and generation for recovery. Host regressions cover both
idle paths and replacement without restarting the runtime daemon.

## Initial library disk during ROM activation

`load_rom_core`, `load_rom_library_core` and `load_rom_composed_core` accept an
optional `initial_media` object. It has exactly these fields:

```json
{"path":"/media/fat/fogcast/staging/disk.st","size":737280,"unit":0,"data_root":"/media/fat/fogcast/core-data/media","game_id":"st-game","base_media_id":"<lowercase SHA-256 of the immutable base disk>"}
```

This explicitly requests a library disk for a format-3 `fes.atari-st` firmware
package declaring both ST floppy and writable-floppy 1.0. Other cores, formats,
ROM roles, units and disk sizes reject it before programming. The local agent
supplies its trusted media-data root and rejects initial media on development
requests. The object's media-data root is separate from the optional core-data
root of `load_rom_library_core`.

Admission snapshots and hashes the complete source, validates the durable
record and checks namespace writability before retiring the previous session.
After outgoing save, it refreshes the incoming record; the same namespace
therefore restores the just-published bytes. Programming and identity discovery
precede disk upload. Every upload chunk, commit and ready confirmation occurs
while the fresh ST remains in reset. Binding metadata is published only after
ready; Start then releases execution. Load status reports a ready persistent
unit with the requested game/base identities and the selected record revision.
A failed upload never releases execution or replays its ambiguous command;
existing launch recovery owns cleanup. A failed outgoing save retains the
previous disk and generation.

If execution release fails ambiguously after binding the disk, failed-launch
cleanup first performs the existing fault-save capture and durable publication.
The release command is never resent. Successful save allows idle replacement;
failed save retains the newly selected package/generation and persistent disk
in recovery, blocking generic idle programming and replacement.

Omitting this object preserves an empty volatile drive at launch: diskless
boot to GEM remains supported even when the package requires both floppy
interfaces. Those requirements describe hardware capabilities, not inserted
media. Ordinary live insertion remains available. Two-source ROM loads reject
initial media. This startup path has host coverage and the
[exact-package automatic-boot diagnostic](../../../docs/validation/2026-10-06-atari-st-auto-boot.md); assembled appliance acceptance remains pending.

Required `fes.media.atari-st-floppy-geometry` 1.0 permits the 12 uniquely sized
raw ST layouts with 80–82 tracks, one/two heads, nine/ten sectors and 512-byte
sectors (368640–839680 bytes). Discovery proves capability bit 10 and exact
extended live limits. Nonlegacy immutable bases require matching BPBs; saved
records use minor 1, retain guest BPB edits, and must match the base length
before restore. Legacy 737280-byte behavior and minor-0 bytes are unchanged.
This extension has software coverage; no new hardware acceptance is claimed.
