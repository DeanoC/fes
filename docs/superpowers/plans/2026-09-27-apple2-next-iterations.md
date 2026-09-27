# Apple II: next iterations and open decisions

Date: 2026-09-27. Follows the pathfinder
([design](../specs/2026-09-26-apple2-pathfinder-design.md),
[seal](../../validation/2026-09-26-apple2-pathfinder-seal.md),
[kit diagnostic](../../validation/2026-09-27-apple2-kit-diagnostic.md)).
This is a backlog, not an instruction list: each item records the current
anchors, a proposed approach and the decisions still needed. Prefer general
mechanisms that serve other systems and cards over Apple-only special cases.

Suggested order: 1 → 2 → 3, then 4 before 5 (both touch media units), with 6,
7 and 8 as independent tracks. Estimates are rough.

## 1. Cart-merge robustness after FES #250

Done in this iteration: nextpnr PR DeanoC/nextpnr#87 maps every traced cart
clock onto the socket clock and rejects cart inputs on undriven nets.
`toolchains/apple2.lock` pins it, and the Apple II probe card went back to a
plain inferred RAM. The shell and cards were resealed and passed a
[kit re-check](../../validation/2026-09-27-apple2-cart-clock-recheck.md).

- Other locks (`toolchain.lock`, `zx81-expansion.lock`, `coleco-sgm.lock`,
  `coleco-expansion.lock`) still pin `a93fe013`. Today's ZX81 and SGM carts are
  unaffected (explicit TDP RAM, or RAM in the shell). Bumping a lock reseals
  its shells and invalidates their kit acceptance, including factory
  `fes.pong`, `fes.zx81` and `fes.coleco`.
  **Decision:** bump them together with the next nextpnr change they need, or
  proactively in one factory reseal?
- `scripts/build_apple2_slot_card.py` also checks card clock pins after
  routing (`validate_cart_clocks`). Other card producers rely on nextpnr alone
  once their lock moves.
  **Decision:** move the check to a shared card-producer helper? Editing
  `fes_build_common.py` changes factory shell inputs, so it would need a new
  module.
- `mistral/tests/fes_slot_clock.py` fails in its routed-shell phase at
  `a93fe013` (a `router2` wire-reservation error), before its later cases run.
  Only `tests/fes_cram.cc` runs in nextpnr CI; the Python FES tests run by hand.
  **Decision:** fix that test, and wire the FES Python tests into CI?

## 2. Image acceptance of the fes.computer runtime and agent

The kit's installed image predates `fes.computer`. The kit diagnostic
bind-mounted diagnostic `make dev` builds and removed them afterwards.

- Path: `make check`, `make build`, `make verify`,
  `make release RELEASE_VERSION=…`, then `fes-update --action update` under the
  kit lease, a reboot and a boot/image identity check. Then
  `make target-acceptance` for the factory cores and an Apple II package run
  ([appliance releases](../../appliance-releases.md)).
- Turn the kit harness (isolated host, HDMI text decoder, keyboard/disk
  sequence; see the kit record) into an in-repo acceptance script.
- The normal FogCast host must be upgraded as well. Its catalog migrates to
  schema 13, which older host binaries cannot read.
  **Decisions:** when to change the installed image, and whether the operator's
  library catalog may migrate.

## 3. Tenfoot disk picker (FogCast, small)

The host already routes `.dsk`/`.do` live media
(`internal/hostapi/live_media.go`, `fogcast/live_media.go`). The tenfoot client
blocks disks in four places:

- `protocol.LiveMediaCapable` accepts only `fes.simple-computer`.
- `hostclient.LiveMediaBinding` requires that check.
- `hostclient.ReplaceLiveMedia` accepts only `.p` names.
- `hostclient.SessionCorePackage` has no `media_units`.

Proposed: give the `ui/tenfoot/tape.go` flow a media kind (tape or disk) that
selects the name/size admission, labels and eject text. Base disk capability
on the observed floppy unit, not the ABI. Roughly 150–250 lines plus tests.

**Decision:** the trigger. The `/` shortcut is also an Apple II key; a
controller button or a menu entry avoids the clash.

## 4. Second drive (medium)

Current state:

- The Disk II card latches drive select, but the drive only uses it to stop,
  so drive 2 is empty.
- One 143,360-byte store uses 140 of the core's 221/553 M10K; a second brings
  the core to about 361.
- Media addressing (`MEDIA_AW=18`) cannot reach two images.
- Unit 0 is hard-coded in `fes_computer_mailbox.v`, the runtime's per-interface
  unit table, and FogCast: `protocol.Apple2FloppyUnit`, the validator,
  `diskUnitBinding`, the live-media request without a unit field, the CLI, and
  one media selection per catalog entry.

Proposed:

- **Core:** per-drive state and a second store.
- **ABI:** unit 1, as `fes.media.apple2-floppy` 1.1 or a unit count, with golden
  fixtures regenerated.
- **Runtime:** N units per interface.
- **FogCast:** a unit in live media and the CLI, and a catalog migration to
  per-unit disk selections.
- A shell reseal and card rebuild.

**Decisions:** the minor-version versus new-interface rule for adding units;
whether a title stores one disk per drive; and the UI for choosing the drive.

## 5. Disk writes (large)

Current state:

- The card stores Q6/Q7 but has no write latch or shifter.
- The drive is write-protected and has no nibble decoder.
- `fes.computer` 1.0 carries data only from host to core.
- FogCast core media is immutable and content-addressed.

