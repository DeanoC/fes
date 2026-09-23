# Mesh node protocol (design draft)

**Status:** design draft awaiting Deano lock (2026-09-23). Conceptual
contracts only. This is not a wire-format freeze, not an opcode list, and
not an implementation claim. Recommended defaults and strawmen are
labeled. Deano owns FES parent merge. Product intent lives in
[`docs/mesh-lan.md`](mesh-lan.md); edit that file for sofa behavior and
this file for what the shells agree.

**Audience:** FES parent, FogCast host and target agent, libmister-runtime
session boundaries, and mister-packages when a later phase actually
freezes bytes. Read [`docs/mesh-lan.md`](mesh-lan.md) first.

**Related:**

- Product, rooms, placement, phases 0–5:
  [`docs/mesh-lan.md`](mesh-lan.md)
- Current host, agent, and runtime jobs:
  [`docs/component-boundaries.md`](component-boundaries.md)
- Current kit lease:
  [`docs/kit-sharing.md`](kit-sharing.md)
- Idle and `reboot_required` (do not redesign here):
  [`docs/idle-menu-rooms.md`](idle-menu-rooms.md),
  [`docs/soft-restart-path-b.md`](soft-restart-path-b.md)
- Working session and DNS-SD identity:
  [`sources/FogCast/docs/ARCHITECTURE.md`](../sources/FogCast/docs/ARCHITECTURE.md)
- Launch slots, Ready, `core_package` versus browse-only:
  [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md)
- Rooms Unavailable / Ready:
  [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md)

---

## Why this exists

[`mesh-lan.md`](mesh-lan.md) is what the person at the menu sees. This
page is what nodes have to agree so that view stays honest: who a node
is, what it can do, how a session is leased, how bytes are named, and
how failure looks.

Encodings, ports, and JSON fields land later, in mister-packages and
FogCast, when a phase is scheduled. Those encodings have to match the
decisions here. They do not get invented beside them.

## What is true now

