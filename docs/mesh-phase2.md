# Mesh LAN — Phase 2 execution brief

**Status:** Phase 2 started. Slices 1 and 2 are on main. Slice 3 host
ensure is this change. The kit content store is next. Bob coordinates.
Deano owns FES parent merges. Do not merge from this brief.

**Audience:** FogCast host (library and host API), Caster when the kit
content store lands, rooms UX (Foggy) when Ready copy lands, and anyone
picking up that store. Read the locks first. This brief names owners,
slices, and the unsigned hash strawman. It does not reopen Phase 1 and
it does not freeze a wire format.

**Base:** Slice 3 is FES `main` `663ce6d5`. Phase 1 closed at
`3d34b6e0`: Soft-stop #132, capability advertisements #134, host
inventory and in-use #137. Acceptance in
[`mesh-phase1.md`](mesh-phase1.md) stays as written.

---

## Goal / non-goals

**Goal:** one library. A catalog entry can name the package / ABI plus
the BIOS, primary-media, and expansion content-ids the composition
requires. Title identity stays the catalog id. Bytes are named so a
later slice can pull them onto the executor this session will use.

**Phase 2 Ready, when that slice lands.** Ready means this session can
play here: an Execute binding this shell can use, every required slot
content-id ensured on that executor, the lease free, and mesh-protocol
major OK. Distant-only bytes are Unavailable with a next action. A
slot mid-pull stays Checking. Slice 1 does not turn this predicate on
in rooms.

**Does not:**

- Automatic placement
- Routable I/O (pads, picture, or a captured remote sink)
- Kit-as-Shell, or a second catalog on the kit
- A second lease type beside the existing kit lease
- Power, image, or SD mutation
- Ready because some other node advertises Execute
- Ready because the bytes exist somewhere on the LAN
- One hash standing in for a Coleco composition
- A change to `LoadIdle()` / `reboot_required`

Phase 0 stays the floor. Phase 1 stays the floor on top of it. One
configured host launching an installed package on one kit must keep
working. Advertisements still do not list titles. Advertisement silence
is not a lease release.

---

## Locks (do not reopen)

| Doc | What it already decided |
| --- | --- |
| [Mesh LAN](mesh-lan.md) | Content plane, content-id distinct from title id, Phase 2 Ready |
| [Mesh node protocol](mesh-node-protocol.md) | Slot identities, failure classes, mid-pull stays Checking |
| [Phase 1 brief](mesh-phase1.md) | Soft-stop, capability ads, host inventory, in-use copy |
| [Launch composition](../sources/FogCast/docs/launch-composition.md) | Core, firmware, primary media, expansion slots |
| [Kit sharing](kit-sharing.md) | The only FPGA lease |

---

## Owners

Owners are component strawmen. Agree files before parallel edits
([agent workflow](agent-workflow.md)).

| Workstream | Strawman owner | What Phase 2 changes | What it must not do |
| --- | --- | --- | --- |
| **Coordination** | Bob | Slice order and this brief | Parent merge. That stays Deano's. |
| **Host / library** | FogCast host | Content-id, catalog entry shape, then pull and cache onto the executor this session uses | A second lease. Placement. Routable I/O. |
| **Cast / kit** | Caster reviews when Execute or content touches the kit | Later ensure against the executor cache | Power, image, or SD mutation. A new lease type. Kit-as-Shell. |
| **Rooms UX** | Foggy | Phase 2 Ready copy, when that slice lands | Treat distant-only bytes as Ready. Collapse Checking, Missing, Needs a choice, In use, and version skew into one string. |
| **Parent merge** | Deano | Merge to `main` | — |

Slices 1–3 do not touch the kit. Caster's review waits until the kit
content store writes the executor cache.

---

## Ordered slices

### 1. Content-id and catalog entry shape — on main

**Owner:** FogCast host. Package `internal/meshcontent`.

**Acceptance:**

- A content-id is an algorithm name plus a digest. The unsigned
  strawman algorithm is `sha256`: canonical text `sha256:` plus 64
  lowercase hex of that slot's bytes. Unknown algorithms fail closed.
  Deano has not locked the algorithm. See Parked, below.
- Package / ABI is its own slot: described package id (64 lowercase
  hex, the existing package identity) plus ABI id and major. It is not
  a content-id and it is not a hash of a raw RBF path. A launchable
  `fpga_native` entry requires this slot. A launchable `native_emu`
  entry carries no package slot.
- BIOS, primary media, and each named expansion carry a content-id
  when that slot is required. ROM-less package titles omit those
  slots. A launchable `native_emu` entry requires primary media; BIOS
  and named expansions stay optional. Title id is the catalog game id
  (`protocol.ValidateGameID`, a lowercase ASCII slug) and does not
  parse as a content-id.
- A catalog entry carries title id, system, those slots, one required
  execute kind, and launchable versus browse-only. Paths do not appear.
- An in-memory cache records which content-ids one executor holds. It
  stores no bytes and does not contact a peer.
- `ReadyHere` evaluates the Phase 2 Ready rule for tests. Rooms,
  `POST /api/v1/session/launch`, `GET /api/v1/games`, and
  `discovery.ReadyForBoundExecutor` do not call it. Phase 1 Ready stays
  composition against the bound executor.

**Does not:** federated pull, byte cache, a new host route, a title
list on advertisements, or a Ready change.

### 2. Project today's library into that shape — on main

**Owner:** FogCast host.