Proposed:

- **Card:** a write latch and shifter.
- **Drive:** decode the address and data fields and undo 6-and-2, commit a
  sector only on a good checksum, and write through store port B.
- **ABI:** a new capability with a per-unit dirty indication and media-read
  opcodes (offset/length header, ordinal data words, CRC). Reading back a whole
  disk takes the same 71,680 exchanges as inserting one.
- **Runtime:** a `read_media` operation.
- **FogCast:** import the result as a new media ID and repoint the title (or
  keep a per-title writable copy).

The same core-to-host path serves cassette save (6) and possibly other
systems' saves. It should be designed as a generic media write-back, not an
Apple-specific opcode.

**Decisions:**
- When to write back: on eject, on Stop, periodically, or on explicit save.
- Versioning of the originals versus modified images in the library.
- Whether v1 refuses whole-track writes (DOS INIT).
- Crash and power-loss semantics.
- Whether the 1.0 read-only rule becomes a 1.1 interface or a new one.

## 6. Cassette tape (medium–large)

The machine already decodes `$C020` (out) and `$C060` bit 7 (in); `top.v` ties
the input low and leaves the output open. ZX81 tape is a ROM-trap blob on
another ABI and does not carry over. A new `fes.media.apple2-cassette` unit
(capability bit 5) fits the generic media-unit transport.

Format options:
1. Host-converted half-cycle durations: covers any loader, but about 32× the
   payload.
2. Byte records (length, then program) that the core turns into the 770 Hz
   header, sync bit and 1/2 kHz bits, like the disk nibblizer.
3. A ROM trap: fragile, because the firmware is supplied by the user.

**Decisions:** the format (option 2 is proposed; option 1 would add turbo
loaders); which file types to import (WAV, `.ct2`, raw binary with a load
address); tape transport controls (play, stop, rewind) in the API and UI;
and whether save (via item 5) is in scope.

## 7. General multi-ROM package format (medium–large, four modules)

What already handles N ROMs:

- ROM map v1: one flat source, at most 256 blocks / 256 KiB.
- The misteross `rom_map.py`.
- `core_package.py` validation and encoding loops.
- The Go `LinkROM`/`ComposeROM`/`ComposeSlotsROM`.
- The runtime's `CoreROMLinks.sources` vector.

Format 4 is hard-coded as exactly two ROMs (firmware, then cartridge) in:

- the schema (`core-bundle-v4.json`);
- `core_package.py` and `export_core_package.py`;
- the runtime (`core_package.cpp`, `protocol.cpp`, `hardware.cpp`), which
  rejects format 4 for `fes.computer` outright;
- FogCast (`corepackage/package.go`, `rom_input_v2.go` with fixed BIOS and
  cartridge members, `fogcast/core_roms.go` with Coleco sizes, `mesh_ensure.go`,
  and `core_entry_roms` keyed by title alone).

Proposed: a named-list format 5, with 1..K entries that have unique IDs,
contiguous offsets and roles (firmware, cartridge, card, charset). The runtime
advertises `rom_linking=2`, loops over sources, and applies per-role lifecycle
rules. FogCast gets a ROM input v3 carrying a source list plus slot cards
through `ComposeSlotsROM`, a catalog keyed by (title, ROM ID) with a scope for
shared household firmware, an API at `…/roms/{rom_id}`, and no core-specific
sizes. Formats 3 and 4 remain accepted. First users: splitting the Apple II
motherboard ROM from the Disk II boot PROM, and future cores with character or
BASIC ROMs.

**Decisions:**
- The ROM scope model (per title versus household).
- Whether sub-KiB ROMs (the 256-byte P5 PROM) are padded or the block size
  changes.
- Whether card ROMs inside sockets become late-bindable. `ComposeSlotsROM`
  refuses socket destinations today; this would need per-socket ROM maps in
  card archives.
- Whether a linkable character ROM justifies new blank lanes and a reseal.

## 8. Mockingboard card (large, resource-bound)

Current state:

- **Socket size:** 69 usable LABs (1,380 LUTs and FFs, 18 M10K in column 26).
- **Existing AY:** `sgm_ay` (`cores/fes-coleco/expansions/sgm_ay.v`,
  GPL-2.0-or-later). The single-AY SGM card needed about 55–57 LABs; placement
  was limited by LAB input bandwidth rather than LUT count.
- **VIA:** no 6522 exists in the tree.
- **Slot IRQ:** wired to the CPU as a level; no card or simulation exercises it
  yet.
- **Slot audio:** one mono 16-bit field.

Proposed:

- A card built for the socket rather than a literal port: clock the AY from
  the bus strobe (about 1.02 MHz, as on the original card) instead of the
  phase accumulator.
- Time-share one tone/noise/envelope engine across six channels, with
  registers in M10K and a shared volume table.
- A minimal 6522 (T1 with IRQ, ORA/ORB, DDR, IFR/IER).
- Scale the mix so it cannot clip the 16-bit field.
- An IRQ test in simulation.
- Fallback: a one-AY, one-VIA "Sound I" card.

**Decisions:**
- Whether mono is acceptable for v1. Stereo means a bus revision (response
  width, pinned boundary flip-flops, a new socket layout major version, a
  shell reseal) or time-multiplexing left/right using the reserved request
  bit 31.
- Where the 6522 comes from (licence).
- Whether the bus revision is worth doing for other stereo cards.
