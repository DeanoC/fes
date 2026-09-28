# Home-computer I/O

The `fes.computer` 1.0 ABI uses identity tag 4 on the existing `fes-gp-v1`
programming profile and GP transport. The authoritative wire contract is
mister-packages [home-computer I/O](../../mister-packages/docs/computer-io.md);
the constants are generated from `packages/abi/fes_computer.yaml` into
`src/native/generated/fes_computer.hpp`. The consumers are the FES Apple II
([pathfinder contract](../../../docs/superpowers/specs/2026-09-26-apple2-pathfinder-design.md))
and the FES ZX Spectrum
([pathfinder contract](../../../docs/superpowers/specs/2026-09-28-spectrum-pathfinder-design.md)).
Existing `fes.simple-game`, `fes.simple-computer` and `fes.application`
packages keep their requirements, wire identities and startup behavior.

## Admission

A package declares `fes.computer` major 1 with `fes-gp-v1`. Minor versions
above 0 and any `core.system` value are rejected. `fes.video.fixed-720p60` 1.0
is required. The recognized operational set is `fes.video.fixed-720p60`,
`fes.keyboard.hid`, `fes.gamepad.ports`, `fes.audio.pcm-s16-stereo-48k`,
`fes.media.apple2-floppy` and `fes.media.spectrum-tape`, each 1.0; each must
be declared required when present. A shell declares at most one of the two
unit-0 media interfaces. `fes.expansion.apple2-bus` 1.0 and
`fes.expansion.spectrum-bus` 1.0 are manifest-only multi-socket buses and must
be optional. Unknown or unsupported-version optional declarations are ignored
and grant nothing; unknown or unsupported required declarations fail admission.

Format 2 and format 3 with a `firmware` ROM are admitted. Format 3 with a
`cartridge` ROM and format 4 are rejected: this ABI releases execution right
after identity and has no media gate that could hold a linked cartridge.
Firmware ROM packages activate through `load_rom_core`,
`load_rom_library_core` or `load_rom_composed_core` with the existing
`rom_link` receipt. The ABI has no persistence interface, so library loads
record volatile core data.

Native capabilities advertise the ABI with every recognized interface:

```json
{"id":"fes.computer","major":1,"minor":0,"interfaces":[{"id":"fes.audio.pcm-s16-stereo-48k","major":1,"minor":0},{"id":"fes.expansion.apple2-bus","major":1,"minor":0},{"id":"fes.expansion.spectrum-bus","major":1,"minor":0},{"id":"fes.gamepad.ports","major":1,"minor":0},{"id":"fes.keyboard.hid","major":1,"minor":0},{"id":"fes.media.apple2-floppy","major":1,"minor":0},{"id":"fes.media.spectrum-tape","major":1,"minor":0},{"id":"fes.video.fixed-720p60","major":1,"minor":0}]}
```

## Lifecycle

Identity must equal the manifest: tag 4, ABI 1.0, build ID, and live
capability bits 0 through 5 equal to the declared required recognized set.
Unregistered live bits above 4 are ignored, as for applications. Discovery
then reads MediaInfo fields 0 through 5 for every declared media unit
(`fes.media.apple2-floppy` is unit 0). The unit must be present and empty, with
512-byte chunks and 1 <= minimum <= maximum <= `FesComputerMediaMaxBytes`
(32 MiB); otherwise activation fails as an identity mismatch. The runtime does
not substitute interface sizes for live Info: an Apple II core reports
143,360 for both limits.

After verified identity the ADV7513 audio packets follow the audio
declaration exactly as for applications. Execution is released immediately:
Start sends only Execution release, with no media gate and no neutral writes,
because programming starts held with neutral input and empty units. No evdev
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
must be ready. Any failure after the first exchange, including a rejected
request, a CRC mismatch or an ambiguous handshake, ejects that unit once
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

Single-socket ZX81 and Coleco requests, manifests, identities and status are
unchanged, and neither shape composes the other bus.

## Evidence

Host tests replay the shared golden exchanges through the GP transport, check
them against an independent C++ reference endpoint, and drive the driver's
identity, HID deltas, controller ports, release, hold and live media
transfers against it, including the synthetic 1..1030-byte unit, CRC, lost and
rejected requests and failed cleanup. Admission, protocol, runtime binding,
multi-slot composition and a runtime lifecycle over native hardware are
covered. No hardware support is claimed.
