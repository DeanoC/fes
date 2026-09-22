# ZX81 tape media (design lock)

**Status:** Deano-confirmed design lock (2026-09-22). First mid-session
media support for ZX81: select and load a tape while the core is already
running. All listed product picks are locked. This document is not an
implementation claim and does not invent RTL. Deano owns FES parent merge.
Do not merge without Deano. Bob coordinates; this PR is design lock only.

**Audience:** FES parent, FogCast (host/rooms/tenfoot/agent),
libmister-runtime media lifecycle, mister-packages ABI, and later misteross
tape-loader work. Read this before adding a sofa tape picker, a mid-session
`load_media` path, or conflating tape with the launch-time machine-ROM splice.

**Related:**

- Launch composition (slots; Phase 3 removable media):
  [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md)
- FES ZX81 first slice (today’s `.p` mailbox contract):
  [`docs/fes-zx81.md`](fes-zx81.md)
- ZX81 expansion bus (launch-time carts; not tape):
  [`docs/zx81-expansion-bus.md`](zx81-expansion-bus.md)
- ZX81 described-core design (`.p` cap; no analog tape):
  [`docs/superpowers/specs/2026-09-10-fes-zx81-design.md`](superpowers/specs/2026-09-10-fes-zx81-design.md)
- FogCast architecture (ROM splice last; library `.p` at launch):
  [`sources/FogCast/docs/ARCHITECTURE.md`](../sources/FogCast/docs/ARCHITECTURE.md)
- Blob / stream capacity and transport:
  [`docs/core-media-evolution.md`](core-media-evolution.md),
  [`sources/mister-packages/docs/media-stream.md`](../sources/mister-packages/docs/media-stream.md)
- Idle MENU → rooms (design-lock pattern; sofa yields during play):
  [`docs/idle-menu-rooms.md`](idle-menu-rooms.md),
  [`docs/idle-menu-rooms-phase2.md`](idle-menu-rooms-phase2.md)
- Rooms / tenfoot chrome:
  [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md),
  [`sources/FogCast/docs/native-tenfoot-launcher/README.md`](../sources/FogCast/docs/native-tenfoot-launcher/README.md)
- Coleco BIOS picker (pre-launch household import pattern to mirror for UX only):
  [`sources/FogCast/docs/core-package-library.md`](../sources/FogCast/docs/core-package-library.md)

---

## Why this exists

Deano’s product ask (2026-09-22): start designing the first media support for
ZX81 — the ability to **select and load tapes whilst the core is running**.

FES tip already has three related but distinct paths:

