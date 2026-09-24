# Mesh node protocol (design draft)

**Status:** merged as #131. Phase 1 closed on main `3d34b6e0`. Phase 2
execution is [`mesh-phase2.md`](mesh-phase2.md). Bob coordinates; Deano
merges parents. Still edit strawmen in place. Caster's cast/kit
contract review is folded below. Foggy's product review is folded in
[`docs/mesh-lan.md`](mesh-lan.md); this page changes only where that
review meets a protocol rule. Neither review is a design lock.
Conceptual contracts only. This is not a wire-format freeze, not an
opcode list, and not an implementation claim for slices still open in
[`mesh-phase2.md`](mesh-phase2.md). Deano owns FES parent merge.

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
preview. That preview, and any ShadowCast-class preview of the same
kind, is a host setting. It is not a DisplaySink.

Phase 0 sessions keep this path. Mesh fields, when they exist, are
additive.

## Planes, and which contracts appear when

Names match [`mesh-lan.md`](mesh-lan.md). A cell says the contract is
in force for new mesh behavior. Phase 0 columns are the current
system, described so later phases have a floor.

| Phase | Control | Content | I/O |
| --- | --- | --- | --- |
| **0 — now** | Host session API, named `target`, kit lease. No mesh-protocol version. | This host's library and that target's cache. | Kit HDMI and audio. Kit pad. Host input on the foreground session. |
| **1 — see the nodes** | Node identity, capability advertisement, TTL and heartbeat, mesh-protocol version. One active Shell. Ready stays Phase 0 composition against the bound executor. A second shell that sees the kit leased does not claim it. | Unchanged from Phase 0. Advertisements do not list titles and do not make some other node Ready. The shell's knowledge that the kit already has the package selected or installed is bound-target composition only. | Unchanged. A DisplaySink advertisement means the node can present. It does not mean the picture is up. |
| **2 — one library** | A session may name required slot content-ids. Failure class: content missing and no source. Mid-pull stays Checking. Ready means this session can play here, not that the bytes exist on some LAN node. | Catalog entry shape. Package / ABI identity plus BIOS, primary-media, and expansion content-ids. Pull and cache onto the executor this session will use. | Unchanged. |
| **3 — placement** | One coordinator. Chosen Execute, Display, and Input recorded on the session. Lease conflict rejects. | Ensure step completes before execute. | Bindings are named. They are still local to the chosen nodes. |
| **4 — routable I/O** | An I/O route is a binding that can fail closed. | Unchanged. | Video, audio, and input may run on nodes other than the executor. A captured remote sink bound as DisplaySink belongs here. Today's V4L2 or ShadowCast-class preview is not that sink. |
| **5 — symmetry** | Any node may advertise Shell, Execute, or both. | Same content-ids. | Same routes. Kit-as-Shell is the same host binary. |

## Node identity

**Recommended default.**

| Field | Rule |
| --- | --- |
| **node-id** | Stable, opaque. Not a bearer token, not a hostname, not a boot id. |
| **Human name** | Display only. Not a join key. Two nodes may share a label; they do not share an id. |
| **OS and arch** | Informational. `linux` / `arm` does not mean the node can program an FPGA. |
| **mesh-protocol version** | Major and minor. A major mismatch fails closed for any session that needs the higher contract. Minor may add optional fields. |

**Recommended default, updated from Caster review 2026-09-23.** Kits
generalize today's `target_id` into node-id. A kit that later gains a
shell keeps that id. It does not mint a second one. Phase 0 nodes that
only speak the current target API omit mesh-protocol version and stay
directly bindable.

**Strawman, unsigned.** Hosts that are not targets need a minting rule:
where the id is created, where it is stored, and what a reinstall does.
This draft does not pick that rule. Phase 0 shells that are not kits
keep working without a node-id, because Phase 0 is still the direct
bind.

## Capability advertisement

**Recommended default, updated from Caster review 2026-09-23.** A node
announces its bag with a TTL. A heartbeat refreshes the TTL. Silence
past the TTL means placement may treat the node as absent for a new
choice. That silence is not a lease release. A playing session follows
its lease. Only lease expiry frees play, and only after the cleanup
[`kit-sharing.md`](kit-sharing.md) already runs. Advertisement TTL is
not a second clock that frees the kit, including relative to the
roughly 90 second lease.

Advertisements carry no credentials, no title list, and no lease
secrets. That matches today's DNS-SD discipline (id and version only).

**Recommended bag.** Names are for this draft. They are not a frozen
enum.

