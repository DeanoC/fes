# FES Apple II pathfinder contract

**Status:** Approved direction 2026-09-26 (new `fes.computer` ABI; full multi-socket chain). Simulation of the machine and Disk II passes; no sealed package, runtime, host or kit evidence exists yet.
**Base:** FES `583822c8` plus the `kepler/apple2-pathfinder-implementation` branch (ABI contract `49fa1c46`).
**Owners:** mister-packages for the ABI; misteross for the machine, endpoint, shell, cards, producers and the Go linker; libmister-runtime for admission, the GP driver and protocol; FogCast for library selection, input, media and composition; FES for recipes and evidence. All components change in this one integration branch.

## Intended result

`fes.apple2` is the first FES home computer and the pathfinder for an FPGA computer that can take cards, change ROMs without a rebuild and run software from removable media:

- An Apple II+ class machine: NMOS 6502 at 1.0205 MHz, 48 KiB RAM plus a built-in 16 KiB language card, text/lo-res/hi-res video with NTSC artifact colour on fixed 720p60 HDMI, speaker audio, keyboard, two push buttons/paddles from controller ports.
- **Late-bound firmware:** the 16 KiB `$C000-$FFFF` firmware image (motherboard ROM at `$D000-$FFFF`, built-in card pages at `$Cn00`, i.e. the Disk II boot PROM at offset `$0600`) is linked into CRAM at download time as one format-3 `firmware` ROM. No Apple ROM bytes are in git or in the package.
- **Removable media:** a built-in slot 6 Disk II controller and drive read an exact 143,360-byte DOS-order image delivered to media unit 0; the disk can be inserted, swapped and ejected while the machine runs.
- **Expansion slots:** slots 1-7 share one registered Apple II slot bus. Several slots are physical sockets: separately built cards are placed in their own reserved CRAM rectangles of the frozen shell and linked at launch, any combination per library entry.

## ABI

`fes.computer` 1.0 (tag 4) is specified by [home-computer I/O](../../../sources/mister-packages/docs/computer-io.md). The Apple II package declares required `fes.video.fixed-720p60`, `fes.keyboard.hid`, `fes.gamepad.ports`, `fes.audio.pcm-s16-stereo-48k` and `fes.media.apple2-floppy`, all 1.0, plus optional `fes.expansion.apple2-bus` 1.0. It is a format-3 package whose single ROM has id `apple2-firmware`, role `firmware`, source size 16384.

## Runtime (libmister-runtime)

Admission: `fes.computer` major 1 on `fes-gp-v1`. Minor above 0 or a `core.system` declaration is rejected. Video is required. The five interfaces above are the recognized operational set: each must be declared required when present; unknown optional interfaces are ignored; `fes.expansion.apple2-bus` 1.0 must be optional. Format 3 with a `firmware` ROM is allowed; format 3 `cartridge` and format 4 are not (this ABI has no media gate to hold them). Advertise the ABI and its interfaces (and `fes.expansion.apple2-bus` 1.0) in `capabilities.abis`.

Lifecycle: identity must match the manifest exactly (tag 4, declared = live capability set). ADV7513 audio packets follow the audio declaration as for applications. **Execution is released right after identity validation**; there is no media gate. No evdev input worker is opened for this ABI. Stop holds execution before reprogramming. Hold neutralizes keyboard and controllers in the core; the runtime also treats its own input state as neutral after Stop.

Protocol 2 additions (strict field sets, as existing ops):

- `set_keyboard_hid`: `{"protocol":2,"operation":"set_keyboard_hid","package_id":ID,"expected_generation":N,"rows":[r0,…,r8]}`. Nine integers 0..65535; row 0 bits 0..3 and row 8 bits 8..15 must be zero. Requires the active generation to be a `fes.computer` package declaring `fes.keyboard.hid`. The driver writes only rows that differ from the last acknowledged state (all nine after Start), in ascending row order.
- `set_controller`: accept `fes.computer` packages declaring `fes.gamepad.ports`; `keypad` must be 0. Uses opcode 4.
- `insert_media`: `{"protocol":2,"operation":"insert_media","path":P,"expected_package_id":ID,"expected_generation":N,"unit":U,"size":S}`. Live transfer of the exact file into unit U without holding reset: MediaInfo for U (state must not be absent, S within min..max), Begin, 512-byte Chunk/Data, Commit, then confirm Info state ready. On any failure or ambiguity: Eject U once, report the error, leave execution released. S is bounded by `FesComputerMediaMaxBytes`. The unit must belong to a declared media interface (`fes.media.apple2-floppy` → unit 0).
- `eject_media`: `{"protocol":2,"operation":"eject_media","expected_package_id":ID,"expected_generation":N,"unit":U}`.
- Status: while a `fes.computer` generation is active, report `capabilities.media_units`: `[{"unit":0,"interface":{"id":"fes.media.apple2-floppy","major":1,"minor":0},"min_bytes":143360,"max_bytes":143360,"chunk_bytes":512,"state":"empty|loading|ready"}]` from live Info.