1. **Launch-time machine ROM splice** (post-#112): sealed `fes.zx81` keeps
   column-5 ROM empty; host splices 8 KiB BASIC after package identity;
   cart compose first, ROM splice last. That mutates the bitstream before
   programming. It is not tape.
2. **Optional primary `.p` at launch**: library or development `load_media`
   fills the GP mailbox before or as part of session start; BASIC
   `LOAD ""` then consumes it.
3. **Empty `LOAD ""`**: with no committed blob, the RTL patch at `$0347`
   returns `0/0` instead of the original cassette waiter.

What is missing is an honest **mid-session** path: the session stays
`active`, the FPGA stays programmed, RAM / expansion / machine ROM stay as
they are, and the user can arm a different tape and load it. That is the
first concrete ZX81 case of launch-composition **Phase 3** (secondary /
removable media after boot). Do not fake it with Stop → relaunch, and do
not overload today’s hold-reset launch `load_media` into a silent reboot.

## What is true now

Present tense, FES tip `4dede116` (includes #111 packages-only cleanup and
#112 ZX81 machine-ROM splice). Paths below exist in this tree. No TZX /
`.tzx` vocabulary appears under `sources/` or `docs/`.

### Product path

Described `fes-gp-v1` packages are the sole FPGA product path. There is no
Main_MiSTer / FIFO / CORENAME / protocol-1 / raw-core catalog for household
play. The agent is always native / `fpga_native`. Work lives in the FES
monorepo (`sources/FogCast`, `sources/misteross`,
`sources/libmister-runtime`, mister-packages). Standalone `DeanoC/misteross`
is retired for day-to-day.

### ZX81 package and expansion

| Item | Today |
| --- | --- |
| Package | `fes.zx81` (`fes.simple-computer` 1.0), profile `fes-gp-v1` |
| Interfaces | `fes.keyboard`, `fes.media.blob`, `fes.video.fixed-720p60`; optional `fes.expansion.zx81-bus` |
| Vacant shell | 1 KiB mirrored internal RAM when the expansion edge is vacant |
| Expansion | Independent bus carts linked at load (`link_static_rbf.py`); no synthesis at launch |
| Machine ROM | Sealed shell ROM M10Ks empty; launch splices `zx8x.hex` via `link_static_rbf.py init --machine` |
| Persistence | None; library launches are volatile |
| Tape contract (docs) | Runtime `load_media` of a `.p`; empty `LOAD ""` reports `0/0` ([`fes-zx81.md`](fes-zx81.md)) |

### Mailbox tape path (surveyed RTL / ABI)

- ABI `fes.media.blob` 1.0: opcodes begin / data / commit (`0x04`–`0x06`);
  length **1..16384** bytes ([`fes_simple_computer.yaml`](../sources/mister-packages/packages/abi/fes_simple_computer.yaml);
  design: [`2026-09-10-fes-zx81-design.md`](superpowers/specs/2026-09-10-fes-zx81-design.md)).
- misteross `fes_computer_gp.v`: 16 KiB media M10K; commit sets `media_ready`
  and size; hold-reset **aborts an open** media transfer (`media_open <= 0`)
  and clears keyboard; it does not by itself clear a committed blob’s
  interpretation beyond the next begin.
- misteross `zx81_machine.sv`: always intercepts LOAD at `$0347`. A committed
  mailbox blob is copied into RAM; with no blob, SCF so BASIC returns `0/0`
  instead of the original cassette waiter (black screen until BREAK). Raw
  `.p` bytes only — **no audio / TZX decode**.
- Package manifest declares **`fes.media.blob` only** (not
  `fes.media.blob-stream`). Coleco/SMS stream is a different contract.

### Host / runtime delivery today

| Path | Behavior |
| --- | --- |
| Library launch | Optional `MediaRole=blob` + `MediaID` after core up → staged `load_media` / `load_media_stream` ([`core_packages.go`](../sources/FogCast/fogcast/core_packages.go)) |
| Development | `POST` development media; CLI `fogcast core-media PATH`; protocol-2 `load_media` |
| Runtime gate | `Runtime::LoadComputerMedia` admits only `State::running_development` ([`runtime.cpp`](../sources/libmister-runtime/src/runtime.cpp)) |
| Reset policy | `FesGpCoreDriver::LoadMedia` **Quiesce (hold reset) → begin/data/commit → release** ([`fes_gp.cpp`](../sources/libmister-runtime/src/native/fes_gp.cpp)). Comment: computer consumes committed media while execution reset is held. That is **launch bind policy**, not a second ear-playback ABI. |
| Library selection | `core-media-select` affects the **next** launch, not the active session ([`ARCHITECTURE.md`](../sources/FogCast/docs/ARCHITECTURE.md)) |
| Sofa | Catalog/grid for `game_id` is FogCast UI; no mid-session tape picker. Coleco BIOS picker is **pre-Play** Unavailable fix, not in-session media swap. |

### Contrast table (do not conflate)

| Artifact | Timing | Mutates programmed bitstream? | Session stays `active`? |
| --- | --- | --- | --- |
| 8 KiB BASIC ROM splice | Before program | Yes (M10K INIT) | N/A (pre-boot) |
| Expansion cart overlay | Before program | Yes (CRAM link) | N/A (pre-boot) |
| Primary `.p` at launch | After program, before/at boot bind | No (mailbox RAM) | Becomes active after release |
| **Mid-session tape (this lock)** | After boot, while playing | No (mailbox RAM) | **Yes** |
| Stop → relaunch | Tears down session | Reprograms | No |

---

## Decisions

Locked 2026-09-22. Deano confirmed every product pick listed here
(including who types `LOAD ""` and former open picks 2–6). Implementation
PRs do not reopen them without changing this document. No product picks
remain open.

### 1. User journey

While a ZX81 library (or equivalent native package) session is **`active`**
— sitting in BASIC, or after a cart-composed launch that still exposes
BASIC/LOAD — the user selects a tape and arms it **without** Stop and
**without** reprogramming the FPGA.

Concrete living-room loop:

1. User is already in an active ZX81 session (sofa tenfoot yielded; kit HDMI
   shows the core).
2. User opens the in-session system/chrome action that **arms a tape** (sofa
   rooms/tenfoot overlay — Decision 3). Chrome arms the deck / mailbox; it
   does not itself type BASIC `LOAD ""`.
3. User picks a household `.p` (already imported, or import-then-pick).
4. Host/agent/runtime deliver the bytes into the existing mailbox and mark
   the blob committed (`media_ready`). Arming is allowed anytime the session
   is `active` and the tape-loader is not already copying (Decision 8).
5. When the user wants BASIC load, **they type `LOAD ""` on the ZX81
   keyboard** after the overlay reports armed. No host matrix inject for v1
   (Decision 8). Arming is not LOAD-only: the same mid-session media path may
   later feed an already-running program that loads data without BASIC
   `LOAD ""`, and may later support SAVE — those are adjacent/future uses of
   the armed deck, not a v1 SAVE product claim (Decision 6).
6. For the BASIC `LOAD ""` case, the existing `$0347` loader patch copies
   mailbox bytes into RAM. Session, expansion composition, and spliced
   machine ROM remain. Cart-composed sessions use this same tape path; the
   expansion is unchanged (Decision 8).

**Not this journey:**

- Relaunch with a different primary media selection.
- Re-running the machine-ROM splice or expansion link.
- Using today’s hold-reset `LoadMedia` and calling the resulting soft reboot
  “still running.”

### 2. Artifact class

A **tape** in FES v1 is a **household library media object**: content-addressed
bytes in the existing core-media store, role interpreted as ZX81 `.p` tape
image for the active `fes.simple-computer` / `fes.media.blob` contract.

| Rule | v1 |
| --- | --- |
| File format | **`.p` / `.P` only** (ZX81 memory-image tape). Reject other extensions at admission once enforcement lands. |
| Size | **1..16384** bytes (existing ABI max). |
| Storage | Ordinary `POST /api/v1/core-media` object (digest id). Not an FPGA package. Not sealed into `fes.zx81`. |
| Free path | Absolute local path remains valid for **development** upload; household product path is library media id. |
| Not v1 | `.tzx` / TZX, `.tap`, `.o`, `.col`, `.chr`, analog cassette wav, multi-file albums. |

Primary launch-time `.p` and mid-session tape share the **same byte class**.
They differ in **bind time** (launch primary vs post-boot secondary), not in
file format.

### 3. Where selection lives

**v1 selection surface: sofa rooms / tenfoot** on the FogCast host (same
renderer that already owns Play, Stop, and the Coleco BIOS import overlay).

| Surface | v1 |
| --- | --- |
| Sofa rooms / tenfoot | **Yes** — in-session chrome that **arms a tape** while session is `active` and the package is ZX81 / `fes.media.blob`. Pad-friendly picker can mirror the Coleco BIOS import overlay’s file-browse and Confirm patterns, but it binds the **active** session, not the next launch. Chrome reports armed / ready; it does not assume every consumer is BASIC `LOAD ""`. |
| Kit `fogcast-kit` grid | **No** — do not grow a second offline catalog or tape browser on kit HDMI ([idle lock](idle-menu-rooms.md)). |
| Host API / CLI | **Required underneath** the sofa path. Once slice 2 exists, `fogcast` session media change is an **operator/diagnostics** path. Sofa remains the living-room surface (Decision 8). |
| Kit-as-host | Same host/tenfoot process ⇒ same overlay. No distinct kit picker product. |

During play, tenfoot stays parked for GPU cover work; the arm-tape overlay
is FES-owned chrome over the active session, analogous to system-menu /
Stop ownership, not a room Lua feature.

### 4. Transport / ABI

**Reuse `fes.media.blob` 1.0** begin / data / commit into the existing ZX81
mailbox RAM. Do **not** invent ear-bit / ADC playback for v1. Do **not**
require `fes.media.blob-stream` for ZX81 while the sealed package declares
blob only and the cap stays 16 KiB.

| Layer | Owns |
| --- | --- |
| FogCast host | Asset pick, size/extension admission, stage to agent, session-scoped “change tape” request. Does **not** drive Z80 baud or pixel timing. |
| FogCast agent | Transfer staged bytes; call runtime; report success/failure on the active generation. |
| libmister-runtime | Mailbox protocol on the live package generation. **Mid-session path must not Quiesce/hold execution reset** (that soft-reboots the machine and wipes the “core already running” product). Launch-time hold-reset bind may remain for primary media. |
| misteross FPGA | Mailbox storage; `$0347` LOAD patch; consumption pacing when `tapeloader` runs. Empty blob ⇒ `0/0`. |
| mister-packages | Keep blob size/opcodes authoritative. Any new “mid-session media without hold-reset” semantic is an ABI/runtime contract clarification or additive opcode — decide in the ABI slice, not by silent driver divergence. |

**Timing ownership (locked):**

- **Host** owns when bytes are staged and when commit is requested. Host does
  **not** type `LOAD ""` for v1.
- **Runtime** owns the mailbox exchange and abort/error surfaces.
- **FPGA** owns consumption pacing when the tape-loader runs (today: BASIC
  `LOAD` at `$0347`). Later non-LOAD consumers of the same armed mailbox are
  adjacent/future, not a v1 SAVE claim.
- There is **no** host-side cassette clock and **no** ear-bit playback in v1.

Surveyed fact for implementers: RTL media opcodes do not require
`exec_reset` held to accept begin/data/commit; today’s reset hold is
**runtime launch policy**. Mid-session must use a path that leaves
execution released (or explicitly documents a different product). Do not
claim a new RTL tape machine that is not in the tree.

### 5. Session lifecycle

When a new tape is selected mid-session:

| State | Behavior |
| --- | --- |
| FPGA bitstream | Unchanged (no reprogram, no ROM splice, no cart re-link). |
| Machine ROM | Unchanged (already spliced at launch). |
| Expansion cart | Unchanged (launch composition retained). |
| ZX81 RAM / BASIC program | **Preserved** across the mailbox fill. |
| Mailbox blob | **Replaced** by the new begin/data/commit (or cleared on eject). |
| Ongoing `LOAD` | **Reject** a replace with busy while the tape-loader is copying. Do not abort the Z80 loader mid-copy. Surface busy/retry. Do not tear down the session (Decision 8). |
| Eject / cancel | Clear readiness so the next `LOAD ""` yields `0/0`. Cancel of an in-flight transfer leaves no partial `media_ready`. |
| Errors | Fail closed on the host API and sofa overlay (size, type, transfer, generation mismatch). Do not report `active` success with a silent empty mailbox. |
| Stop | Existing package Stop → idle; tape selection does not survive as session state. Next launch uses ordinary composition. |

Selecting a tape is **not** a new library launch and **not** a persistence
bind. Development loads stay volatile; library context stays explicit.

### 6. Non-goals for v1

- SAVE to tape / write-back from ZX81 to household media as a **v1 product
  deliverable** (the mid-session arm path may later support SAVE and
  in-program loads that are not BASIC `LOAD ""`; that future use must not be
  read as shipping SAVE in this slice — Decision 8)
- Multi-tape changers, playlists, or auto-next
- Analog ear-in, microphone, or real cassette audio
- TZX / TAP / pulse-level playback
- Attract / screensaver integration
- Host matrix inject of `LOAD ""` (user types it when using BASIC load)
- Conflating tape with machine-ROM splice or expansion carts
- Kit HDMI catalog tape browser
- Main_MiSTer ioctl tape, FIFO, or protocol-1 paths
- Claiming every FES core supports disk-swap because ZX81 got tapes
- Overloading launch-time hold-reset `LoadMedia` as the mid-session product
- Factory image changes, kit HIL, or FPGA recipe changes in this docs PR

### 7. Relation to launch composition Phase 3

This lock **is** the first ZX81-shaped Phase 3: secondary / removable media
that may change after boot without reprogramming
([`launch-composition.md`](../sources/FogCast/docs/launch-composition.md)).

Keep the names distinct:

- **Primary `.p`** — optional media the machine may start with (today’s
  launch bind). Mid-session tape does not remove that optional launch-time
  bind (Decision 8).
- **Mid-session tape** — the same `.p` class, bound after boot while
  `active`.

Do not invent a parallel “ZX81-only” launch API. Extend session coordination
so an active ZX81 generation can accept a media change. Sofa may still key
off `game_id` / session status; composition facts stay host-internal.

### 8. Confirmed session rules (Deano, 2026-09-22)

All former open product picks. Locked.

- **Who types `LOAD ""`.** The user types it on the ZX81 keyboard after the
  overlay reports armed. **No host matrix inject for v1.**
- **Arming is not LOAD-only.** Per Deano: the same mid-session media path
  may later be used when a running program loads data without BASIC
  `LOAD ""`, and for future SAVE. Sofa chrome arms the deck / mailbox; it
  must not assume every consumption is BASIC `LOAD ""`. SAVE and non-LOAD
  consumers remain non-goals for the v1 product deliverable (Decision 6).
- **CLI.** Once slice 2 exists, `fogcast` session media change is an
  operator/diagnostics path. Sofa rooms/tenfoot remains the living-room
  surface.
- **When to arm.** Arm anytime while the session is `active` and not inside
  an active loader copy. When using BASIC load, the user chooses when to
  type `LOAD ""`.
- **Cart-composed sessions.** Same tape path. Expansion composition stays
  unchanged.
- **Replace during LOAD.** Reject with busy. Do not abort the Z80 loader
  mid-copy in v1.
- **Primary launch `.p`.** Stays optional. Mid-session tape does not remove
  the launch-time primary bind.

---

## Ownership

| Owner | Responsibility |
| --- | --- |
| mister-packages | Blob size/opcodes; any mid-session-without-hold-reset contract text or additive ABI |
| misteross | Existing mailbox + LOAD patch; only change RTL if a surveyed gap requires it |
| libmister-runtime | Mid-session media delivery on a live generation without soft-reboot; error/abort |
| FogCast host | Library media objects; session-scoped change-tape API; rooms/tenfoot Load-tape chrome |
| FogCast agent | Staging and runtime calls for the active session |
| FES | This lock, indexes, integration evidence later; no image/kit work in the lock PR |

---

## Phased slices

Same lock in every slice. Ordered implementation PRs after this document
merges (docs → ABI/runtime → host API → sofa picker → HIL), similar in
spirit to idle rooms Phase 2 splash slices.

| Slice | Delivers | Does not deliver |
| --- | --- | --- |
| **0 — this document** | Locked journey, artifact class, selection surface, transport/timing, lifecycle, session rules, non-goals | Code, ABI bytes, UI, kit time |
| **1 — ABI / runtime mid-session blob** | Runtime path to begin/data/commit on an **active** ZX81 generation **without** hold-reset soft-reboot; generation checks; eject/clear; busy-while-LOAD policy | Sofa UI; TZX; stream ABI on ZX81 |
| **2 — host / agent session API** | Session-scoped change-tape (and eject) over existing core-media ids; development CLI/API enough to prove the wire | Rooms chrome; kit HIL |
| **3 — sofa / tenfoot picker** | In-session chrome that **arms** a `.p` on active ZX81; import-or-pick; honest armed/ready errors. Does not host-inject `LOAD ""` | Kit catalog; SAVE; multi-tape; auto-LOAD |
| **4 — HIL / acceptance** | Leased designated-kit proof: BASIC → arm tape → user types `LOAD ""` → program runs; RAM/expansion preserved; Stop restore | Claiming all profiles/images; ear-in; SAVE |

Slice 1 may clarify mister-packages prose or add a narrow opcode if Deano
rejects silent driver divergence from the launch hold-reset policy. Prefer
the smallest contract that matches surveyed RTL.

Exact HTTP paths, opcode numbers for a non-hold-reset clarification, and
picker layout are implementation follow-ups inside the locked classes. They
are not open product picks.

---

## Pointers

| Doc | Why |
| --- | --- |
| [`docs/README.md`](README.md) | Index (Proposals) |
| [`docs/fes-zx81.md`](fes-zx81.md) | Current ZX81 tape one-liner; labelled pointer here |
| [`docs/zx81-expansion-bus.md`](zx81-expansion-bus.md) | Launch-time carts ≠ mid-session tape |
| [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md) | Phase 3 removable media vocabulary |
| [`docs/core-media-evolution.md`](core-media-evolution.md) | Blob vs blob-stream; capacity |
| [`docs/idle-menu-rooms.md`](idle-menu-rooms.md) | One sofa renderer; no kit catalog growth |
| [`sources/misteross/cores/fes-zx81/rtl/zx81_machine.sv`](../sources/misteross/cores/fes-zx81/rtl/zx81_machine.sv) | `$0347` LOAD intercept / `0/0` |
| [`sources/libmister-runtime/src/native/fes_gp.cpp`](../sources/libmister-runtime/src/native/fes_gp.cpp) | Today’s hold-reset `LoadMedia` |
| [`sources/FogCast/fogcast/zx81_rom.go`](../sources/FogCast/fogcast/zx81_rom.go) | Launch-time ROM splice (not tape) |

FES parent merges stay Deano’s. Mark implementation PRs: do not merge;
Deano owns merge.
