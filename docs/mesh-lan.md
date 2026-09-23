# Mesh LAN (design draft)

**Status:** merged as #131. Phase 1 closed on main `3d34b6e0`. Phase 2
execution is [`mesh-phase2.md`](mesh-phase2.md). Bob coordinates; Deano
merges parents. Still edit strawmen in place. Caster's cast/kit
contract review and Foggy's product review, both 2026-09-23, are
folded into the recommended defaults and strawmen marked below.
Neither review is a design lock. The product intent under "Why this
exists" is from Deano. Phase 1 execution is
[`mesh-phase1.md`](mesh-phase1.md). This page is not an implementation
claim for slices [`mesh-phase2.md`](mesh-phase2.md) still lists as open.

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

Recommended defaults, written 2026-09-23. Items marked Caster or Foggy
replace the earlier strawman on that point. Deano has not signed each
bullet. Implementation work should treat the paragraph in this file as
the working text and change it here rather than forking a parallel
design.

Foggy's review endorses, and this draft keeps: content-id distinct from
title id; one lease owner per executor; Phase 0 as the floor; a Windows
node as Shell first; idle, splash, and `reboot_required` left as they
stand in the idle and soft-restart docs.

### 1. Capabilities over roles

**Recommended default.** The protocol has no permanent Host or Kit value
as a node's identity. A node advertises a capability bag:

| Capability | Sofa meaning |
| --- | --- |
| **Execute** | Can run a title. Kinds include `fpga_native` and `native_emu`, and later kinds named the same way. |
| **DisplaySink** | Can present the session picture and audio. The advertisement means the node can present. It does not mean HDMI is healthy or that a capture preview is the sink. |
| **InputSource** | Can supply a pad, keyboard, or pointer to a session. One play session on a kit. Not two players at once on that kit. |
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

**Recommended default, updated from Caster review 2026-09-23.** A title
is not one hash of "the bytes the executor loads." Coleco-class
composition keeps separate identities:

- package / ABI identity
- BIOS content-id, when that slot is required
- primary media content-id, when that slot is required
- expansion content-ids, when those slots are required

ROM-less Pong may be package / ABI only. The UI shows title, system, and
an availability state. A path is a cache detail on the node that holds
one of those objects. Phase 2 proposes an unsigned SHA-256 strawman
in [`mesh-phase2.md`](mesh-phase2.md). Deano has not locked it. Shape:
[`mesh-node-protocol.md`](mesh-node-protocol.md).

### 4. One owner per executor session

**Recommended default, updated from Caster review 2026-09-23.** One
coordinator owns one executor's session. A second coordinator does not
fight that executor. Many sessions may run together when each uses a
different executor. Phase 0 already does this: a second configured
target may play while the first is still playing.

Soft-stop and idle recovery stay kit-local in libmister-runtime. The
mesh still states the lease policy rooms need, aligned with
[`kit-sharing.md`](kit-sharing.md). An empty-body
`POST /api/v1/session/stop` that reaches idle releases the kit lease
(`ReleaseKitLease` is the explicit user Stop). Tenfoot rooms Soft-stop
sends `{"retain_lease":true}` and keeps the grant after that same idle
cleanup. Replacement Stop and development `stop` retain. The mesh keeps
that split and places sofa Soft-stop (B, back to the same room) on the
retain side:

| Action | Hardware | Lease |
| --- | --- | --- |
| **Sofa Soft-stop** (B while playing). Rooms Scenario 1: Play, then B, same room. | Defined idle on the executor. Observing shells clear "playing." | Retained through the room stay, including a later load in that visit. |
| **Explicit user Stop** | Cleanup, then idle. | Released after cleanup. |
| **Development `stop`** | Hardware returns to idle. | Retained, as kit-sharing already does. |
| **Stop that replaces a game** | Idle, then the next load. | Retained across that handoff. |

When the Shell is remote from the kit, `LoadIdle()` belongs to the
Execute node and its runtime path. A second Shell does not call it.

