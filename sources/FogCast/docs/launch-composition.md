# Launch composition

**Status:** The existing library session path implements household Coleco
firmware readiness and optional ZX81 RAM composition. Coleco exact-package
video/audio/input evidence is recorded in the
[playable application validation](../../../docs/validation/2026-09-21-playable-audio.md).
The [ZX81 RAM guide](../../../docs/zx81-ram-expansion.md) documents its producer,
selection API and acceptance procedure. Removable media remains future work.

**Related:**

- Working launch path: [`ARCHITECTURE.md`](ARCHITECTURE.md)
- Package inventory, media selection, and `POST /api/v1/session/launch`:
  [`core-package-library.md`](core-package-library.md)
- Sofa availability states (Checking / Missing / Needs a choice /
  Unavailable / Ready): [`rooms-experience.md`](rooms-experience.md)
- Tenfoot launch overlay and session chrome:
  [`native-tenfoot-launcher/README.md`](native-tenfoot-launcher/README.md)
- Wire projection of declared media roles:
  [`protocol/core_media.go`](../protocol/core_media.go)
- Format-2 descriptor (`ABI` and `interfaces`):
  [`corepackage`](../corepackage)
- Catalog media identity (one `blob` role today):
  [`catalog/CORE_MEDIA.md`](../catalog/CORE_MEDIA.md)
- FES package set and ZX81 first slice:
  [described FPGA core packages](../../../docs/core-packages.md),
  [FES ZX81](../../../docs/fes-zx81.md)
- Coleco reset shim versus private BIOS bring-up:
  [misteross Coleco README](../../misteross/cores/fes-coleco/README.md)
