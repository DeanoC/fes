# Mesh LAN (design draft)

**Status:** design draft awaiting Deano lock (2026-09-23). The product
intent under "Why this exists" is from Deano. Recommended defaults and
strawmen below are proposals for him to rewrite in this file. This is not
an implementation claim and not a design lock. Deano owns FES parent merge.

**Audience:** FES parent, FogCast (rooms, tenfoot, host, target agent), and
libmister-runtime session boundaries. Read
[the node protocol companion](mesh-node-protocol.md) before inventing a
wire format, a second lease, or a Host/Kit identity enum.

**Related:**

- Node capability, lease, content-id, and I/O contracts:
  [`docs/mesh-node-protocol.md`](mesh-node-protocol.md)
- Rooms product, availability states, Stop restore:
  [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md)
- Rooms authoring:
  [`sources/FogCast/docs/rooms.md`](../sources/FogCast/docs/rooms.md)
- Launch slots and Ready versus composition:
  [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md)
- Kit-as-host is the same host binary, not a second catalog:
  [`docs/idle-menu-rooms.md`](idle-menu-rooms.md)
- Soft-stop, eject, idle, `reboot_required` (mesh orbits these):
  [`docs/soft-restart-path-b.md`](soft-restart-path-b.md)
- Who owns host, agent, runtime, image, packages:
  [`docs/component-boundaries.md`](component-boundaries.md)
- Today's kit lease (claim, renew, expiry, takeover):
  [`docs/kit-sharing.md`](kit-sharing.md)
- Working host → agent → runtime path:
  [`sources/FogCast/docs/ARCHITECTURE.md`](../sources/FogCast/docs/ARCHITECTURE.md)

---

## Why this exists

Product intent from Deano, 2026-09-23.

The original host/kit split assumed a dumb Chromecast-like kit. The kit has
moved beyond that and will eventually run its own host. Several hosts
already sit on the LAN (Linux and Mac) with no real concept of how they
work together.

The intended product is a mesh:

- Today a kit and a host are nodes. The kit has FPGA execute, video and
  audio output, and a local controller. The host displays rooms and sends
  commands to the kit.
- Later there are many nodes. An operation is separate from a particular
  device. An FPGA kit can be a shell. A machine that today only hosts the
  menu might play games locally.
- Eventually I/O is routable. The person at the menu sees the accumulated
  systems and games and runs them without knowing where anything is stored
  or executed.
- The end state is: see rooms and games, pick one, it runs. Sitting at a
  MacBook, a MiSTer, Linux, or Windows stays hidden. Different versions,
  and whether a ROM lives on machine X, stay out of that view. One view of
  a heterogeneous gaming LAN.

This page is the FogCast-facing half: what the sofa shows, how placement
feels, and the migration. The shells' contracts are
[`docs/mesh-node-protocol.md`](mesh-node-protocol.md).

## What is true now