| Capability | Advertises | Does not advertise |
| --- | --- | --- |
| **Execute** | Kind: `fpga_native`, `native_emu`, later kinds. For `fpga_native`, the ABIs / package families this node can run. | "Any RBF", a display name, or a raw file path. |
| **DisplaySink** | This node can present session picture and audio. Capability only. | Liveness. HDMI or ADV health is a session or health fact, not this advertisement. An advertisement must not be read as "the picture is up." A V4L2 or ShadowCast-class preview is not this capability. |
| **InputSource** | This node can supply pad, keyboard, or pointer for the session. | A second simultaneous player on the same kit. One kit carries one play session. |
| **Catalog** | This node can answer library queries for the mesh. | The whole library inside the advertisement. |
| **Content** | This node can serve bytes for content-ids it holds. | Paths. |
| **Shell** | This node runs rooms (the same host UI). | A distinct kit catalog. |
| **Coordinator** | This node is willing to own session lifecycle. Optional. | A requirement that a dedicated box exist in Phase 1. |

Catalog without Content is a directory. Content without Catalog is a
cache. A node may advertise both.

This draft does not pick TTL seconds. Deano sets them when Phase 1 is
scheduled. Whatever the number, silence past TTL does not free play.

## Catalog entry

**Recommended default.** Aligns with `enrichLaunchable`, `core_package`,
and the `fpga` platform lesson from FES #130, and with launch
composition slots.

A catalog entry is what a shell may show. It is not a filesystem row.

| Field | Meaning |
| --- | --- |
| **Title identity** | Stable id for the work the person picks. The room and library already key off a game id. Mesh keeps that, separate from bytes. |
| **System** | Browse system (`coleco`, `zx81`, and the rest). Not an execute capability. |
| **Required slot identities** | Package / ABI identity, plus BIOS, primary-media, and expansion content-ids when those slots are required. Empty content slots when the package is the whole title (ROM-less Pong). One hash of the whole launch is not this field. |
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

**Ready by phase. Recommended default, updated from Caster review
2026-09-23.**

- **Phase 0 and Phase 1.** Ready is Phase 0 composition against the
  bound executor. The shell uses that target's installed or selected
  package, plus firmware, primary media, and expansion flags for that
  bind. Knowing the kit already has the package is bound-target
  composition only. Another node's Execute advertisement does not make
  the row Ready. There is no federated content yet.
- **Phase 2 and later.** Ready means this session can play here: an
  Execute binding this shell can use, every required slot content-id
  ensured on that executor, the lease free, and mesh-protocol majors
  matching. A copy that only exists somewhere else on the LAN is not
  Ready. Distant-only is Unavailable with a next action. Sofa copy for
  that split is the visible contract in
  [`mesh-lan.md`](mesh-lan.md). A slot mid-pull, or only partly
  present, is Checking. It is not Ready, and it does not program the
  FPGA. The host implements that rule when a mesh execute session is
  installed: rooms and `GET /api/v1/games` call `ReadyHere`. The games
  row then includes `ready_here`, and, when the title is not Ready,
  `ready_block` plus `next_action`. Those are host catalog fields, not
  a mesh wire freeze. Lease-free for that view is this session's grant
  and generation, or an unleased kit this session can claim. A foreign
  holder is not Ready. When the session is not installed the fields
  are omitted and Phase 0 composition Ready stays in force.
- **Phase 3 and later.** Placement may choose the bound executor.
  Until that choice exists, Ready still does not mean "any node that
  advertises Execute."
- The executor for that session is free of a conflicting lease, and is
  idle enough to accept a launch. Busy is its own class, below.

