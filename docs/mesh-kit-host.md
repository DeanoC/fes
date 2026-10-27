# Mesh under kit-as-host

**Status:** approved target architecture, 22 October 2026; not deployed.
[#863](https://github.com/DeanoC/fes/issues/863) remains open with no
implementation or acceptance evidence. This page supersedes the kit-side
placement, pairing and
paired-listener designs in [`mesh-lan.md`](mesh-lan.md),
[`mesh-node-protocol.md`](mesh-node-protocol.md),
[`mesh-phase2.md`](mesh-phase2.md), [`mesh-phase3.md`](mesh-phase3.md) and
[`mesh-vnext.md`](mesh-vnext.md) §3–§4. Deano owns FES parent merges. Items
marked *proposed* are not yet decided.

## What changes

- Each kit runs its own `fogcast-api` on `127.0.0.1:8787` and is its own
  owner. Its `fogcast-tenfoot` is an unpaired client of that loopback API. The
  kit-local launch path (`internal/localcores`, the local cartridge loader,
  the agent's `-local-control` launch, kit-local catalog serving) is deleted
  in #863 slice 7, with its tests and docs. The separate, authenticated
  launcher listener remains the only LAN control surface; it is not used by
  the kit-local tenfoot client.
- Powerboat, a NAS and desktops are library and content sources. A source is
  never an owner; a second owning host is still refused (Q16).
- The kit host has exactly one executor: its own agent at `127.0.0.1:8182`.
  `[[targets]]` is one loopback row. Placement (`mesh-lan.md` §7 Decision 7,
  `mesh-phase3.md` slices 1–8), `display_preference`, `placement_override` and
  last-sink memory do not apply on a kit. A desktop client that can see several
  kits chooses one through that kit's authenticated launcher listener; nothing
  on a kit places onto another kit.
- Security (updated 22 October): a local LAN is not an authorization boundary.
  Keep the general host API loopback-only; `fogcast-api` currently enforces
  this in `normalizeListenAddress`. Never expose that API on all interfaces
  without authentication. Remote control uses the separate launcher listener,
  which requires a per-kit bearer and `X-FogCast-Target-ID` and exposes only
  its allowlisted operations (no settings, filesystem, or development-RBF
  routes; see [`launcher-host.md`](../sources/FogCast/docs/launcher-host.md)).
  Keep the general API's Host-header restriction. Discovery does not grant
  access. Do not reuse the host-to-agent token as a launcher credential.

## Decisions (22 October 2026)

1. **Process.** `fogcast-api` is a fourth child of the base supervisor,
   found by role, restarted alone like the launcher. Its death never stops the
   agent, the runtime or a running core. This is a base-image change and ships
   in the first image cut of #863.
2. **Binary.** One multi-call binary, `fogcast-kit host` and
   `fogcast-kit tenfoot`, as two processes. The bundle grows by one binary,
   not by the 17 MB a separate `fogcast-api` measured in #864.
3. **Remote sources.** The FogCast library API, served by a read-only LAN
   listener on the source. The kit host indexes a source's titles into its own
   catalog as rows that carry provenance (source id, remote content id) and
   fetches content objects into its package store on first play; cached
   content lives on FESDATA3. This revises `mesh-lan.md` "Combined library
   contract" decision 2: a remote title becomes a local row. Mounted folders
   (USB, SMB/NFS) are plain library roots and come later.
4. **Sessions.** `mister-agent` holds the lease for sessions the local host
   starts, with a grace window that spans a host restart; tenfoot reattaches
   to the running session through the restarted host. A host or UI restart
   never ends a game. A desktop launch goes through the kit host and gets the
   same treatment.
5. **LAN exposure.** The general host API stays on loopback. Remote control
   uses only the separate authenticated launcher listener and its allowlisted
   operations. An unauthenticated all-interface host API is unsupported.

## What stays

- Phase 1 as delivered: Soft-stop, `_fogcast._tcp` advertisement with
  `node_id`, `target_id` and `cap`, and the host inventory. Desktops and
  sources use it to find kits.
- The #379 decision 1 titles projection, `GET /api/v1/library/titles`. It is
  the remote-source read.
- Leases, Soft-stop retention and explicit Stop release
  ([kit sharing](kit-sharing.md)).
- The read-only LAN library listener on Powerboat, and `ui/localfeed`, which
  carries pad input for host sessions.
- Hostnames `mister-<mac3>` (IMG-2/IMG-3) for discovery.

## What is historical

| Where | Standing after 22 October |
| --- | --- |
| `mesh-lan.md` §7 placement policy; "Kit-as-Shell" as an optional deployment mode; "Combined library contract" decisions 2 and 4 | Superseded by this page |
| `mesh-node-protocol.md` "Kit content source reads the host"; the host node-id strawman | Superseded: content reads go from the kit host to a source |
| `mesh-phase2.md` ensure seam, still gated on #177 and #172 (both closed) | Re-scoped: ensure means fetch from a remote source |
| `mesh-phase3.md` slices 1–8 | Cancelled for kits; `meshplace` may be reused by a desktop client to pick a kit |
| `mesh-vnext.md` §2.4b and §4 P4a launcher credentials; P1–P3 PAKE; P4b; Q1–Q3, Q8, Q15, Q17 | Cancelled or moot; Q16 is revised in place |
| `mesh-m1-integration.md` | Evidence for host-local RetroArch; it does not transfer to the kit profile |
| `idle-menu-rooms.md` "kit-as-host is optional" | Replaced: kit-as-host is the product |

## Open

- *Proposed:* static source configuration on the kit first; a source
  advertising Catalog/Content capabilities on its LAN listener can follow.
- Remote desktop control uses the authenticated launcher listener, not the
  unrestricted host API. Define the kit-host pairing and credential delivery
  before enabling that client path; pointing a browser at the host URL alone
  does not authorize it.
- Powerboat `host_only` RetroArch sessions and casting onto a kit are not a
  supported path once slice 6 lands; whether host emulation stays in the
  product at all is the re-opened #871 decision.

## Acceptance

#863's checks: focused FogCast tests for the kit profile and local-library
roots; a kit HIL under the kit-sharing rules with Powerboat unplugged (boot to
Home, import firmware, launch a ZX81 and an SMS title, Stop and return) and
with Powerboat up (a remote-source title is listed and plays); frame timings
within the existing tenfoot budgets under #811 tracing. Security checks must
also show that the general API is not reachable on a LAN interface, the
launcher listener rejects missing or mismatched bearers, and unlisted routes
such as settings remain unavailable there.
