# Mesh LAN — Phase 1 execution brief

**Status:** Phase 1 in progress. Bob is driving mesh development and
starts phases when ready. Deano owns FES parent merges.

**Audience:** FogCast host/agent (Caster lane for kit and agent), rooms
UX (Foggy), and anyone picking up a later slice. Bob coordinates. Read
the locks first. This brief names owners, slices, and the Soft-stop
flag. It does not reopen the design.

**Base:** FES `main` tip `81fe16ab` (Slice 1 Soft-stop lease retain, #132).
Slice 2 capability advertisements are the PR on top of that tip.

---

## Goal / non-goals

**Goal:** see the nodes. Multi-node discovery and capability
advertisements. Still one active Shell. Kits remain FPGA executors.
Ready stays Phase 0 composition against the **bound** executor. A
second shell that sees that kit leased shows Unavailable "in use" and
does not take the lease.

**Does not deliver (Phase 2+):** federated catalog, placement, routable
I/O, Ready because some other node advertises Execute, a second lease,
remote reboot, or a change to `LoadIdle()` / `reboot_required`.

Phase 0 stays the floor. One configured host launching an installed
package on one kit must keep working. Mesh claim for FPGA is the
existing kit lease. Advertisement silence is not a lease release.

---

## Locks (do not reopen)

| Doc | What it already decided |
| --- | --- |
| [Mesh LAN](mesh-lan.md) | Product intent, phases, Decision 4 lease policy |
| [Mesh node protocol](mesh-node-protocol.md) | Capability, lease, content-id, and failure classes |
| [Kit sharing](kit-sharing.md) | Claim, renew, expiry, takeover; development `stop` retains |
| [Rooms experience](../sources/FogCast/docs/rooms-experience.md) | Scenario 1: Play, then B, same room |
| [Soft-restart Path B](soft-restart-path-b.md) | `reboot_required` stays kit-local and on hold |

---

## Owners

Owners are component strawmen. Agree files before parallel edits
([agent workflow](agent-workflow.md)).

| Workstream | Strawman owner | What Phase 1 changes | What it must not do |
| --- | --- | --- | --- |
| **Host / agent** | FogCast host and target agent (Caster lane) | Session-stop lease split, then discovery advertisements and the host's node list | A second lease authority. Remote reboot. `LoadIdle()` / `reboot_required` changes. |
| **Rooms UX** | FogCast tenfoot (Foggy) | Now-playing stop keeps the lease and returns to the same room. Later, a second shell's in-use copy. | Steal a leased kit. Treat an Execute advertisement as Ready. |
| **Coordination** | Bob | Slice order and the parent brief | Parent merge. That stays Deano's. |

---

## Ordered slices

### 1. Soft-stop retains the kit lease — done on main

Landed as #132 (`81fe16ab`). Do not reopen `retain_lease`, Path B,
`LoadIdle`, or `reboot_required`.

**Owner:** FogCast host session API and tenfoot client. Caster locked
this acceptance for the kit contract.

**Acceptance:**

- Soft-stop = rooms B/Back while session active → agent/runtime Stop → defined idle (LoadIdle path). NOT Select+Start. NOT lease release.
- After Soft-stop: kit lease RETAINED by the same coordinator/owner. Release only on explicit shell leave / session end / EOF / release API — not Soft-stop.
- Sofa/session record: clear “playing” / active play so Soft-stop→idle is visible; lease may still show held. Do NOT map Soft-stop to “Lease held” Unavailable for the same shell.
- Conflict still rejects; Confirm does not steal. Ops takeover stays generation/reason path off Confirm.
- Phase 0 bind + POST session/launch unchanged. No new mesh lease object. No remote reboot / Path B invent.
- reboot_required / idle recovery stay kit-local unchanged.

**Routes that already exist.** No second lease object.

- Rooms B/Back while the session is active, and the sofa aliases that
  share that room return (Esc, Backspace, and `s` outside play-HID
  letter entry), post `POST /api/v1/session/stop` with
  `{"retain_lease":true}`. The host still runs service Stop: agent
  `POST /v1/stop`, then the runtime LoadIdle path. It does not call
  `ReleaseKitLease`. The same kit-lease token stays with that owner.
- Kit Select+Start is not Soft-stop. It posts an empty body on the same
  session stop route. That is explicit session end and releases after
  idle.
- Release stays on the existing paths: empty-body session stop (CLI,
  browser library, kit Select+Start), host process shutdown, and
  `POST /v1/kit/release` from kit.py `release` or EOF. Soft-stop is
  none of those.
- The recorded session event is `session.stop` with state `idle`, so
  playing is cleared. The same owner's held lease remains a lease
  strip. It is not Unavailable for that shell. A foreign holder is
  still "held by another session"; Confirm does not claim over it.
  Takeover remains `POST /v1/kit/takeover` with the current generation
  and a reason.
- `POST /api/v1/session/launch` is unchanged. A stop that does not
  reach idle, including `reboot_required`, does not release and does
  not add a reboot.

**Tests:** host session stop covers retain versus explicit release and
an idle session record; service Stop keeps the same lease token until
`ReleaseKitLease`, including after an idle selected-target change drops
that kit's client; an already-idle explicit Stop still releases when the
newly selected target's probe or Stop fails; invalidating one target
leaves another retained grant in place; a failed release leaves that
grant for retry; an owned held lease after idle stays ready; kit
Select+Start posts an empty stop body. Kit HIL is optional and does not
block merge.

### 2. Capability advertisements — this PR

**Owner:** FogCast target agent discovery, then the host that reads it.

**Acceptance:**

- DNS-SD TXT keeps Phase 0 `protocol` and `target_id`. It adds `node_id`
  (the same stable id; no second id is minted), `mesh` (`1.0`), and `cap`.
- The kit bag advertises Execute `fpga_native` and DisplaySink. InputSource
  is included for the kit's local pad path. ABI / package-family suffixes
  are encoded when the caller knows them; the agent omits them because it
  does not inventory packages before it announces. An empty family list is
  not "any RBF". Catalog, Content, Shell, and Coordinator are omitted.
- DisplaySink means the node can present. Parsing it does not report HDMI
  or ADV liveness, and it does not mean the picture is up.
- The host reader parses the new fields additively. Phase 0 TXT that omits
  `mesh` stays directly bindable. A mismatched or malformed mesh major does
  not remove that bind; a session that needs the mesh contract fails closed
  on the major.
- Advertisements carry no credentials, no title list, and no lease secrets.
- Ready stays Phase 0 composition against the bound executor. Another node's
  Execute advertisement does not make a row Ready.
- No TTL number is emitted. The protocol strawman still leaves seconds
  unsigned, so this slice does not start an advertisement clock. A peer
  `ttl` key is parse-only. Silence past that value is absence for a future
  placement choice only. It does not release the kit lease.

**Does not:** list titles, federate the catalog, add a second lease, reboot
remotely, or change kit power, image, or SD card contents. Host node
inventory and the second-shell "in use" copy stay Slice 3.

### 3. Host node inventory and second-shell in-use — not started

**Owner:** FogCast host, then rooms UX for the Unavailable copy.

**Do:** The host collects advertisements. An optional second shell /
observer shows a leased kit as Unavailable "in use" and does not claim
it. The active Shell's Phase 0 bind is unchanged.

**Does not:** silent steal. Does not free the kit because an
advertisement went quiet.

---

## Phase 0 floor

Empty-body session stop still releases. Launch, replacement Stop,
development `stop`, and the kit lease claim/renew/expiry/takeover paths
are unchanged. A room with one shell and one kit launches as it does
now.
