# Mesh LAN — Phase 1 execution brief

**Status:** Phase 1 in progress. Bob is driving mesh development and
starts phases when ready. Deano owns FES parent merges.

**Audience:** FogCast host/agent (Caster lane for kit and agent), rooms
UX (Foggy), and anyone picking up a later slice. Bob coordinates. Read
the locks first. This brief names owners, slices, and the Soft-stop
flag. It does not reopen the design.

**Base:** FES `main` tip `428dd224` (mesh draft PR #131).

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

### 1. Soft-stop retains the kit lease — done

**Owner:** FogCast host session API and tenfoot client.

**Policy** ([mesh-lan.md](mesh-lan.md) Decision 4, rooms Scenario 1):

| Action | Lease |
| --- | --- |
| Soft-stop (rooms B / tenfoot session stop that returns to the same room) | **Retain** after cleanup to idle |
| Explicit user Stop (full stop / release ownership) | **Release** after cleanup |
| Development `stop` / replacement Stop | **Retain** (already kit-sharing; unchanged) |

**Flag.** Both actions are `POST /api/v1/session/stop`. The body
distinguishes them:

- Empty body, or JSON with `retain_lease` absent or false: explicit
  user Stop. After idle cleanup the host calls `ReleaseKitLease`.
  CLI `fogcast stop`, the browser library stop, and the kit-grid stop
  stay on this path.
- `{"retain_lease":true}`: sofa Soft-stop. Idle cleanup does not
  release. Tenfoot now-playing stop (East/B, Esc, Backspace, and `s`
  outside play-HID letter entry) sends that body. Client stamps stay
  on the existing `X-FogCast-Client-*` headers or the same JSON object.
- A stop that does not reach idle, including `reboot_required` /
  `stopping`, does not release, with or without the flag.
- Failed cleanup does not release.
- Host process shutdown still closes the live grant. Retain is for the
  room stay, not for a dead host.
- Unknown JSON fields and a non-boolean `retain_lease` stay
  `BAD_REQUEST` and do not touch the lease.

**Success:** host unit tests cover release versus retain, a non-idle
stop, and a rejected body. Tenfoot posts `retain_lease: true`. No kit
HIL required for this slice.

**Does not:** change `LoadIdle()`, Path B, development `kit.py stop`,
or replacement Stop.

### 2. Capability advertisements — not started

**Owner:** FogCast target agent discovery, then the host that reads it.

**Do:** DNS-SD / discovery TXT grows a capability bag and a
mesh-protocol version. Kits generalize `target_id` toward node-id.
A DisplaySink advertisement means the node can present. It does not
mean the picture is healthy.

**Does not:** list titles, carry credentials, or carry lease secrets.
Does not make Ready follow an advertisement from some other node.

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
