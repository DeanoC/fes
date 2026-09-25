# Mesh LAN — Phase 3 execution brief

**Status:** Phase 3 not started. This brief is the slice order. It does
not place a title, and it does not merge code. Bob coordinates. Deano
owns FES parent merges.

**Audience:** FogCast host (placement policy), rooms UX (Foggy) when
sofa copy lands, and Caster when a later slice touches kit or execute
binding. Read the locks first. This brief names owners, slices, and
the unsigned placement strawman. It does not reopen Phase 1 or Phase 2,
and it does not freeze a wire format.

**Base:** FES `main` `019f3168`. Phase 1 closed at `3d34b6e0`
(Soft-stop #132, capability advertisements #134, host inventory and
in-use #137). Kit Soft-stop / ads / inventory smoke closed green
2026-09-25. Phase 2 Slices 1–9 wiring is on main via #173
(`2ebab245`). Slice 9 kit HIL closed green 2026-09-25 (H1–H5; H6 N/A;
H7 documented). #207 (GET `/games` versus Launch session lock) stays
parked. Acceptance in [`mesh-phase1.md`](mesh-phase1.md) and
[`mesh-phase2.md`](mesh-phase2.md) stays as written.

---

## Goal / non-goals

**Goal:** automatic placement, plus an optional advanced override.
Given a title and the nodes this host already knows, choose the
Execute node and the DisplaySink and InputSource that belong on that
same node. The default sofa path does not ask which machine.

The working order is [Decision 7](mesh-lan.md#7-placement-policy) in
[`mesh-lan.md`](mesh-lan.md). Deano has not locked that paragraph.
Treat it the way Phase 2 treated the sha256 content-id: an unsigned
strawman. Do not describe it as Deano's choice. Which `native_emu`
node wins when several can run the title stays open. See Parked,
below.

**Does not deliver:**

- Routable I/O (Phase 4). Picture, audio, and pad stay on the chosen
  Execute node. A binding that sends them to some other node is out
  of this phase.
- Kit-as-Shell, or host-as-Execute (Phase 5).
- A second lease type beside the existing kit lease.
- Turning `[mesh] ensure` on by default. That waits on #177.
- Ready because some other node advertises Execute.
- A V4L2 or ShadowCast-class preview counted as DisplaySink.
- A change to `LoadIdle()` / `reboot_required`.
- A default winner among several `native_emu` nodes.

Phase 0 stays the floor. Phase 1 and Phase 2 stay the floor on top of
it. One configured host launching an installed package on one kit must
keep working. Advertisements still do not list titles. Advertisement
silence is not a lease release.

---

## Locks (do not reopen)

| Doc | What it already decided |
| --- | --- |
| [Mesh LAN](mesh-lan.md) | Product intent, phases, Decision 4 lease policy. Decision 7 is the working placement text and is unsigned. |
| [Mesh node protocol](mesh-node-protocol.md) | Phase 3 names Execute, Display, and Input on the session. Lease conflict rejects. Bindings stay local to the chosen nodes. Ensure completes before execute. |
| [Phase 1 brief](mesh-phase1.md) | Soft-stop retains. Capability ads. Host inventory. In-use copy. |
| [Phase 2 brief](mesh-phase2.md) | Content-id, ensure on the bound executor, Phase 2 Ready. sha256 stays an unsigned strawman. `[mesh] ensure` defaults off. |
| [Kit sharing](kit-sharing.md) | The only FPGA lease |
| [Launch composition](../sources/FogCast/docs/launch-composition.md) | Core, firmware, primary media, expansion slots |

---

## Owners

Owners are component strawmen. Agree files before parallel edits
([agent workflow](agent-workflow.md)).

| Workstream | Strawman owner | What Phase 3 changes | What it must not do |
| --- | --- | --- | --- |
| **Coordination** | Bob | Slice order and this brief | Parent merge. That stays Deano's. |
| **Host / placement** | FogCast host | Policy selection, then a host-local record of preference and last sink, then attaching a decision that matches the bound executor | Kit mutation. A second lease. Routable I/O. Flipping `[mesh] ensure`. |
| **Rooms UX** | Foggy | Sofa stays quiet when policy selected one node. Unresolved does not launch. | A default "which machine" prompt. Collapse Checking, Missing, Needs a choice, In use, and version skew into one string. |
| **Cast / kit** | Caster, only when a slice binds a different executor | Later apply onto a kit the session was not already bound to | Power, image, or SD mutation. Remote reboot. Kit-as-Shell. |
| **Parent merge** | Deano | Merge to `main` | — |

Slices 1–5 do not touch the kit. Slice 6 does. Caster reviews that
boundary, and the kit HIL needs Deano present. Slice 7 does not start
until Deano locks the `native_emu` tie-break.

---

## Ordered slices

### 1. Host-only placement policy selection — first slice

**Owner:** FogCast host. New package `internal/meshplace`. Package
tests only.

The function takes one projected `meshcontent.Entry` and the candidate
nodes the caller already has. It does not browse DNS-SD, dial a kit,
read a config file, or import the target agent. Candidates carry the
fields Phase 1 inventory already stores: node id, mesh-major OK, and
`discovery.Capabilities` (Execute kind plus ABI id and major,
DisplaySink, InputSource). Address, human name, and inventory order
are not ranking keys. Household display preference and last play
DisplaySink are optional node ids the caller passes. Empty means
unset. This slice does not store them.

A host V4L2 or ShadowCast-class preview is not a candidate DisplaySink.
The function has no preview input. `discovery` picture-up stays false.

**Acceptance:**

- Result is host-local. No JSON tags. Not a wire freeze. Three
  outcomes: selected, unresolved, or fail closed. Selected names
  Execute, and DisplaySink and InputSource only when that same node
  advertises them. Display and input are never a different node.
- Unsigned Decision 7, applied as written. Do not describe the order
  as locked.
- Prefer the household display preference when that id is a candidate
  that advertises DisplaySink and can execute the title. Otherwise
  the last play DisplaySink under the same test. A menu shell that
  cannot execute the title does not win because the menu is running
  there. "Near the shell" is not a fallback.
- Launchable `fpga_native`: eligible candidates advertise Execute
  `fpga_native` with the entry's package ABI id and major
  (`meshcontent.ABIMatches`). Picture stays on that kit, so Execute
  and DisplaySink are that node. One eligible kit is selected. If
  several eligible kits exist, the preference or last sink selects
  one of them when it names one of them. If neither names one, the
  result is unresolved. Do not rank the kits. That unresolved FPGA
  case is this brief's reading of the unsigned order so Slice 1 does
  not invent a winner. It is not a new lock.
- Launchable `native_emu` only when no eligible `fpga_native`
  candidate exists. Exactly one candidate that advertises Execute
  `native_emu` is selected. Several such candidates are unresolved.
  Do not pick a winner. That tie-break is parked.
- Fail closed, and do not launch, when the entry is not launchable,
  no candidate can run it, every candidate that could run it has a
  mesh-major mismatch, or the caller reports a required composition
  slot with no source. The function does not open files and does not
  pull.
- Unresolved and fail closed do not invent an Execute node.

**Tests:** `go test` for `internal/meshplace` with fixture candidates.
No kit. No Powerboat. No HIL.

**Does not:** call `ReadyHere` from rooms, `GET /api/v1/games`, or
launch. Does not install a mesh session. Does not flip
`[mesh] ensure`. Does not claim, renew, or release a lease. Does not
mutate a kit, image, or SD card. Does not add a host route. Does not
implement the override. Does not start a `native_emu` mesh claim.

### 2. Optional advanced override — still package-only

**Owner:** FogCast host. Same package.

An optional override node id. Empty keeps Slice 1. When it names a
candidate that can run the title, that candidate is the selection,
including one of several `native_emu` nodes. The override does not
create a default winner for the empty case. Display and input stay on
that same node. A node that cannot run the title, or that fails the
mesh major, does not win by being named.

No sofa prompt. No host route yet. No kit. No Powerboat. No HIL.

**Does not:** lock the `native_emu` tie-break. Does not route picture
or pad to another node.

### 3. Host-local preference and last sink

**Owner:** FogCast host.

Remember the household display preference and the last play
DisplaySink and pass them into Slice 1. The record lives on the host.
Empty stays empty. Updating last sink when a play session starts is
host memory for this slice.

**Does not:** write `agent.toml`, the SD card, or any kit file. Does
not dial the kit. No Powerboat. No HIL.

### 4. Record the decision when it matches the bound executor

**Owner:** FogCast host.

When Slice 1 or Slice 2 selects the executor this session is already
bound to, record that Execute node and its local DisplaySink and
InputSource on the in-memory session. Launch and Ensure keep using
that bind. A selection that names any other node is not applied.
Return before bind, the same refusal family as `ErrUnboundNode`.
Phase 0 and Phase 1 sessions that are not asking for placement keep
today's bind.

**Does not:** move the session to another kit. Does not turn
`[mesh] ensure` on. Does not change Phase 2 Ready while the seam is
off. No kit mutation. No Powerboat. No HIL.

### 5. Rooms stay quiet on a selection

**Owner:** Foggy for sofa behavior. FogCast host for the predicate
the room reads.

When the policy returns selected, Play does not ask which machine.
When the policy returns unresolved or fail closed, the row is not
Ready and Confirm does not launch and does not pick a node. That
state is not the edition "Needs a choice", not version skew, and not
"in use". This brief does not freeze the new string. The five-way
copy split in [mesh LAN](mesh-lan.md) stays intact.

Host and rooms tests. No kit. No Powerboat. No HIL.

**Does not:** treat an Execute advertisement as Ready. Does not show
a capture preview as the sink.

### 6. Bind a different FPGA executor — kit HIL, Deano present

**Owner:** FogCast host. Caster reviews this boundary.

Only after Slices 1–5. The policy has selected a kit that is not the
session's already-bound executor. Claim that kit with the existing
kit lease. Conflict rejects. Confirm still does not steal. Generation
takeover stays the operations path. Ensure runs on that executor only
when an operator has already set `[mesh] ensure = true`. This slice
does not change the default. Picture stays on that kit's HDMI. The
pad that plays is that kit's local pad. A remote pad toward that HDMI
is Phase 4.

**Kit HIL, Deano present.** Do not start it from an unattended agent.
Follow [kit sharing](kit-sharing.md) and the designated kit in the
selected FogCast `docs/DEVELOPMENT.md`. Release the lease when the
check ends. Cover: the chosen kit is the one that plays; a second
shell sees in use and does not take the lease; Soft-stop retains;
empty-body stop releases; a preview on the menu host is not the
DisplaySink; `[mesh] ensure` left unset still takes the Phase 0 and
Phase 1 launch path.

**Does not:** write an image or SD card. Does not remote-reboot. Does
not add a second lease type. Does not implement kit-as-Shell.

### 7. `native_emu` default tie-break — do not start

**Owner:** FogCast host after Deano locks the parked question.

No slice work until that lock exists. Slice 2's override may name one
node. The empty-override case stays unresolved. A later lock can add
one default without treating this brief as the choice. The protocol
strawman still says a `native_emu` executor grows a mesh claim in the
phase that places native play. That claim waits on this lock. It is
not a second kit lease and it is not Phase 5 host-as-Execute. If the
chosen node is a kit, that follow-up needs Caster and a kit HIL with
Deano present.

---

## Phase 0 / Phase 1 / Phase 2 floor

Empty-body session stop still releases. Soft-stop still retains.
The kit lease, DNS-SD advertisements, and `GET /api/v1/mesh/nodes`
stay as Phase 1 left them. Content-id, ensure-on-the-bound-executor,
and Phase 2 Ready stay as Phase 2 left them. sha256 stays the
unsigned strawman in [`mesh-phase2.md`](mesh-phase2.md).

`[mesh] ensure` still defaults off until #177 (legacy fallback when
the source does not advertise) lands. An unset key or `ensure = false`
keeps the Phase 0 and Phase 1 path. No Phase 3 slice flips that
default. Kit-local pad (#172) is already on main (#179, #187, #192)
and is not a Phase 3 dependency. Do not reopen that feed.

#207 stays parked. Placement work does not pick up the GET `/games`
versus Launch session lock.

Another node's Execute advertisement still does not make a row Ready.
Advertisement silence still does not release the kit lease.

---

## Parked for Deano

**Placement order.** Not locked.

Strawman, still unsigned: [Decision 7](mesh-lan.md#7-placement-policy) in
[`mesh-lan.md`](mesh-lan.md). Household display preference or last
play sink first (living-room kit HDMI over a Mac shell). FPGA execute
uses that kit's DisplaySink. Near the shell only for the same seat or
a Phase 4 captured remote sink. A V4L2 or ShadowCast-class preview is
not a DisplaySink. The default sofa path does not ask which machine.
An optional advanced override exists for a power user.

`internal/meshplace` implements that strawman as Slice 1 and does not
treat it as signed. A later lock can change the order in
[`mesh-lan.md`](mesh-lan.md) without a second design. Do not describe
the strawman as Deano's choice.

**Which `native_emu` node wins.** Not locked.

When several nodes can run the title and no `fpga_native` candidate
can, Slice 1 returns unresolved. It does not rank those nodes. Slice 2
selects one only when the override names it. Do not add a default
tie-break in this phase until Deano locks the sentence.