Multi-slot composition (`fes.expansion.apple2-bus`): the composed load operations (`load_composed_core`, `load_rom_composed_core`, `load_initialized_composed_core`) accept, instead of `expansion_path` and a v1 tuple, an `expansions` array `[{"slot":S,"path":P},…]` (ascending unique slots, 1..7 entries) and a v2 `composition` tuple `{"composition_id","package_id","expansions":[{"slot":S,"expansion_id":E}],"shell_sha256","payload_sha256","payload_size"}`. Each expansion directory holds exactly `manifest.json` and `cart.rbf`; each manifest is canonical, has `slot` `fes.expansion.apple2-bus`, `map` `fes.apple2-bus.slots/1`, `slot_major` 1 and a `slot_index` equal to its request slot and in the map's socket set, and binds the exact shell. The v2 id is SHA-256 of `fes-composition-v2`, NUL, package id, NUL, then for each slot in ascending order the decimal slot, `:`, the expansion id and NUL, then the linked payload SHA-256. Status reports the same v2 object under `active_package.composition`. As today the runtime trusts the linked payload by hash and rechecks all files before programming; it does not relink. V1 single-socket behaviour for ZX81 and Coleco is unchanged.

## Go linker (misteross `expansion`)

Adds `Apple2Slot = "fes.expansion.apple2-bus"`, `Apple2Map = "fes.apple2-bus.slots/1"` and a `slot_index` manifest field (`json:"slot_index,omitempty"`, sorted between `slot` and `slot_major`; required 1..7 for multi-socket maps, forbidden otherwise). The closed map table gives each physical socket index its own half-open CRAM rectangle; rectangles are disjoint. New API:

```go
type SlotExpansion struct { Slot int `json:"slot"`; ExpansionID string `json:"expansion_id"` }
type SlotComposition struct { ID string `json:"composition_id"`; PackageID string `json:"package_id"`; Expansions []SlotExpansion `json:"expansions"`; ShellSHA256 string `json:"shell_sha256"`; PayloadSHA256 string `json:"payload_sha256"`; PayloadSize int64 `json:"payload_size"` }
func SlotSockets(slot, mapping string) []int
func SlotCompositionID(packageID string, expansions []SlotExpansion, payloadSHA256 string) (string, error)
func AdmitSlots(shell Shell, assets []Asset) error
func ComposeSlotsContext(ctx context.Context, shell Shell, assets []Asset) (SlotComposition, []byte, error)
func ComposeSlotsROM(ctx context.Context, shell Shell, assets []Asset, m ROMMap, rom []byte) (SlotComposition, []byte, []byte, error)
```

Every card is diffed against the original shell and admitted only inside its own socket rectangle; duplicate slots, a slot outside the map, or a ROM destination inside any socket are rejected. Zero assets is valid for `ComposeSlotsROM` (plain ROM link).

## FogCast

- **Library:** an entry may select one card per physical slot. Catalog key `(game_id, slot)` for `fes.expansion.apple2-bus` shells (schema migration; existing single-expansion rows unchanged). API: `GET /api/v1/library/core-entries/{game_id}/expansions` and `PUT /api/v1/library/core-entries/{game_id}/expansions/{slot}` with `{"package_id","expected_expansion_id","expansion_id"}` (empty clears). Import validates the archive's `slot_index` against the shell. A missing or incompatible selected card blocks launch before any Stop/programming, as today.
- **Launch:** link the selected firmware ROM and the selected cards (`ComposeSlotsROM`), transport the closed evidence to the agent, which relinks independently and calls the runtime v2 composed/ROM operation (or `load_rom_core`/`load_rom_library_core` with no cards). After Start, insert the entry's selected disk into unit 0 with `insert_media`.
- **Media:** `fes.media.apple2-floppy` projects as role `disk`, exact size 143,360, names `.dsk`/`.do`, transport `unit` 0. The live-media session API (`POST /api/v1/session/live-media`, `…/clear`) accepts it for `fes.computer` sessions and maps to `insert_media`/`eject_media`; `.p` behaviour for ZX81 is unchanged.
- **Keyboard:** for sessions whose active interfaces include `fes.keyboard.hid`, forward physical key state as HID usages (browser, tenfoot and on-target evdev) through `set_keyboard_hid`. No per-core character map. Esc and Backspace must reach the core in these sessions; session stop stays on the UI Stop action and the controller Select+Start chord.
- **Controllers:** `set_controller` port 0/1 for `fes.gamepad.ports`, as for applications.

## misteross

`cores/fes-apple2` machine, video, Disk II card and drive, keyboard HID adapter, speaker audio, the `fes.computer` endpoint (`cores/fes-common/rtl/fes_computer_mailbox.v`) replaying the golden exchanges, `scripts/build_fes_apple2_oss.py` (format 3, `toolchains/apple2.lock`), a shell with named socket rectangles and pinned per-slot boundary registers, a card producer that builds one card for one named slot region, and an open probe card. Open diagnostics (`cores/fes-apple2/diagnostic`) exercise everything without Apple ROMs.

## Evidence

Simulation, sealed routes, host tests and the kit are separate results. Kit acceptance needs the exact package, firmware image digest, card archives, composition id and disk digest, on a leased kit, with Stop/relaunch and a host restart. The open diagnostic firmware and synthetic disk are the first hardware control; a user-supplied Apple II+ ROM and DOS 3.3 disk are the second.