Otherwise the row is Unavailable with one of the classes below, or it
stays browse-only. Checking, Missing, and Needs a choice keep their
rooms meanings
([`rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md)).

Paths do not appear in this shape.

**Recommended default, updated from Caster review 2026-09-23.** One
hash of "the exact bytes the executor loads" is not enough. A launch
composition is:

| Slot | Identity |
| --- | --- |
| **Package / ABI** | The described package and ABI the executor will run. Not a content-id of a raw RBF path. |
| **BIOS** | Content-id when the composition requires firmware (Coleco-class). |
| **Primary media** | Content-id when the title starts from a cart, ROM, or other primary medium. |
| **Expansions** | Content-ids for expansion slots the composition includes. |

Title identity stays the catalog id. Phase 2 proposes an unsigned
SHA-256 strawman in [`mesh-phase2.md`](mesh-phase2.md). Deano has not
locked it. This draft does not freeze bytes.

## Session request

**Recommended default.**

| Step | Who |
| --- | --- |
| Initiate | The Shell the person is using. |
| Coordinate | One coordinator. Strawman: that same Shell, unless the household pinned another. |
| Bind | Chosen Execute, DisplaySink, and InputSource, by node-id. |
| Ensure content | Before execute, each required slot is already on the Execute node or is pulled from a Content node. A pull in progress is Checking. Failure or partial content happens before programming. |
| Execute | The Execute node's existing runtime path. For `fpga_native`, that is still agent then mister-runtime. The mesh does not program the FPGA itself. |

Phase 0 initiation remains `POST /api/v1/session/launch` to the
configured target. Mesh bindings are extra fields on a later request,
not a second launch API beside the session the sofa already uses.

**Strawman, unsigned.** The coordinator is the only node that claims
the executor's lease for this session. Other shells observe. They do
not renew.

## Lease

**Recommended default, updated from Caster review 2026-09-23.**

- Operations are claim, renew, and release.
- Conflict rejects. A second shell's launch does not preempt the holder.
- One owner per executor session. Many sessions may exist across
  different executors. Phase 0 already allows a second configured
  target to play.
- Kit-local soft-stop and idle recovery stay on the executor. The mesh
  record does not load a bitstream. When the Shell is remote,
  `LoadIdle()` is the Execute node's runtime path. A second Shell does
  not call it.
- Sofa policy, matching [`kit-sharing.md`](kit-sharing.md) and
  [`mesh-lan.md`](mesh-lan.md) Decision 4. An empty-body idle
  `POST /api/v1/session/stop` releases the kit lease. Tenfoot rooms
  Soft-stop sends `retain_lease: true` and keeps it. Replacement Stop
  and development `stop` retain. This policy puts sofa Soft-stop on the
  retain side:
  - Sofa Soft-stop (B while playing; Play then B back to the same room)
    reaches defined idle, clears "playing" for observing shells, and
    retains the lease through the room stay.
  - Explicit user Stop releases after cleanup.
  - Development `stop` returns hardware to idle and retains ownership.
  - Stop that replaces a game retains the lease across that handoff.
- Advertisement TTL silence does not release a lease. Only lease expiry
  frees play, after cleanup.
- **Renewal loss.** If the coordinator crashes or stops renewing, the
  kit lease (about 90 seconds) is the recovery. The sofa leaves the
  half-active session. Other shells see the lease free only after
  expiry and cleanup. The mesh does not steal on silence.
- **Operator takeover.** Generation takeover in kit-sharing is a
  development and operations exception. It is explicit, it uses the
  current generation, and it stays off the sofa Confirm path. It is not
  the Lease held failure class and it is not a steal.

`reboot_required` stays kit-local. The mesh does not add a remote
reboot. Path B in [`soft-restart-path-b.md`](soft-restart-path-b.md)
stays on hold. The class is sofa-visible; the recovery action is not a
new mesh command.

**Strawman, unsigned.** For an FPGA executor, the mesh claim is the
same kit lease the agent already enforces, so two authorities cannot
both believe they own the kit. A separate mesh-only lease object waits
until a session can bind an executor that has no kit lease (native
execute on a host). Deano confirms this before Phase 1 code.

## Failure and session classes

**Recommended default, updated from Caster review 2026-09-23.** Each
class is visible. The sofa uses the Unavailable family, Checking, or
launch-failure chrome, with a specific next action. Confirm does not
no-op. A session does not stay half-active. Soft-stop completed is a
success outcome in the same list so every shell uses one vocabulary.

| Class | When | Sofa |
| --- | --- | --- |
| **No capable executor** | No bound executor can run the required Execute kind and ABI. Phase 1 judges the bound kit, not every advertisement on the LAN. | "Can't play here yet," naming the missing capability in plain language. |
| **Content missing, no source** | A required slot has no source. Phase 2 and later for federated content. Phase 0 and 1 use today's bound-target composition miss. | Same honesty as a missing BIOS or ROM: name the missing slot and the resolution action. |
| **Version skew** | Mesh-protocol major, or the required ABI, does not match. | "Can't play here yet." Not a silent skip, not a half-session. |
| **Lease held** | Another owner holds this executor's session. | Say it is in use. Do not steal. Generation takeover is not this class. |
| **Executor busy** | The executor is not idle, a transition is in progress, or the agent is busy. Distinct from Lease held. | Wait. Do not describe it as another person playing, and do not take the lease. |
| **Ensure in progress** | A required slot is mid-pull or only partially present. | Checking. Never Ready. Never program the FPGA on partial content. |
| **reboot_required** | Idle recovery failed, or the runtime already published `reboot_required`. | Sofa-visible. The mesh does not invent a remote reboot. Path B stays on hold and kit-local ([`soft-restart-path-b.md`](soft-restart-path-b.md)). |
| **Agent unreachable** | Transport to the agent is dead, or the renewer crashed and the lease has not expired. | Expiry path. The sofa leaves the half-active session. Other shells see the lease free only after expiry and cleanup. Confirm does not retry as if the kit were idle. |
| **I/O route unavailable** | The chosen DisplaySink or InputSource cannot be bound. Phase 4 for remote routes. Before that, the same class covers a chosen node that cannot present or accept input locally. HDMI or ADV health is this session/health path, not the DisplaySink advertisement. A V4L2 or ShadowCast-class preview does not satisfy the sink. | Name which binding failed. |
| **Soft-stop completed** | Sofa Soft-stop finished. The executor is in defined idle. | Observing shells clear "playing." The lease stays with the owner through the room stay. `LoadIdle()` stays on the Execute node / runtime path. |

Checking remains "still resolving," including a slot that is mid-pull.
Missing remains "not in the household library" after the phase's
catalog has answered. Needs a choice remains an edition choice. These
do not collapse into one error string.

## Kit content operations

**Strawman, unsigned. Not an Ensure-result freeze.** Phase 2's kit
store is `meshcontent.Executor` on the target agent for the node a
session bound. The host calls Ensure against that executor. Ensure's
`Result` still has no JSON tags and is not this section.

The agent exposes the executor's operations so the host can drive the
store. Each call is one method. None of them programs the FPGA or
returns an Ensure result. Pull and link are kit-lease mutations. Node,
slot, and source reads are not.

| Call | Request | Response |
| --- | --- | --- |
| Node | `GET /v1/mesh/content/node` | `node_id`, `abis` (`id`, `major`) |
| Slot | `GET /v1/mesh/content/slot?id=sha256:<64 hex>` | `state`: `present`, `checking`, or `missing` |
| Source | `GET /v1/mesh/content/source?id=sha256:<64 hex>` | `advertises` |
| Pull | `POST /v1/mesh/content/pull?id=sha256:<64 hex>` with an empty body | `state` after the kit reads its content source |
| Link | `POST /v1/mesh/content/link?name=<expansion>` body `{"content_id":"sha256:<64 hex>"}` | the same content-id |

Pull is the kit's copy. The body is not the bytes. A canceled pull
deletes its partial file. A partial file is not `present`. Link records
that expansion's slot-bytes content-id. It does not rewrite primary
media and it does not store a programmed image. Linking the same name
and content-id again is a no-op. A later link failure leaves earlier
links in place.

Pull and link carry `X-FogCast-Kit-Lease` and use the same kit-lease
admission as other kit mutations. A missing or foreign token is
rejected. A hostless owner is `KIT_LEASE_DENIED`. On the host, pull is
the lease-acquiring mutation: a fresh session's first FPGA mesh launch
on a free kit claims that session's grant and then Ensure proceeds. A
kit held by another session fails closed and does not pull. Link
requires the grant the session already holds. LeaseFree stays this
session's current grant and its generation. InUse stays the busy
connection. A held grant whose observed generation does not match
fails closed before any pull.

A pull that does not land the id returns `CONTENT_PULL_FAILED`. The
host session API uses that same class, plus `CONTENT_MISSING`,
`ABI_INELIGIBLE`, `CONTENT_CHECKING_TIMEOUT`, and `KIT_LEASE_DENIED`.
`CONTENT_CHECKING` is Ensure's in-progress block. Launch waits with a
positive timeout, so session launch returns the timeout class instead.

## Non-goals for the v1 protocol

v1 matches [`mesh-lan.md`](mesh-lan.md) through Phase 5 unless a later
PR reopens that table.

- WAN mesh
- Cloud accounts
- DRM
- Simultaneous play by more than one person on one kit. InputSource
  is one play session on that kit, not a second player beside it.
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

**Per-slot hash.** Recommended default, updated from Caster review
2026-09-23: package / ABI identity plus BIOS, primary-media, and
expansion content-ids. One hash of the whole launch is not the model.
Phase 2 Slice 1 proposes SHA-256 (`sha256:` plus 64 lowercase hex) as
an unsigned strawman in [`mesh-phase2.md`](mesh-phase2.md). Deano has
not locked it.

**node-id and `target_id`.** Recommended default, updated from Caster
review 2026-09-23: kits generalize `target_id` and keep that id when
they grow a shell. **Strawman, unsigned:** hosts that are not targets
still need minting rules. Phase 0 does not require those hosts to have
a node-id.

**Heartbeat and TTL numbers.** Unspecified on purpose. Only lease
expiry frees play. TTL silence does not.

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
| [`docs/mesh-lan.md`](mesh-lan.md) | Product intent, placement policy, phases |
| [`docs/mesh-phase2.md`](mesh-phase2.md) | Phase 2 execution and the unsigned hash strawman |
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