Present tense, FES tip `97fadf24` (includes #130).

Control today is the FogCast host session API to a configured target
agent, then `/run/mister-runtime.sock` protocol 2. The agent is the kit
lease authority. A lease lasts 90 seconds; clients renew every 20
seconds; expiry and explicit takeover are in
[`docs/kit-sharing.md`](kit-sharing.md). The runtime decides physical
transitions, including idle recovery and `reboot_required`.

Identity today is a persistent `target_id` per configured kit. DNS-SD
TXT carries that id and a discovery protocol version. Advertisements
carry no credentials, no library, and no lease secrets. Health must
confirm the id and the target API version before the host adopts a new
address. There is no mesh-protocol version and no capability bag.

Content today is the host library plus target-side cache for a launch
the host already resolved. Catalog `launchable` is a platform and
target gate. `enrichLaunchable` keeps installed FPGA `core_package`
rows, and `fpga` platform rows, launchable when the browse system has
no host emulator (FES #130). Raw carts for that system stay browse-only.
Composition flags (`firmware_ready` and the other slot flags) still
decide Ready. Package bytes have hashes on the package path; the sofa
does not yet address the household library by content-id, and paths are
still a host-local detail rather than a LAN-wide name.

I/O today is the kit's HDMI and local controllers, plus host input
attached to the foreground session. One primary host input stays on
that foreground session. A Linux host may show a local V4L2 capture
preview. That preview is a host setting. It is not a routable
DisplaySink.

Phase 0 sessions keep this path. Mesh fields, when they exist, are
additive.

## Planes, and which contracts appear when

Names match [`mesh-lan.md`](mesh-lan.md). A cell says the contract is
in force for new mesh behavior. Phase 0 columns are the current
system, described so later phases have a floor.

| Phase | Control | Content | I/O |
| --- | --- | --- | --- |
| **0 — now** | Host session API, named `target`, kit lease. No mesh-protocol version. | This host's library and that target's cache. | Kit HDMI and audio. Kit pad. Host input on the foreground session. |
| **1 — see the nodes** | Node identity, capability advertisement, TTL and heartbeat, mesh-protocol version. One active Shell. | Unchanged. Advertisements do not list titles. | Unchanged. |
| **2 — one library** | A session may name required content-ids. Failure class: content missing and no source. | Catalog entry shape. Federated content-id. Pull and cache onto the executor. | Unchanged. |
| **3 — placement** | One coordinator. Chosen Execute, Display, and Input recorded on the session. Lease conflict rejects. | Ensure step completes before execute. | Bindings are named. They are still local to the chosen nodes. |
| **4 — routable I/O** | An I/O route is a binding that can fail closed. | Unchanged. | Video, audio, and input may run on nodes other than the executor. |
| **5 — symmetry** | Any node may advertise Shell, Execute, or both. | Same content-ids. | Same routes. Kit-as-Shell is the same host binary. |

## Node identity

**Recommended default.**

| Field | Rule |
| --- | --- |
| **node-id** | Stable, opaque. Not a bearer token, not a hostname, not a boot id. |
| **Human name** | Display only. Not a join key. Two nodes may share a label; they do not share an id. |
| **OS and arch** | Informational. `linux` / `arm` does not mean the node can program an FPGA. |
| **mesh-protocol version** | Major and minor. A major mismatch fails closed for any session that needs the higher contract. Minor may add optional fields. |

**Strawman, unsigned.** One id scheme covers every node, kits included,
so a kit that later gains a shell keeps the same id. Today's `target_id`
is the precedent to generalize, rather than a second identifier minted
beside it. Phase 0 nodes that only speak the current target API omit
mesh-protocol version and stay directly bindable.

## Capability advertisement

**Recommended default.** A node announces its bag with a TTL. A
heartbeat refreshes the TTL. Silence past the TTL means placement
treats the node as absent. Silence does not, by itself, release a
kit-local lease. Lease expiry stays the rule in
[`kit-sharing.md`](kit-sharing.md). A playing session follows its
lease, not the advertisement TTL.

Advertisements carry no credentials, no title list, and no lease
secrets. That matches today's DNS-SD discipline (id and version only).

**Recommended bag.** Names are for this draft. They are not a frozen
enum.

| Capability | Advertises | Does not advertise |
| --- | --- | --- |
| **Execute** | Kind: `fpga_native`, `native_emu`, later kinds. For `fpga_native`, the ABIs / package families this node can run. | "Any RBF", a display name, or a raw file path. |
| **DisplaySink** | This node can present session picture and audio. | A promise that HDMI is the only sink, or that capture preview is already routing. |
| **InputSource** | This node can supply pad, keyboard, or pointer. | A second player on the same kit. |
| **Catalog** | This node can answer library queries for the mesh. | The whole library inside the advertisement. |
| **Content** | This node can serve bytes for content-ids it holds. | Paths. |
| **Shell** | This node runs rooms (the same host UI). | A distinct kit catalog. |
| **Coordinator** | This node is willing to own session lifecycle. Optional. | A requirement that a dedicated box exist in Phase 1. |

Catalog without Content is a directory. Content without Catalog is a
cache. A node may advertise both.

**Strawman, unsigned.** TTL is short enough that a closed laptop drops
out of placement within a few heartbeats, and long enough that a brief
Wi-Fi gap does not remove a node that is still renewing a play lease.
This draft does not pick seconds. Deano sets them when Phase 1 is
scheduled.

## Catalog entry

**Recommended default.** Aligns with `enrichLaunchable`, `core_package`,
and the `fpga` platform lesson from FES #130, and with launch
composition slots.

A catalog entry is what a shell may show. It is not a filesystem row.

| Field | Meaning |
| --- | --- |
| **Title identity** | Stable id for the work the person picks. The room and library already key off a game id. Mesh keeps that, separate from bytes. |
| **System** | Browse system (`coleco`, `zx81`, and the rest). Not an execute capability. |
| **Required content-ids** | Bytes that must be ensured before play: firmware, primary media, and expansions that are content. Empty when the package is the whole title (ROM-less Pong). |
| **Required execute capabilities** | For example Execute `fpga_native` plus a package ABI, or Execute `native_emu` for a system that has one. |
| **Launchable versus browse-only** | The current split. See below. |

Launchable versus browse-only, carried forward on purpose:

- Installed FPGA `core_package` rows, and rows on the `fpga` catalog
  platform, stay launchable even when `PlatformLaunchable(system)` is
  false. A firmware-ready Coleco package is not browse-only.
- Raw library carts for a system with no executor mapping stay
  browse-only.
- Composition flags still gate Ready. Missing `firmware_ready` (or ROM,
  or a required expansion) is Unavailable with a next action, as in
  [`launch-composition.md`](../sources/FogCast/docs/launch-composition.md).
- Missing flags do not grant eligibility.

**Ready** on the mesh means all of the following:

- Some node advertises the required Execute capability.
- Every required content-id has a source.
- Mesh-protocol majors match for the nodes in the session.
- The executor is free of a conflicting lease.

Otherwise the row is Unavailable with one of the failure classes below,
or it stays browse-only. Checking, Missing, and Needs a choice keep
their rooms meanings
([`rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md)).

Paths do not appear in this shape.

**Strawman, unsigned.** content-id is a cryptographic hash of the exact
bytes the executor will load, not of a zip wrapper and not of a path.
Title identity stays the catalog id. Deano picks the hash when Phase 2
is scheduled.

## Session request

**Recommended default.**

| Step | Who |
| --- | --- |
| Initiate | The Shell the person is using. |
| Coordinate | One coordinator. Strawman: that same Shell, unless the household pinned another. |
| Bind | Chosen Execute, DisplaySink, and InputSource, by node-id. |
| Ensure content | Before execute, each required content-id is already on the Execute node or is pulled from a Content node. Failure happens before programming. |
| Execute | The Execute node's existing runtime path. For `fpga_native`, that is still agent then mister-runtime. The mesh does not program the FPGA itself. |

Phase 0 initiation remains `POST /api/v1/session/launch` to the
configured target. Mesh bindings are extra fields on a later request,
not a second launch API beside the session the sofa already uses.

**Strawman, unsigned.** The coordinator is the only node that claims
the executor's lease for this session. Other shells observe. They do
not renew.

## Lease

**Recommended default.**

- Operations are claim, renew, and release.
- Conflict rejects. A second shell's launch does not preempt the holder.
- One owner per session.
- Kit-local soft-stop, eject, idle, and `reboot_required` stay on the
  executor. The mesh record does not load a bitstream.
- Development expiry and explicit operator takeover in
  [`kit-sharing.md`](kit-sharing.md) remain available for the designated
  kit.

**Strawman, unsigned.** For an FPGA executor, the mesh claim is the
same kit lease the agent already enforces, so two authorities cannot
both believe they own the kit. A separate mesh-only lease object waits
until a session can bind an executor that has no kit lease (native
execute on a host). Deano confirms this before Phase 1 code.

## Failure classes

**Recommended default.** Each class is visible. The sofa uses the
Unavailable family or launch-failure chrome, with a specific next
action. Confirm does not no-op. A session does not stay half-active.

| Class | When | Sofa |
| --- | --- | --- |
| **No capable executor** | No node advertises the required Execute kind and ABI. | "Can't play here yet," naming the missing capability in plain language. |
| **Content missing, no source** | A required content-id has no Content node. | Same honesty as a missing BIOS or ROM: name the missing piece and the resolution action. |
| **Version skew** | Mesh-protocol major, or the required ABI, does not match. | "Can't play here yet." Not a silent skip, not a half-session. |
| **Lease held** | Another session owns the executor. | Say it is in use. Do not steal. |
| **I/O route unavailable** | The chosen DisplaySink or InputSource cannot be bound. Phase 4 for remote routes. Before that, the same class covers a chosen node that cannot present or accept input locally. | Name which binding failed. |

Checking remains "still resolving." Missing remains "not in the
household library" after federation has answered. Needs a choice
remains an edition choice. These do not collapse into one error string.

## Non-goals for the v1 protocol

v1 matches [`mesh-lan.md`](mesh-lan.md) through Phase 5 unless a later
PR reopens that table.

- WAN mesh
- Cloud accounts
- DRM
- Simultaneous play by more than one person on one kit
- Redesign of FPGA ABI packages, format-2 descriptors, or misteross
  recipes
- A wire-format freeze in this document
- Replacing `/run/mister-runtime.sock` or the target agent HTTP API
- Putting the library or credentials in discovery advertisements
- Redesign of `LoadIdle()`, splash, attract, or `reboot_required`
- A second catalog protocol for `fogcast-kit`

## Open questions

Prefer the strawman. Change the sentence here, or in
[`mesh-lan.md`](mesh-lan.md) when the question is the sofa, rather than
forking a parallel design.

**Hash and canonical bytes.** Strawman: content-id hashes the exact
bytes the executor loads. Title identity stays the catalog id. Algorithm
unnamed until Phase 2.

**node-id and `target_id`.** Strawman: generalize `target_id` into
node-id. Kits do not gain a second stable id when they grow a shell.

**Heartbeat and TTL numbers.** Unspecified on purpose.

**Coordinator as a capability.** Strawman: advertise it so a headless
node can coordinate later. Phase 1 may leave the capability unused and
treat the single active Shell as coordinator.

**Native execute lease.** Strawman: an FPGA executor keeps today's kit
lease; a `native_emu` executor grows a mesh claim only in the phase
that places native play (Phase 3). Until then, host emulators stay on
the host that owns the shell, as they do now.

## Pointers

| Doc | Why |
| --- | --- |
| [`docs/mesh-lan.md`](mesh-lan.md) | Product intent, placement strawman, phases |
| [`docs/README.md`](README.md) | Index |
| [`docs/component-boundaries.md`](component-boundaries.md) | Agent coordinates; runtime touches hardware |
| [`docs/kit-sharing.md`](kit-sharing.md) | Current claim / renew / expiry / takeover |
| [`docs/idle-menu-rooms.md`](idle-menu-rooms.md) | Kit-as-host binary; idle contracts |
| [`docs/soft-restart-path-b.md`](soft-restart-path-b.md) | `reboot_required` stays kit-local |
| [`sources/FogCast/docs/ARCHITECTURE.md`](../sources/FogCast/docs/ARCHITECTURE.md) | Session API, `target_id`, DNS-SD |
| [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md) | Slots and Ready |
| [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md) | Availability states |
| [`sources/FogCast/docs/rooms.md`](../sources/FogCast/docs/rooms.md) | Room authoring; mesh does not change the Lua model |

FES parent merges stay Deano’s.