Present tense, FES tip `97fadf24` (includes #130). Paths below exist in
this tree. None of them is a mesh.

One FogCast host owns the catalog, rooms, launch intent, and content
selection. The target agent owns network session and transfer coordination
for a configured kit. libmister-runtime owns FPGA programming, media and
input delivery, Stop, and return to idle. Image assembly stays in FES
`image/`. That split is
[`docs/component-boundaries.md`](component-boundaries.md).

`POST /api/v1/session/launch` may name `target` and bind a live FPGA
session without rewriting `selected_target`. A second configured target
may play at the same time; `GET /api/v1/sessions` lists those plays.
`GET /api/v1/session` is the foreground session, which sofa and kit attach
to for input. The kit lease on the target agent remains the ownership
authority for that kit
([`docs/kit-sharing.md`](kit-sharing.md): 90 second lease, renew every
20 seconds, expiry, explicit takeover). That is still a direct bind from
one shell to configured kits.

Each named target may have a persistent `target_id`. The agent advertises
it with local DNS-SD (`_fogcast._tcp`). TXT carries `target_id` and a
discovery protocol version, not a capability bag, not a catalog, and not
lease secrets. The host adopts a new address only after authenticated
health confirms the expected id and API version. Discovery does not launch
a game. Explicit address configuration remains the fallback. See
[`sources/FogCast/docs/ARCHITECTURE.md`](../sources/FogCast/docs/ARCHITECTURE.md)
(target identity and reconnection).

Rooms distinguish Checking, Missing, Needs a choice, Unavailable, and
Ready. Confirm does not silently do nothing
([`rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md)).
Installed FPGA `core_package` rows, and rows on the `fpga` catalog
platform, stay launchable even when the browse system has no host-emulator
mapping. Raw Coleco carts stay browse-only. Composition readiness
(firmware, ROM, expansion) is a further predicate on the same `game_id`.
FES #130: rooms must not treat a firmware-ready Coleco package as
browse-only. Core-present is not composition-ready
([`launch-composition.md`](../sources/FogCast/docs/launch-composition.md)).

`LoadIdle()`, splash versus attract, and `reboot_required` are specified
elsewhere. This draft does not redesign them
([`idle-menu-rooms.md`](idle-menu-rooms.md),
[`soft-restart-path-b.md`](soft-restart-path-b.md)).

[`idle-menu-rooms.md`](idle-menu-rooms.md) already locks kit-as-host: a kit
may run the same FogCast host and tenfoot as a deployment mode. That is
not a second catalog. `fogcast-kit`'s on-kit grid retires toward rooms.
This mesh draft inherits that lock.

Nothing in the current product joins two hosts' libraries, places execute
on a node the user did not configure, or routes a pad to a different HDMI
sink.

## Decisions

Recommended defaults, written 2026-09-23. Deano has not signed each bullet.
Implementation work should treat the paragraph in this file as the working
text and change it here rather than forking a parallel design.

### 1. Capabilities over roles

**Recommended default.** The protocol has no permanent Host or Kit value
as a node's identity. A node advertises a capability bag:

| Capability | Sofa meaning |
| --- | --- |
| **Execute** | Can run a title. Kinds include `fpga_native` and `native_emu`, and later kinds named the same way. |
| **DisplaySink** | Can present the session picture and audio. |
| **InputSource** | Can supply a pad, keyboard, or pointer to a session. |
| **Catalog / Content** | Can answer "what can we play?" and serve the bytes. |
| **Shell** | Runs rooms (the same host UI). |
| **Coordinator** | Optional. Willing to own one session's lifecycle. |

Today's living-room host is usually Shell plus Catalog. Today's kit is
usually Execute (`fpga_native`), DisplaySink, and InputSource. Those are
deployments of the bag. A later kit-as-Shell node adds Shell. A later
machine that plays locally adds Execute.

### 2. Three planes

**Recommended default.**

| Plane | Carries | Arrives |
| --- | --- | --- |
| **Control** | Discovery, advertisements, leases, session lifecycle, version negotiation | Phase 1, growing through Phase 5 |
| **Content** | Federated library, content-id assets, pull and cache onto the executor | Phase 2 |
| **I/O** | Routable video, audio, and input | Phase 4 |

Control may say which content-ids a session requires. The person's name
for a game is never a filesystem path.

### 3. Content is content-id addressed

**Recommended default.** Hash or other content-id, specified in
[`mesh-node-protocol.md`](mesh-node-protocol.md). The UI shows title,
system, and an availability state. A path is a cache detail on the node
that holds the bytes.

### 4. One lease owner per session

**Recommended default.** One coordinator owns a session. A second
coordinator does not fight the same kit. Soft-stop, eject, and idle
recovery stay kit-local contracts in libmister-runtime. The mesh orbits
them. `LoadIdle()`, splash, attract, and `reboot_required` stay as written
in [`idle-menu-rooms.md`](idle-menu-rooms.md) and
[`soft-restart-path-b.md`](soft-restart-path-b.md).

**Strawman, unsigned.** Through Phase 1 the existing target-agent kit
lease is the only lease, and there is one active Shell. A mesh session
lease that is distinct from that kit lease waits until more than one
shell could address the same executor. The kit lease remains the
executor's admission token either way. Details:
[`mesh-node-protocol.md`](mesh-node-protocol.md).

### 5. Version skew is visible

**Recommended default.** Mesh-protocol major, execute ABI, and required
content are negotiated and shown. An incompatible node is "can't play
here yet" in the rooms Unavailable family (AvailUnavailable-class UX).
A session does not half-start. The same honesty bar as launch
composition: fail closed before programming when a required slot is
absent, and do not treat a black picture as success.

### 6. Today's direct bind keeps working

**Recommended default.** Phase 0 is the current host↔kit bind, including
named `target` and a second configured target already playing. Mesh is
additive. A room with one shell and one kit launches as it does now,
without waiting for a mesh.

### 7. Placement policy

**Strawman (Phase 3).** Deano has not signed this order.

1. Prefer a display near the shell the person is using.
2. Prefer FPGA execute when an ABI / `core_package` exists for the title.
3. Otherwise native execute on a node that advertises the required
   Execute kind.
4. Fail closed when the mesh-protocol major does not match, or required
   content has no source.

An optional advanced override exists for a power user. The default sofa
path does not ask which machine.

### 8. Kit-as-Shell and host-as-Execute

**Recommended default.** Both are first-class Phase 5 outcomes. They
match [`idle-menu-rooms.md`](idle-menu-rooms.md): kit-as-host is the same
host binary, not a second catalog. A host that executes locally uses the
same Execute capability as any other node.

## Visible contract

What the person at the menu should see. "Today" is tip `97fadf24`.
"Target" is the intended product across the phases below. Target is
intent, not a claim that mesh code exists.

| Moment | Today | Target |
| --- | --- | --- |
| **Sitting down** | One shell (tenfoot rooms, browser, or kit grid) on one host's library, bound to configured kits. | Rooms and the library are one view of the LAN. OS, seat, and which disk holds a ROM stay hidden. |
| **Picking a game** | Checking, Missing, Needs a choice, Unavailable, Ready against this host and this kit's packages. | The same states. Unavailable also covers no capable executor, content with no source, version skew ("can't play here yet"), lease held, and I/O route unavailable. |
| **Playing** | Picture on the kit HDMI. Pad on the kit, or host input attached to the foreground session. | Picture and pad are the session's bindings. Where execute ran is not a prompt. |
| **Another computer on the LAN** | Its own host and catalog, if someone started one. No shared rooms view. | The same rooms and games, subject to leases and version skew. |
| **Kit alone** | `fogcast-kit` grid today. Locked direction: the same host binary (kit-as-host), not a second library. | Shell is just a capability on that node. Still one catalog: rooms. |

## Process model

```text
Phase 0 (now)
  Shell on one host --session API--> that host
       --configured target--> agent on the kit
       --local socket--> mister-runtime --> FPGA
  Display and local pad are on the kit.
  Host input attaches to the foreground session.

Phase 5 (intent)
  Shell on whichever node the person is using
       --control--> one coordinator for this session
       --content--> content-ids ensured on the chosen Execute node
       --I/O--> DisplaySink and InputSource bindings
  Rooms show one library. Placement is policy.
```

Owners stay where [`component-boundaries.md`](component-boundaries.md)
puts them. Physical transitions stay in libmister-runtime. Network and
session coordination stay in the FogCast agent. Rooms and the library
stay in FogCast. Shared definitions, when a later phase freezes a wire
contract, stay in mister-packages. Image assembly stays in FES `image/`.
This draft names the contract those owners will speak. It does not move
the owners.

## Phases

The recommended defaults above hold in every phase. Later phases do not
add a Host/Kit identity enum.

| Phase | Delivers | Does not deliver |
| --- | --- | --- |
| **0 — now** | One Shell bound to configured kits. Direct session API, kit lease, rooms availability, `core_package` launchable versus browse-only. A second configured target may already play. This is the current post soft-restart / rooms T11 world. | Mesh discovery, a federated catalog, placement, routable I/O |
| **1 — see the nodes** | Multi-node discovery and capability advertisements. Still one active Shell. Kits remain FPGA executors. | A second shell taking the kit; moving ROMs; remote HDMI |
| **2 — one library** | Federated catalog. Content-id addressed bytes. Multi-host content with no "ROM is on machine X" in the UI. | Automatic placement; routable pads and picture |
| **3 — placement** | Automatic placement, plus an optional advanced override. Policy strawman is Decision 7. | Routable I/O as the normal path |
| **4 — routable I/O** | Remote pad toward a remote HDMI or capture-class sink. | Kit-as-Shell required for ordinary play |
| **5 — symmetry** | Kit-as-Shell and host-as-Execute. Same host binary on the kit. Same Execute capability on a machine that also runs a shell. | WAN, accounts, DRM, two people playing one kit at once |

Phase 0 is the compatibility floor. A change that breaks one configured
host launching an installed package on one kit is outside every phase.

Which protocol objects show up in which phase is the table in
[`mesh-node-protocol.md`](mesh-node-protocol.md).

## Non-goals for v1

v1 means through Phase 5 unless a later PR reopens this table. The
protocol page repeats the wire-level non-goals.

- WAN mesh
- Cloud accounts
- DRM
- More than one player in simultaneous play on one kit
- Redesign of FPGA ABI packages
- Redesign of `LoadIdle()`, splash, attract, or `reboot_required`
- A second offline catalog beside rooms
- Filesystem paths as the way the sofa names a game
- A permanent Host or Kit role as node identity
- Claiming any phase above 0 is implemented

## Open questions

Prefer the strawman. Change the sentence in this document rather than
forking a parallel design.

**Placement order.** Strawman: Decision 7 (display near the shell, then
FPGA execute when a package exists, then native, else fail closed).
Deano has not signed that order.

**Second shell.** Recommended default: Phase 1 still has one active
Shell. A second shell may see nodes and must not take the lease. Deano
can pull a second shell earlier if two sofas must share one kit before
placement exists.

**Content-id versus title.** Strawman: content-id names bytes; title
identity is the catalog id the room already uses. The hash algorithm is
open in the protocol doc.

**Who coordinates.** Strawman: the Shell that started the session, unless
the household has pinned another coordinator. Coordinator is an optional
capability, not a box that must be bought.

**Windows.** Intent includes sitting at Windows. Strawman: a Windows node
is Shell, DisplaySink, and InputSource first. Execute waits until a
native executor exists there. Mac, Linux, and kit phases do not wait on
Windows.

No other product question is posed as locked. Wire encodings, TTL
numbers, and the exact placement tie-break are follow-ups inside the
protocol doc once Deano locks the paragraphs above.

## Pointers

| Doc | Why |
| --- | --- |
| [`docs/mesh-node-protocol.md`](mesh-node-protocol.md) | Capability, lease, content-id, session, failure classes |
| [`docs/README.md`](README.md) | Index |
| [`docs/idle-menu-rooms.md`](idle-menu-rooms.md) | Same host binary for kit-as-host; idle contracts this mesh does not reopen |
| [`docs/soft-restart-path-b.md`](soft-restart-path-b.md) | `reboot_required` and idle recovery stay kit-local |
| [`docs/component-boundaries.md`](component-boundaries.md) | Host, agent, runtime, image, package owners |
| [`docs/kit-sharing.md`](kit-sharing.md) | Current kit lease |
| [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md) | Availability states and Stop restore; LAN routing was deferred here |
| [`sources/FogCast/docs/rooms.md`](../sources/FogCast/docs/rooms.md) | Room authoring |
| [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md) | Slots, Ready, #130 launchable versus browse-only |
| [`sources/FogCast/docs/ARCHITECTURE.md`](../sources/FogCast/docs/ARCHITECTURE.md) | Current session, target id, DNS-SD |

FES parent merges stay Deano’s.