`fogcast.ProjectMeshLibrary` fills `[]meshcontent.Entry` from the host
library that already exists. A package-backed title gets a
`package_abi` slot: the described package id, the ABI id, and the ABI
major. That major is the package ABI major, not the mesh protocol
major. A title that requires household firmware gets that slot's stored
core-media digest. The selected primary-media digest and each named
expansion's stored digest are content-ids. Digests the host already
stores pass through `meshcontent.FromSHA256`. The projection does not
open those files and does not hash them again. Today's `host_only`
execution projects as `native_emu` and carries no package slot.
Launchable `fpga_native` requires the package slot. Title ids are
catalog game ids (`protocol.ValidateGameID`). A title that cannot be
projected is omitted and returned with a reason. It is not invented.

There is no new host route. Rooms, `POST /api/v1/session/launch`,
`GET /api/v1/games`, and `discovery.ReadyForBoundExecutor` do not call
`ReadyHere` or this projection.

**Does not:** treat that projection as Phase 2 Ready. Does not move
bytes onto the kit. Does not pull across nodes. Does not add a
removable or secondary slot. The hash algorithm stays the unsigned
sha256 strawman.

### 3. Ensure required slots on the bound executor — this change

**Owner:** FogCast host. The executor is an interface. Tests use a
fake. Caster's kit review waits for the next slice.

**Do:** `meshcontent.Ensure` takes one projected `meshcontent.Entry`
and the executor the session is already bound to. Each required
content-id comes back Present, Checking, or Missing. Present means
that id is on the executor. Checking means a pull is in progress.
Missing with a source starts a pull; the slot stays Checking until
that pull reports Present. `Launch` calls this only when
`SetMeshExecuteSession` installed a session, and it returns before the
existing execute path while any required slot is Checking. `LaunchOn`
ensures a launchable FPGA entry on the target that call will execute
on. A named target or a changed selected target that is not that
executor is rejected before any pull. The launch captures that target
once; Ensure and bind both use the capture, so a settings change
during the call cannot bind a different node. A changed address or
TargetID on that same name is rejected, and the captured client stays
on an unchanged endpoint. Launch also refuses to program when the
selected package, media, firmware, ROM, or expansion composition no
longer matches the row Ensure checked. A foreign-kit denial for that
FPGA launch returns before Ensure. Host-only play stays on the
installed session node.

`MeshExpansion.Digest` is the slot-bytes digest: SHA-256 of that
slot's own bytes (`expansion.Manifest.CartSHA256`). It is not
`Asset.ID`, not the archive `media_id`, and not `ProgrammedSHA256`.
`ExpansionSlotBytesID` names it. Primary media uses `PrimarySourceID`:
the format-3 source `MediaID`, which the executor records as
`SourceSHA256`. Ensure does not read `ProgrammedSHA256`. Expansion
bytes stay separate content-ids. `Executor.LinkExpansion` links them
on the executor. The host does not pre-link them.

`ReadyHere` checks package ABI id and major as well as package id. An
unlisted ABI is no capable executor. The same ABI id at another major
is version skew. Package id alone is not eligibility. Rooms,
`GET /api/v1/games`, and `discovery.ReadyForBoundExecutor` still do
not call `ReadyHere`.

**Failure class:** `ErrContentMissingNoSource`. A required content-id
that is missing on the bound executor and has no source. Fail closed.
It is not `ErrExecuteBlocked` and it is not `ErrUnboundNode`.

**Does not:** automatic placement. Does not pull onto a node the
session did not bind (`ErrUnboundNode`). Does not free or change a
lease. Does not move bytes through mister-agent. Does not add a host
route or a wire freeze. JSON tags stay on the host catalog shape.
Ensure results have none. The hash algorithm stays the unsigned sha256
strawman.

### 4. Kit content store — not started

**Owner:** FogCast target agent. Caster reviews this boundary.

**Do:** Implement `meshcontent.Executor` on the kit the session bound.
Hold content-ids, pull bytes from a content source onto that kit, and
link expansion slot-bytes there. Report Present, Checking, or Missing.
Honor the same failure class. Do not program the FPGA while a required
slot is Checking.

**Does not:** automatic placement. Does not pull onto a node the
session did not bind. Does not free a lease. Does not turn rooms Ready
on. That is the following slice.

### 5. Phase 2 Ready — not started

**Owner:** FogCast host for the predicate. Foggy for the sofa copy.

**Do:** Rooms Ready uses the Phase 2 rule for a mesh session: Execute
binding, every required slot ensured on that executor, lease free,
mesh major OK. Distant-only is Unavailable with a next action. Copy
stays the five-way split in
[mesh LAN](mesh-lan.md). Phase 0 and Phase 1 sessions that are not
asking for the mesh content contract keep today's composition Ready.

**Does not:** Ready from an Execute advertisement alone. Does not
Ready from bytes that only exist on some other LAN node.

---

## Phase 0 / Phase 1 floor

Empty-body session stop still releases. Soft-stop still retains.
The kit lease, DNS-SD advertisements, and `GET /api/v1/mesh/nodes`
stay as Phase 1 left them. The mesh ensure seam stays off unless a
caller installed a session, so a room with one shell and one kit
launches as it does now. Another node's Execute advertisement still
does not make a row Ready.

---

## Parked for Deano

**Per-slot hash algorithm.** Not locked.

Strawman, still unsigned: SHA-256 over that slot's bytes, lowercase
hex, text form `sha256:<64 hex>`. The same digest family as today's
catalog content hash and core-media id, with an algorithm prefix so
the value is not a title id and not a package id. Package / ABI stays
the described package identity plus ABI id and major.

`internal/meshcontent` rejects any other algorithm. A later lock can
add one without treating this strawman as signed. Do not describe the
strawman as Deano's choice. Size, extension, and filesystem path stay
cache details on the node that holds the object. They are not the id.