**Stop restore. Recommended default, updated from Foggy product review
2026-09-23.** Return from play restores the same room, location, and
parent stack on the Shell that started play
([`rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md)).
That restore stays on that Shell even if the coordinator later moves.
Another Shell does not become the return surface.

`LoadIdle()`, splash, attract, and `reboot_required` stay as written in
[`idle-menu-rooms.md`](idle-menu-rooms.md) and
[`soft-restart-path-b.md`](soft-restart-path-b.md). The mesh does not
add a remote reboot. Path B stays on hold and kit-local.
`reboot_required` is sofa-visible. Classes:
[`mesh-node-protocol.md`](mesh-node-protocol.md).

**Renewal loss.** The kit lease, about 90 seconds, is the recovery
clock. The sofa leaves a half-active session. Other shells see that
lease free only after expiry and cleanup. Silence is not a reason to
take the kit. An advertisement going quiet does not release play. Only
lease expiry frees play.

**Operator takeover.** Generation takeover in kit-sharing is a
development and operations exception. It is explicit, and it stays off
the sofa Confirm path. It is not a failure-class steal.

**Strawman, unsigned.** Through Phase 1 the existing target-agent kit
lease is the only lease. A mesh session lease distinct from that kit
lease waits until a session binds an executor that has no kit lease.
The kit lease remains the FPGA executor's admission token. Deano
confirms before Phase 1 code.

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

**Recommended default, reconciled from Foggy's product review and
Caster's review, 2026-09-23.** Phase 3. Deano has not locked this order.
The general rule is not "a display near the shell."

1. Prefer the household display preference, or, if none is set, the last
   DisplaySink used for play. In the usual living room that is the kit
   HDMI, including when the shell is a Mac. The laptop screen does not
   win because the menu is running there.
2. When Execute is `fpga_native`, prefer that kit's DisplaySink. FPGA
   picture stays on the kit.
3. Prefer FPGA execute when an ABI / `core_package` exists for the
   title. Otherwise native execute on a node that can run it. Otherwise
   fail closed: mesh-protocol major mismatch, or a required composition
   slot with no source.
4. "Near the shell" applies only when the shell and the sink are the
   same seat, or when Phase 4 binds a captured remote sink.
5. A V4L2 or ShadowCast-class preview is a host preview. It is not a
   DisplaySink, and it does not win placement.

An optional advanced override exists for a power user. The default sofa
path does not ask which machine.

### 8. Kit-as-Shell and host-as-Execute

**Recommended default.** Both are first-class Phase 5 outcomes. They
match [`idle-menu-rooms.md`](idle-menu-rooms.md): kit-as-host is the same
host binary, not a second catalog. A host that executes locally uses the
same Execute capability as any other node.

### Attract

**Recommended default, updated from Foggy product review 2026-09-23.**
This draft does not reopen attract. Idle attract stays the ABI in
[`idle-menu-rooms.md`](idle-menu-rooms.md). While a room is the focused
surface, attract stays default off, as in
[`rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md).
A mesh node does not start attract, move focus, or steal input on
another Shell.

## Visible contract

What the person at the menu should see. "Today" is tip `97fadf24`.
"Target" is the intended product across the phases below. Target is
intent, not a claim that mesh code exists.

| Moment | Today | Target |
| --- | --- | --- |
| **Sitting down** | One shell (tenfoot rooms, browser, or kit grid) on one host's library, bound to configured kits. | Rooms and the library are one view of the LAN. OS, seat, and which disk holds a ROM stay hidden. |
| **Picking a game** | Checking, Missing, Needs a choice, Unavailable, Ready against this host and the bound kit's packages. Each state already has its own Confirm behavior. | The same states, with the copy contract below. Phase 1 Ready stays bound-kit composition. Phase 2 Ready means this session can play here. |
| **Playing** | Picture on the kit HDMI. Pad on the kit, or host input attached to the foreground session. A host V4L2 preview, when configured, is a local preview. | For FPGA execute the picture is the kit DisplaySink. A capture preview is not that sink. Where execute ran is not a prompt. |
| **Soft-stop (B)** | Tenfoot now-playing stop posts `retain_lease: true` and keeps the lease. Empty-body session stop (CLI, browser, kit grid) still releases. Replacement Stop and development `stop` retain. | Same room. Defined idle. Observing shells clear "playing." Soft-stop retains the lease through the room stay. A separate explicit user Stop still releases after cleanup. |
| **Renewer disappears** | The kit lease expires in about 90 seconds if renewal stops. The holder loses mutations. The kit is not taken early. | The sofa leaves the half-active session. Other shells see the executor free only after expiry and cleanup. |
| **Another computer on the LAN** | Its own host and catalog, if someone started one. No shared rooms view. | The same rooms and games, subject to leases and version skew. |
| **Kit alone** | `fogcast-kit` grid today. Locked direction: the same host binary (kit-as-host), not a second library. | Shell is just a capability on that node. Still one catalog: rooms. |
| **Second shell, kit leased** | A client without the lease cannot take the kit to Stop someone else. | Phase 1: Unavailable, copy "in use." No silent steal. The lease frees only after expiry and cleanup. |
| **Stop restore** | Stop returns the sofa to the same room, location, and parent stack. | That restore stays on the Shell that started play, even if the coordinator later moves. |

**Ready, Phase 2. Recommended default, updated from Foggy product review
2026-09-23.** Ready means this session can play here:

- an Execute binding this Shell can use
- required content ensured for that binding
- the executor's lease free
- mesh-protocol major OK

A title that only exists somewhere on the LAN is not Ready. Distant-only
is Unavailable, with a next action. Phase 1 Ready is unchanged: composition
against the bound executor, not an advertisement from some other node.

**Availability copy. Recommended default, updated from Foggy product
review 2026-09-23.** Details and Confirm stay distinct. These five do
not collapse into one Unavailable string. Other protocol classes
(executor busy, `reboot_required`, agent unreachable, ensure in
progress, I/O route) keep their own copy in
[`mesh-node-protocol.md`](mesh-node-protocol.md) and do not fold into
this list.

| State | Details | Confirm |
| --- | --- | --- |
| **Checking** | Still resolving whether this title can play here. | Wait. Do not launch. |
| **Missing content** | Name the missing slot (BIOS, primary media, or expansion) and the way to supply it. | The resolution action for that slot. If there is no one-step action, open Details. |
| **Needs a choice** | Several editions match and none is saved. | Force a clear choice. Remember it for the household. Do not launch an arbitrary edition. |
| **In use** | This executor is in use. | Do not launch. Do not take the lease. |
| **Version skew** | "Can't play here yet," with the mismatch in plain language. | Do not launch. |

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
       --content--> package/ABI plus slot content-ids ensured on the chosen Execute node
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
| **1 — see the nodes** | Multi-node discovery and capability advertisements. Still one active Shell. Kits remain FPGA executors. Ready stays Phase 0 composition against the bound executor. A second shell that finds that kit leased shows Unavailable "in use" and does not take the lease. | A second shell taking the kit; silent steal; moving ROMs; remote HDMI; treating "some node advertises Execute" as Ready |
| **2 — one library** | Federated catalog. Package / ABI identity plus BIOS, primary-media, and expansion content-ids. No "ROM is on machine X" in the UI. Ready means this session can play here (Execute, content ensured, lease free, mesh major OK). | Automatic placement; routable pads and picture; one hash standing in for a Coleco composition; Ready merely because the bytes exist somewhere on the LAN |
| **3 — placement** | Automatic placement, plus an optional advanced override. Policy is Decision 7. | Routable I/O as the normal path; a capture preview counted as DisplaySink |
| **4 — routable I/O** | Remote pad toward a remote HDMI sink, or a captured remote sink that is actually bound as DisplaySink. | Kit-as-Shell required for ordinary play; treating today's V4L2 / ShadowCast-class preview as that sink |
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
- More than one player in simultaneous play on one kit (InputSource does not mean a second player on that kit)
- Redesign of FPGA ABI packages
- Redesign of `LoadIdle()`, splash, attract, or `reboot_required`. Attract stays default off on a focused room, and does not steal focus across nodes.
- A second offline catalog beside rooms
- Filesystem paths as the way the sofa names a game
- A permanent Host or Kit role as node identity
- Claiming any phase above 0 is implemented

## Open questions

Prefer the strawman. Change the sentence in this document rather than
forking a parallel design.

**Placement order.** Recommended default, Foggy and Caster, 2026-09-23:
Decision 7. Household display preference or last play sink first
(living-room kit HDMI over a Mac shell). FPGA execute uses that kit's
DisplaySink. Near the shell only for the same seat or a Phase 4
captured remote sink. A V4L2 or ShadowCast-class preview is not a
DisplaySink. Deano has not locked the paragraph. Which `native_emu`
node wins when several can run the title is still open.

**Second shell.** Recommended default, aligned with Caster and Foggy:
Phase 1 still has one active Shell. A second shell may see the kit. If
the lease is held, that shell shows Unavailable "in use" and does not
take it. Many sessions across different executors stay allowed, as
Phase 0 already allows a second configured target. Deano can pull a
second shell onto one kit earlier if two sofas must share that kit
before placement exists.

**Content identity.** Recommended default, updated from Caster review
2026-09-23: package / ABI identity plus BIOS, primary-media, and
expansion content-ids as the composition requires. Title identity stays
the catalog id. One hash of the whole launch is not the model. Phase 2
Slice 1 proposes SHA-256 (`sha256:` plus 64 lowercase hex) as an
unsigned strawman in [`mesh-phase2.md`](mesh-phase2.md). Deano has not
locked it.

**Who coordinates.** Strawman: the Shell that started the session, unless
the household has pinned another coordinator. Coordinator is an optional
capability, not a box that must be bought.

**Windows.** Intent includes sitting at Windows. Strawman, endorsed by
Foggy: a Windows node is Shell, DisplaySink, and InputSource first.
Execute waits until a native executor exists there. Mac, Linux, and kit
phases do not wait on Windows.

**Play facts.** Strawman, from Foggy's review. Last played, play count,
and any later completion record key on the game / title id. They do not
key on node-id. Two shells show the same played state for the same
title. What happens when two shells write at once (write-wins) is TBD.

**Destination strips.** Strawman, from Foggy's review. The compact
destination strip in a room stays Shell-local Lua: the room script and
the tenfoot display list. It is not a federated catalog object and it
is not served from another node.

No other product question is posed as locked. Wire encodings, TTL
numbers, native-executor tie-break, and host node-id minting are
follow-ups inside the protocol doc once Deano locks the paragraphs
above. Advertisement TTL never frees a play lease. Only lease expiry
does.

## Pointers

| Doc | Why |
| --- | --- |
| [`docs/mesh-node-protocol.md`](mesh-node-protocol.md) | Capability, lease, content-id, session, failure classes |
| [`docs/mesh-phase1.md`](mesh-phase1.md) | Phase 1 execution, closed on `3d34b6e0` |
| [`docs/mesh-phase2.md`](mesh-phase2.md) | Phase 2 execution, content plane |
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