- Proven expansion linker (static CRAM overlay at load, ZX81 example):
  [FPGA cartridge expansion](../../../docs/fpga-expansion.md),
  misteross [`link_static_rbf.py`](../../misteross/scripts/link_static_rbf.py)
  and [Freeze-scaffold cartridges](../../misteross/README.md#freeze-scaffold-cartridges)

---

## Why this exists

A native launch today is mostly “activate this format-2 package, then maybe
deliver one blob.” That is enough for ROM-less Pong, BIOS-free Coleco
diagnostics, and ZX81 BASIC. It is not enough for the next honest product
steps: household BIOS, optional hardware that must exist before reset, and
disks that can change after boot.

The Coleco kit observation is the grounding case, not a one-off bug:

- Commercial Frogger carries the stock ColecoVision `55aa` cartridge header
  and needs Coleco BIOS services at `0x0000`.
- The default FES package is an open `JP 0x8000` reset shim. It does not
  interpret that header and does not provide BIOS calls.
- The same sealed package and runtime can pass the BIOS-free Graphics I /
  graphics-i control and still leave Frogger on black HDMI.

Core-present is not composition-ready. Launching a required-firmware title
without firmware must not look like **Ready**. Silent black HDMI is a
readiness failure.

FES already proved that a privately supplied 8192-byte BIOS can boot Frogger
when it is baked into a separate `fes.coleco.private-bios` package at FPGA
build time. That is a bring-up proof. It is not the household model: BIOS
bytes stay out of git, the default image stays BIOS-free, and a retail title
should compose firmware at launch rather than require a second FPGA package.

## Core principle

A launch is a **composition of named slots**, not “load this RBF.”

The package and ABI declare which slots exist and which are required. The
host fills them from household and library assets. The kit executes the
bound composition. UI **Ready** means that composition can boot on the
current setup.

This model starts with the slots we already have (core + optional primary
media) and grows without renaming the idea: firmware, expansions, then
removable media.

## Slots and roles

Slots are roles in one launch, not folders on disk and not core IDs. A
package names them in its contract. Transport, size, and bind time come
from that contract plus the runtime’s observed endpoints, never from a
display name or file extension.

| Slot | What it is | When it must be bound | First systems |
| --- | --- | --- | --- |
| **Core** | Format-2 FPGA package (sealed manifest + RBF) | Before boot | `fes.pong`, `fes.zx81`, `fes.coleco` |
| **Firmware** | BIOS / boot ROM the CPU fetches at reset | Before boot | Coleco 8 KiB BIOS (Phase 1) |
| **Expansions** | Optional carts **linked at load** onto the core’s reserved socket | At load, before programming | ZX81 16K RAM pack via the proven nextpnr CRAM linker (Phase 2) |
| **Primary media** | Cart, ROM, or the media the machine is meant to start with | Before boot, unless the package allows a media-less start | Coleco cart, Mega Drive `cartridge`, ZX81 `.p` tape |
| **Secondary / removable media** | Disk, CD, or other media that can change after the machine is running | After boot; may change mid-session | Later disk/CD systems (Phase 3) |

Primary media is already visible as today’s library `blob` / stream
selection and as native Mega Drive `cartridge`. Firmware is not a second
blob of the same role: Coleco BIOS occupies `0x0000–0x1fff` and is not the
cartridge mailbox.

Expansions are not media and are not a unique bitstream per option. The
core is a shell with a reserved socket. Each expansion is an independently
built cart. At **load**, the existing static linker
(`scripts/link_static_rbf.py`) overlays that cart’s CRAM onto the shell.
The kit programs one full-chip RBF. There is no place-and-route per
combination and no sealed `fes.zx81-16k` versus `fes.zx81-1k` package.
The 901 shell plus 900/903 carts already proved this overlay on kit; that
linker is the ZX81 expansion example. Phase 2 uses it directly for the
household 16K RAM pack. It does not invent a second “runtime device” path
and it does not wait on a bitstream-per-expansion model.

ZX81’s current first-slice package still compiles 16 KB RAM into the
sealed RBF. That is today’s factory image, not the expansion slot. The
slot is the load-time link of an optional RAM cart onto a socketed core.

## What is true now

Present tense, current FES `sources/FogCast` and the ordered package set:

- Library launch is `POST /api/v1/session/launch` with a `game_id`. The
  host resolves one installed package ID and at most one selected media
  object (`role` + digest). See
  [`core-package-library.md`](core-package-library.md).
- `protocol.DeclaredCoreMediaCapabilities` projects supported **media**
  transports from exact ABI and interface versions. The implemented media
  role remains `blob`. `DeclaredFirmwareCapabilities` projects the optional
  Coleco firmware slot from exact `fes.firmware.blob` 1.0. Unknown versions
  expose no roles. Optional declarations still need active runtime support
  at launch.
- `fes.application` 1.0 packages that require blob media already reject a
  missing selection **before** package activation. Legacy
  `fes.simple-computer` packages may still launch package-only (ZX81 BASIC,
  Coleco with no cart).
- Coleco’s default reset image is the open shim inside the RBF. Private
  BIOS is a misteross producer input (`--bios PATH`) that exports
  `fes.coleco.private-bios` to an ignored private store. It is not loaded
  through `load_media` / `load_media_stream`.
- Catalog eligibility (`hostclient.Game.LaunchEligible`) requires available
  state, explicit `launchable` and `root_online`, and composition readiness.
  A title with `firmware_required` is ineligible until household firmware is
  filled **and** the selected package declares `fes.firmware.blob` 1.0.
  Rooms map that block to **Unavailable** (“Coleco BIOS required…”) with
  action “Import Coleco BIOS.” Graphics I / graphics-i omit the title-level
  flag and stay Ready on the same Coleco package.
- Household firmware is one content-addressed `firmware` slot in catalog
  schema 9 (`core_firmware` pointing at `core_media`). Import is ordinary
  core-media; `GET`/`PUT /api/v1/library/firmware` and
  `fogcast core-firmware-select` bind or clear the slot. Tenfoot/rooms Confirm
  on **Unavailable** (“Import Coleco BIOS.”) opens a pad-friendly file picker
  that posts the same APIs: `POST /api/v1/core-media` then
  `PUT /api/v1/library/firmware`. No BIOS bytes are stored in git. The factory
  image stays BIOS-free.
- Library launch still posts `game_id` to `POST /api/v1/session/launch`.
  When a title requires firmware, the host admits the household object
  **before** package activation, programs the core, binds firmware
  (`load_firmware`, reset held), then binds cartridge media (which releases).
  Missing required firmware never reports `state: active`.
- Coleco’s sealed `fes.application` 1.0 package may declare optional
  `fes.firmware.blob` 1.0. The producer enables the mailbox firmware
  overlay (`ENABLE_FIRMWARE=1`) for synthesis only. Sims of `top` keep the
  default off so BIOS-free diagnostics stay on the open `JP 0x8000` shim.
  The private `--bios` producer remains bring-up only.
- Agent health is not session readiness. An idle menu is not launch
  readiness. Those existing gates stay.
- The expansion linker already exists as a development compose:
  `link_static_rbf.py overlay` copies a cart CRAM rectangle onto a frozen
  shell. It is kit-proven on the 901/900/903 path and does not yet seal
  `fes.zx81` or appear on `session/launch`. Phase 2 is that load-time
  link on the library path, not a new overlay algorithm.

Proposed work below does not rewrite those paths. It adds slot fill and
composition readiness in front of the same session launch.

## Lifecycle ordering

Default order, unless a package declares a stricter recipe:

1. Resolve the core package and its slot contract.
2. Fill every **required** slot from household and library assets. Fail
   closed before any FPGA mutation.
3. **Link** selected **expansions** onto the core shell (static CRAM
   overlay at load). Skip this step when no expansion is selected.
4. Admit the composition on the target (existing kit lease, identity, and
   lifecycle exclusion).
5. Program the linked bitstream (one full-chip RBF).
6. Bind **firmware**.
7. Bind **primary media** (today’s `load_media` / `load_media_stream` /
   native `cartridge`, held in reset where the core already does that).
8. Release reset / boot.
9. After boot, **secondary / removable media** may change without
   reprogramming the FPGA.

Link-at-load versus bind-after-boot is the important split:

- Expansions are part of the bitstream the kit programs. The 16K RAM pack
  is linked before programming, the same way a real pack is plugged in
  before power-on. Linking after BASIC has started is a different product;
  this model does not do that.
- Firmware is bytes the CPU fetches at reset, not a CRAM cart. It still
  binds before boot, after the FPGA is programmed.
- Coleco cartridge delivery already holds CPU/VDP reset through the
  mailbox commit. Firmware belongs on that same side of boot: the CPU must
  not fetch `0x0000` until the BIOS slot is filled.
- A disk swap is allowed to happen later because the running machine
  expects removable media. Phase 3 is that later change, not a second
  launch.

Development `core-load` and raw RBF loads stay volatile and outside
library composition. They do not become a back door that skips required
slots for a library title.

## Ownership

| Owner | Responsibility |
| --- | --- |
| mister-packages + sealed format-2 descriptor | Declare ABI and versioned interfaces: which slots exist, required versus optional, size and transport. Unknown versions fail closed. |
| FogCast host | Compose the launch from household/install assets and the library, including which expansion carts to link at load. Refuse **Ready** and refuse launch when a required slot is empty. Store firmware and media as ordinary household objects (content-addressed, like today’s core-media). **No private BIOS or ROM bytes in git.** |
| FogCast target agent | Cache, transfer, and session-coordinate the already-composed facts. It does not invent slot semantics, run nextpnr, or program the FPGA. |
| libmister-runtime | Execute bind order, programming of the linked RBF, firmware/media delivery, reset release, later media change, and return to idle. It remains the compatibility authority through the negotiated ABI registry. |
| misteross | Build the core shell, expansion carts, and the static linker. The private `--bios` producer remains a bring-up proof, not the household firmware path. Place-and-route stays on each shell and each cart once; load only overlays. |
| FES | Select compatible module revisions, keep the default image BIOS-free, and record evidence. |

Household firmware is an install/library asset, not an image feature and
not an FPGA rebuild. The default `fes.coleco` package stays the open shim.
A retail Coleco title becomes Ready only when that title’s composition
includes a household BIOS object the runtime can bind before boot.

The host never infers a required BIOS from a display name, a `.col`
extension, or a raw RBF. It infers slots from the admitted descriptor.
Cartridge header inspection (for example noticing `55aa`) may later help
explain *why* a title needs firmware; it does not replace the contract.

## Readiness and UI honesty

Rooms already distinguish **Unavailable** (“matched, but a requirement
blocks play”) from **Ready**. Composition uses that same split.

| Situation | State | Confirm |
| --- | --- | --- |
| Core package missing | Missing / Unavailable | Explain install; do not launch |
| Core present, required firmware missing | **Unavailable** | “Coleco BIOS required… Import Coleco BIOS.” Confirm opens the household file picker. Not Ready. |
| Core present, optional expansion unset | Ready, with the unbound expansion omitted | Launch the 1K (or otherwise default) composition; Details can offer the 16K pack |
| Required primary media missing | Unavailable | Same as today’s application-blob rejection, surfaced in the panel |
| All required slots filled and target gates pass | Ready | Play |

Rules:

- **Core-present ≠ composition-ready.** An installed `fes.coleco` package
  is not enough to offer Play on Frogger.
- Catalog `launchable` remains a platform/target gate. Composition
  readiness is an additional predicate on the same `game_id`. Missing
  flags still do not grant eligibility.
- Confirm never silently no-ops. Unavailable shows the missing slot and a
  specific next action. Ready launches through the existing session API.
- A successful package activation with a black HDMI picture is not a
  successful play session. Phase 1 must fail closed *before* boot when
  required firmware is absent, rather than reporting `state: active` and
  hoping the sofa overlay looks busy.
- Tenfoot chrome that already maps `execution=fpga_development` to
  DIAGNOSTIC stays a session-label concern. Composition readiness is
  decided before launch, not by relabelling a black screen.

Coleco control for this honesty bar: Graphics I / graphics-i on the
BIOS-free package may stay Ready (open-cartridge convention, no BIOS
slot required). Frogger on that same package is Unavailable until the
firmware slot is filled. That is the product difference the current
single-blob model cannot express.

## Phased roadmap

Same model in every phase. Each phase adds fill/bind rules for slots that
already have names. Do not invent a parallel “Coleco-only” launch path
and then try to generalize it.

| Phase | Delivers | Does not deliver |
| --- | --- | --- |
| **0 — this document** | Shared vocabulary, ownership, bind-before-boot vs later change, Ready rule | Code, ABI changes, kit time |
| **1 — Coleco firmware slot + readiness** | Descriptor-declared firmware slot; household BIOS import; Unavailable when required firmware is missing; runtime bind before reset release | Permanent BIOS in the factory image; git-tracked BIOS; rewriting the cartridge mailbox into a BIOS loader for every core |
| **2 — expansion slots** | Load-time CRAM link of optional carts, starting with ZX81 16K RAM on the proven nextpnr linker | A unique place-and-route / sealed bitstream per RAM size or per expansion combination |
| **3 — removable media / media-change** | Secondary slot and mid-session change without reprogramming the core | A claim that every core already supports disk swap |

Phase 1 may keep the private `--bios` producer as a diagnostic compare. It
must not become the way a sofa title gets a BIOS.

Phase 2 skips bitstream-per-expansion and goes directly to the linking
system already proven on the ZX81 freeze-scaffold example (`link_static_rbf`
overlay of an independent cart onto a reserved socket). The household 16K
RAM pack is that cart on the library launch path. Do not add a parallel
runtime-device protocol.

Phase 3 waits until a core actually has removable media. Do not overload
today’s single `blob` role into a fake disk swap.

## Non-goals

This Phase 0 PR, and the model it sets, explicitly do **not**:

- install a Coleco BIOS in the factory image or in git
- add kit HIL, HDMI captures, or hardware acceptance
- rewrite every core onto a new ABI
- build a unique bitstream per expansion (no `fes.zx81-16k` P&R package)
- replace `POST /api/v1/session/launch` with a new public compose API in
  Phase 1 (the host composes internally; the sofa still posts a `game_id`)
- infer persistence, firmware, or expansions from a display name, package
  path, or raw RBF
- claim retail Coleco compatibility from BIOS bind alone

## Current integration

Firmware and expansion selection use the normal library launch path. The
host and target independently link selected expansion assets; the runtime
programs the checked payload while retaining the original shell identity.
No compiler runs during launch. Physical acceptance applies only to the
artifacts in the linked validation records, not every future package or image.
The sofa/tenfoot household BIOS picker is implemented, while exact-kit
evidence remains tied to named validation records. Physical acceptance applies
only to those artifacts, not every future package or image.

The earlier phase descriptions above retain the rationale and sequence of
the design. They do not supersede the current operator guides.

FES parent merges stay Deano’s.
