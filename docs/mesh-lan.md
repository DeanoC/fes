# Mesh LAN (design draft)

**Status:** merged as #131. Phase 1 closed on main `3d34b6e0`. Phase 2
execution is [`mesh-phase2.md`](mesh-phase2.md). Phase 3 execution is
[`mesh-phase3.md`](mesh-phase3.md): Slices 1–6 are on main, and host
production placement sits behind `[mesh] placement`, default off.
Deano locked Decision 7, the placement order, on 2026-09-27.
Bob coordinates; Deano merges
parents. Still edit strawmen in place. Caster's cast/kit
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

### Delivery direction agreed 2026-10-01

Deano's current priority is a coherent, reliable FES experience on real
hardware. The visible library and resources are the sum of the mesh. A Linux
FPGA kit can also provide host, shell, catalog and coordinator services within
its measured hardware limits. Linux, Mac, PC and Raspberry Pi nodes may have no
FPGA and provide software emulation, expanding the games available or improving
their execution. A later simple non-Linux kit may provide fewer capabilities.
These are deployments of capabilities, not permanent node roles.

A title has execution alternatives; the title is not its FPGA or emulator
backend. Execution, display and input can eventually live on different nodes.
A second kit may play independently, act as a remote display, or resume progress
in another room. For progress portability, first save on A and relaunch/restore
on B; live-state migration is a later problem.

Supported v1+ cores provide usable contracts and can gain features over their
lifetime. Custom games and development cores should use the same system.
Compiler selection should follow latest main automatically, retaining a
last-working revision only for a demonstrated per-core regression and retesting
that exception. Actual builds still record immutable compiler/source identities.
This is a delivery policy target, not a claim that automation already exists.

[M1: Heterogeneous two-node mesh](https://github.com/DeanoC/fes/milestone/1)
and its [outcome issue](https://github.com/DeanoC/fes/issues/357) track the first
accepted slice: one FPGA kit and one Linux or Mac software execution node,
one visible library, truthful availability, independent session/input/Stop
ownership, and exact-artifact real-node acceptance. Portable saves and remote
display routing follow M1. The earlier phase records below retain their dated
implementation evidence; they do not establish this new acceptance outcome.

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

Present tense, verified against the FES source at this worktree's tip.
The existing host-to-kit launch path is still a direct bind; kit DNS-SD
discovery now advertises the implemented mesh capability contract described
below. The broader federated library and remote software-runner behavior are
not implemented.

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
it with local DNS-SD (`_fogcast._tcp`). Current kit TXT carries
`protocol=1`, `target_id`, matching `node_id`, `mesh=1.0`, and a capability
bag containing `execute:fpga_native`, `display_sink`, and `input_source`.
It does not carry ABI families, a catalog, credentials, or lease secrets.
`EncodeKitTXT` does not emit a TXT `ttl`; the parser accepts an optional
`ttl`, and DNS-SD browse expiry governs inventory presence independently of
the kit lease. The host adopts a new address only after authenticated
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

## M1 two-node contract (review baseline)

This is the concise contract for #358–#364. “Implemented” is tied to the
listed FES source/test or the dated HIL record; everything else is a
proposal or gap. The mesh discovery and placement code is not a claim that
two-node software-runner acceptance is complete.

| Concern | Implemented today | M1 contract / remaining gap |
| --- | --- | --- |
| **Capability/version** | Kit DNS-SD advertises mesh `1.0`, `fpga_native`, `display_sink`, and `input_source`; no ABI families in TXT. Host placement reads ABI families from the authenticated kit content document. The host `/api/v1/health` also reports mesh `1.0` and configured `native_emu` core/system identity, configured core version label, and observed SHA-256. The version is copied from config; FogCast does not query RetroArch for it. See `sources/FogCast/internal/discovery/mesh.go`, `fogcast/mesh_place_wire.go`, and the health handler. | Mesh major mismatch is ineligible; unknown optional minor fields are ignored. The software capability is visible from the host health response, but host-to-host discovery/pairing and federated placement are not implemented. Do not advertise shell/catalog/coordinator unless the node actually serves them. |
| **Title/backend** | Catalog entries preserve `game_id`; host execution uses `host_only` with the host RetroArch adapter, while FPGA uses `fpga_native`. `Service.MeshBackendLibrary` projects the local catalog and kit inventory. It groups options by catalog game id and links rows at read time when the ROM sha256 and system match exactly: the package row's game id is canonical and the raw ROM folds in as its emulator option (more rows: package first, then lowest game id; a hash or system mismatch never links). Local Data Storm is one row with the kit `fes.sms` option and the host-local emulator option. The projection does not claim session Ready. `GET /api/v1/library/titles` serves that projection. `content_sources` node ids stay empty until #396, and a remote `native_emu` node stays unverified. See `fogcast/mesh_library.go` and `internal/meshcontent/content.go`. | **Proposed:** publish the combined library with source provenance and select one backend explicitly per session. The wire, provenance, content-id lock, and remote `native_emu` eligibility that were **GAP #360** and **GAP #361** are the contract in [Combined library contract (#379)](#combined-library-contract-379). Phase 2 serves the route. Source provenance waits on #396. Remote `native_emu` eligibility stays unavailable until #298 plus the decision 4 enrollment prerequisites. |
| **Node identity/pairing** | Kit identity is its existing persistent `target_id`; DNS-SD repeats it as `node_id`. Host reconciles discovery with configured targets and authenticated health. The launcher listener supports per-`target_id` credentials after #287; this is not the proposed user pairing flow. See `fogcast/discovery.go`, `internal/discovery/mesh.go`, `docs/mesh-vnext.md` §2.1, and `internal/hostapi/launcher.go`. | **Proposed:** the kit keeps that ID through capability changes. A software runner's `software_backends` are read on that runner's launcher listener with a launcher pairing bearer, and only at the origin recorded at pairing. The bearer is bound to a host node id (#298 item 2). Runner admission and provisioning are further prerequisites in decision 4. This proposal implements none of them, and remote `native_emu` stays unavailable until #298 and those prerequisites. Do not add a permanent Host/Kit identity enum. The v-next PAKE flow is proposed, not implemented. Decision 4 is the contract. |
| **Availability** | Inventory expiry removes a node from a future placement choice but does not release its lease. Room states are Checking/Missing/Needs a choice/Unavailable/Ready. Lease and target health are separate. See `internal/discovery/mesh.go`, `fogcast/mesh_ready.go`, FogCast `docs/rooms-experience.md`. | **Proposed:** report Checking while capability, backend, composition, or required content is unresolved; Ready only for the selected usable execution option; otherwise Unavailable with reason. Software-runner health/readiness and honest UI across both nodes are **GAP #359/#360/#362**. |
| **Ownership/admission** | Kit agent lease is 90 seconds, renewed every 20 seconds; target client mutations use that grant. Busy kit launches reject. No second FPGA lease exists. See `docs/kit-sharing.md`, `sources/FogCast/targetclient/kit_lease.go`, `fogcast/mesh_lease_acquire_test.go`. | **Proposed:** each executor session has one owner; admission checks the existing kit lease for FPGA and an equivalent single-session owner on a software runner. Discovery TTL is not ownership. Do not create a second FPGA lease. Independent per-node session/input/Stop and race/failure acceptance remain **GAP #363**. |

### Combined library contract (#379)

**Approved contract, phase 2 in progress.** Phase 1 of #379 records the
four decisions below. Phase 2 serves decision 1 from
`ProjectMeshBackendLibrary` at `GET /api/v1/library/titles`. There is
no second inventory and no second discovery path. Decision 2 provenance
waits on #396: `content_sources[].node_ids` is present and empty, and
this phase does not read `content_ids` from the node document. Decision
4 remote `native_emu` eligibility waits on #298 and the runner admission
and provisioning prerequisites. Those nodes stay unavailable with
`remote emulator system and version unverified`. This phase does not
read `software_backends` and does not send a bearer to a discovered or
re-resolved address. `[mesh] ensure` and `[mesh] placement` stay
default off. The same four decisions are the wire rules in
[`mesh-node-protocol.md`](mesh-node-protocol.md#combined-library-contract-379).

#### 1. Combined library wire shape

**Proposal.** Publish the grouped title to backend-options view as one
read on the existing host API:

`GET /api/v1/library/titles`

The body is a JSON view of `Service.MeshBackendLibrary`. The new fields
are `content_sources` (decision 2), `available` derived from the
existing reasons, and `core_id` copied from the catalog core id the
projection already reads. The route takes no query and no body. A query string is not a filter.
When the local catalog cannot be read, the route returns 500 INTERNAL
`catalog is unavailable` and does not answer `{"titles":[]}`. The kit
launcher consumes this same route.
Phase 2 adds it to the launcher allowlist as a paired library read
(`launcherOperation` and `launcherPairedRead` in
`internal/hostapi/launcher.go`), with the bearer and
`X-FogCast-Target-ID` those reads already require. It is not a mesh
content read. DNS-SD still carries no title list. The GET does not
launch, does not select a session backend, and does not report Ready.

`title_id` is the canonical catalog game id from #361. `source_game_id`
is that option's catalog game id (`Entry.TitleID`). `execution` is
`fpga_native` or `native_emu`. Today's `host_only` label stays off this
object; a host-local emulator option is `native_emu` with
`host_local: true`. `available` is derived: a host-local option is
available when its reason is empty, even if a nested remote node is
not; any other option is available when one of its nodes is.
`core_id` is the catalog core id the projection already uses for browse
system (`fes.sms`). It appears only on a package-backed option. `package`
is the described package id plus ABI id and major. `content_sources` is
filled by decision 2. Empty `reason` fields are omitted. `nodes` is
always present.

Option and node `reason` strings stay the #361 strings:

| Where | Strings |
| --- | --- |
| Option | `local source unavailable`, `no advertised executor in inventory`, `no compatible executor in inventory` |
| Node | `inventory retained after browse error`, `node identity or address is ambiguous`, `mesh protocol major mismatch`, `remote emulator system and version unverified`, `package unavailable on node`, `package ABI incompatible on node`, `executor unsupported` |

Paths, ROM bytes, tokens, artwork, and `ready_here` stay off the object.
Skipped titles stay off `titles`. An empty library is `{"titles":[]}`.

The Data Storm ids below are what `catalog.GameID` produces for the
#361 fixture: core library `core-packages` with relative id
`fes.sms` plus the title, and raw library `sms-root` with relative path
`Data Storm.sms`. A household row uses the same function with its own
library id and path. `package_id` is a 64-hex shape stand-in, not the
#364 package digest. `abi` `fes.application` major 1 is the ABI the
#361 two-node fixture requires on the node document, not a claim about
the shipped `fes.sms` package ABI. The ROM content-id is the real
Data Storm 1.00 digest.

The object below is the projection `Service.MeshBackendLibrary` returns
for that fixture today: the kit `fes.sms` option and the host-local
emulator option. `resolveExecution` returns only `fpga_native` or
`host_only` (`fogcast/service.go`). `host_local` is true only when the
title's execute label is `host_only`, so this service path does not
emit an option with `host_local: false` and `execution: native_emu`.
With only the kit in inventory the host-local option's `nodes` is
empty, because the kit advertises `fpga_native`. When inventory also
contains a `native_emu` advertisement, #361 appends that node on this
same host-local option with reason `remote emulator system and version
unverified`. That node does not clear the host-local option, and it
does not create a third option.

```json
{
  "titles": [
    {
      "title_id": "fpga-data-storm-1-00-39c4d68f01fa",
      "system": "sms",
      "content_ids": [
        "sha256:4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f"
      ],
      "content_sources": [
        {
          "content_id": "sha256:4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f",
          "node_ids": []
        }
      ],
      "options": [
        {
          "source_game_id": "fpga-data-storm-1-00-39c4d68f01fa",
          "execution": "fpga_native",
          "host_local": false,
          "available": true,
          "core_id": "fes.sms",
          "package": {
            "package_id": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
            "abi": "fes.application",
            "major": 1
          },
          "nodes": [
            {
              "node_id": "01234567-89ab-cdef-0123-456789abcdef",
              "available": true
            }
          ]
        },
        {
          "source_game_id": "sms-data-storm-1-00-3558e845cf24",
          "execution": "native_emu",
          "host_local": true,
          "available": true,
          "nodes": []
        }
      ]
    }
  ]
}
```

Phase 2 adds the remote option by a projection step, not by a catalog
row and not by `resolveExecution`. Inside `ProjectMeshBackendLibrary`,
after the local titles are linked, a row that already has a host-local
`native_emu` option gains one synthetic `MeshTitle`: the same game,
system, and primary-media digest, with `Execute` set to `native_emu`.
The existing loop keeps it because `host_local` differs, so the
duplicate check does not drop it. `source_game_id` stays the raw ROM's
catalog id. The option's nodes are the inventory nodes that advertise
`native_emu`. Until decision 4 accepts a node's provenance, each such
node keeps `remote emulator system and version unverified` and the
option reason stays `no compatible executor in inventory`. The
synthetic option is emitted only when inventory advertises `native_emu`.
Kit-only inventory keeps the two-option document above. The
host-local option stays as #361 built it, nested remote nodes included.

```json
{
  "source_game_id": "sms-data-storm-1-00-3558e845cf24",
  "execution": "native_emu",
  "host_local": false,
  "available": false,
  "reason": "no compatible executor in inventory",
  "nodes": [
    {
      "node_id": "fedcba98-7654-3210-fedc-ba9876543210",
      "available": false,
      "reason": "remote emulator system and version unverified"
    }
  ]
}
```

**Alternatives.** A field on `GET /api/v1/games` would fold this view
into the catalog row rooms already parse, including `ready_here`. A
field on `GET /api/v1/mesh/nodes` would put a title list on the
inventory route. A mister-packages schema is not required while the
launcher reads the host; #358 still freezes no shared wire. An
in-process-only view would leave the kit launcher and #362 without a
document. This proposal uses the new library route.

**Out of scope.** Session selection, launch, Ready, a skipped-title
array, display strings, filters, and any change to `GET /api/v1/games`.
`[mesh] ensure` and `[mesh] placement` stay off. No new reason strings.

**Acceptance tests, phase 2.** `GET /api/v1/library/titles` returns one
Data Storm title in the first shape above: the kit `fes.sms` option and
the host-local emulator option, the real ROM content-id, and no path
substring and no `ready_here`. That body has no `host_local: false`
emulator option. A second test runs the synthetic `Execute: native_emu`
title through `ProjectMeshBackendLibrary` and expects the phase-2
option above, with those reason strings, still unavailable. Package
options sort first. A paired launcher GET with bearer and
`X-FogCast-Target-ID`
returns 200; a missing bearer returns 401; the route is not admitted as
a mesh content read. `GET /api/v1/games` is unchanged. A hash or system
mismatch stays two titles. Default config leaves ensure and placement
off.

#### 2. Remote source-provenance contract

**Proposal.** A node states the content-ids it can supply on the
authenticated node content document the host already reads,
`GET /v1/mesh/content/node` (`fogcast/mesh_place_wire.go`,
`kitcontent.ReadNode`). Phase 2 adds an optional `content_ids` array of
canonical content-id strings to that object, beside `node_id`, `abis`,
and `packages`. The host accepts the array only when `node_id` equals
the configured `target_id`. The same read and the same cache
(`placementNodeFacts`) carry it. DNS-SD does not. An omitted or empty
array supplies nothing. An invalid string is dropped and does not fail
the existing `node_id`, `abis`, or `packages` fields.

That read is `readPlacementNodeFacts`. Today `placementReadAddress`
prefers a unique DNS-SD origin, and the kit agent bearer is sent there
before `node_id` is checked. Phase-2 provenance extends that read, so
it inherits the leak. [#396](https://github.com/DeanoC/fes/issues/396)
is the kit-path fix, and it applies the same enrolled-origin-only rule
as decision 4: any bearer goes only to the origin recorded at pairing
or in configuration. A discovered or changed address is never sent
credentials automatically. The owner-facing sentence is `node moved;
re-pair or confirm the new address`. That sentence is not a new
library `reason` string. Confirmation is an explicit config edit or
re-pairing step by the owner, which replaces the recorded origin.
After that edit, reads send the bearer to the new origin. **Phase-2
provenance must not ship before #396 lands.** This proposal does not
change `placementReadAddress`.

A configured hostname is the same rule. `scripts/prepare_launcher.py`
(`validate_address`) accepts a hostname and writes an `http://` origin.
At enrollment or configuration the implementation resolves that name
once and records the IP beside it. A bearer request connects only to
the recorded IP and sends the configured `Host` header. If the name
later resolves to a different address, the shell sends no
`Authorization` and surfaces `node moved; re-pair or confirm the new
address` until the owner confirms. An IP-literal origin is not
re-resolved. #396 applies this on the kit path. The threat model is
discovery spoofing and DNS or mDNS spoofing. An on-path LAN attacker
who ARP-spoofs the recorded IP, on plain HTTP, is out of scope until
the pinned-TLS option.

The per-id `advertises` answers (`GET /v1/mesh/content/source`,
`GET /v1/mesh/content/slots`, and the host
`GET /api/v1/mesh/content/source`) stay the Ensure check for one id.
The library does not use them to discover titles.

Reconciliation stays on the local catalog:

- The local catalog is the only title list. A remote content-id does
  not create a row.
- The #361 link rule is unchanged: primary-media content-id and system
  must both match. The package row's game id is canonical. A different
  hash or system does not link.
- A remote id that equals a row's slot content-id adds that `node_id`
  to `content_sources` for that id. Package ids stay on `packages` and
  are not content sources.
- Two nodes supplying one id are two `node_ids` on one slot, in
  inventory order, deduped. They are not two titles.
- This host's own bytes are the catalog row. They do not get a
  synthetic node id. A host installation id is #298 item 2. Until that
  id exists, a remote host cannot appear in `node_ids`. A kit already
  has `target_id` and does not wait on #298.
- Paths, sizes, and extensions stay off the view.

**Alternatives.** Probing every catalog id with the per-id source read
would reuse a check Ensure already has, and it would not be the node
stating its set. A DNS-SD content list would put the library on
discovery. Publishing remote-only ids as new titles would be a second
catalog. This proposal extends the one node document and ignores ids
the local catalog does not already name.

**Out of scope.** Byte pull, turning ensure on, a cursor for very large
`content_ids` lists, minting the #298 host id, and changing
`placementReadAddress` (that change is #396).

**Acceptance tests, phase 2.** A second node's document lists the Data
Storm ROM content-id: one title, that `node_id` on `content_sources`,
no path, no second row. A different hash or system does not link and
does not add a source. `node_id` other than `target_id` drops the
document, including its `content_ids`. Two nodes listing one id produce
one title and two source ids. A host with no #298 id contributes no
`node_id`. Address spoof: the node is enrolled at origin A, and an
advertisement claims that node id at origin B. The content read sends
no `Authorization` to B, so there is no credential to relay. A document
from B does not add a source. The owner-facing sentence is `node moved;
re-pair or confirm the new address`. After the owner confirms B by a
config edit or re-pair, the recorded origin is B and the same document
can add the source. Hostname rebinding: enrollment resolved the
configured name to the owner's IP and recorded that IP. A later lookup
returns the attacker's address. The content read sends no
`Authorization` to that address. An IP-literal origin has no lookup
to move. These provenance tests do not ship before #396.
Ensure and placement defaults stay off.

#### 3. Content-id lock

**Proposal.** Lock the per-slot form in
[`mesh-phase2.md`](mesh-phase2.md) as the content id. Canonical text is
`sha256:` plus 64 lowercase hex digits. The digest is SHA-256 of that
slot's own bytes. Primary media uses the digest #361 already links on:
the catalog content SHA-256 for a raw ROM, or the core-media `MediaID`
(`ROMLink.SourceSHA256`) for a package row. It is not
`ProgrammedSHA256`, not a package-archive hash, and not
`expansion.Asset.ID`. BIOS and expansion slots use the same text form
(stored firmware digest, `CartSHA256` of the slot bytes). Package / ABI
identity stays a package id plus ABI id and major, and it is not a
content-id. Title id stays the catalog game id and must not parse as a
content-id.

Normalization is refusal on the wire parser. Producers emit the
canonical text. `meshcontent.ParseContentID` does not trim, does not
downcase, and does not add a missing prefix. It rejects uppercase hex,
an uppercase algorithm name, a bare 64-hex digest, a second colon, the
wrong length, `sha1:` or `blake3:`, a filesystem path, and one hash of
the whole launch. A stored slot digest is kept only when `FromSHA256`
builds an id and `ParseContentID` accepts that id's `String()`. A
canonical `sha256:` value already in the stored field is not coerced.
A digest `FromSHA256` rejects, including uppercase hex, a filesystem
path, and that prefixed wire text, skips the title with `<slot>
digest is not a stored sha256`. A built id that `ParseContentID`
rejects skips the title with `<slot> content id is malformed`. The
title is not dropped without a reason.

`FromSHA256` is not that parser. It accepts a bare 64-lowercase-hex
digest the host already stores and returns a `ContentID` whose
`String()` is `sha256:` plus that digest
(`internal/meshcontent/content.go`). It still rejects uppercase hex,
the wrong length, a prefixed string, and a filesystem path. It does
not trim and it does not downcase.

A later algorithm is a new name before the colon, accepted only when
`internal/meshcontent` is changed to name it. Existing `sha256:` ids
stay valid. The new name does not reinterpret a sha256 digest.
Versioning is that algorithm name. A mesh minor may carry the new name
as an optional field. An unknown name still fails closed
(`ErrAlgorithm`). This proposal does not add a second algorithm.

The content-id lock was approved in #394 on 2026-10-02. Canonical text
is `sha256:` plus 64 lowercase hex. `ParseContentID` does not normalize.

**Alternatives.** Coercing uppercase or a bare digest inside
`ParseContentID` would let two spellings alias one wire id.
`FromSHA256` already builds the prefixed id from a stored digest; that
is the producer path, not a second wire spelling. A mesh-protocol minor as the algorithm
version would tie slot identity to session negotiation. One hash of the
whole launch is the model this draft already refuses. This proposal
keeps the strawman text and rejects every other form.

**Out of scope.** Rewriting stored catalog digests, hashing files again
inside the projection, and adding blake3, sha512, or any other name.

**Acceptance tests, phase 2.** `ParseContentID` accepts `sha256:` plus
the Data Storm ROM digest and rejects the forms listed above, including
the bare digest. `FromSHA256` accepts that bare 64-lowercase-hex digest
and `String()` is the prefixed id. `FromSHA256` rejects uppercase hex,
the wrong length, a `sha256:` prefix, and a path. The title-link key
is that primary-media id, and a `ProgrammedSHA256` of different bytes
does not link. A title id does not parse as a content-id. An unknown
algorithm fails closed. No new algorithm name is accepted in this phase.

#### 4. Remote `native_emu` eligibility and identity

**Proposal.** A remote runner advertises capability only on DNS-SD:
`node_id`, `mesh=1.0`, and execute kind `native_emu`. It advertises
`display_sink` or `input_source` only when it actually presents or
accepts local input. A headless runner advertises execute only. DNS-SD
carries no core pin, title, path, or token. The runner advertises
`mesh` only after it implements this contract.

The eligibility facts are the #360 `software_backends` array:
`execution`, `emulator`, `core_id`, `core_version`, `core_sha256`,
`system`, `available`. That array is authenticated provenance only
after the endpoint, the credential, and the runner's `node_id` are
bound as this decision requires. Until that binding exists, the shell
drops the body and every remote `native_emu` candidate stays
unavailable. Current `host_only` remains a host-local execution label,
not a remote advertisement.

**Runner endpoint.** The shell reads `software_backends` with
`GET /api/v1/health` on the runner's paired launcher listener
(`hostapi.NewLauncherHandler`, the `fogcast-api` launcher listener).
The loopback host API (`-listen`, default `127.0.0.1:8787`) serves the
same handler with no bearer and is not a remote read. The kit agent
port does not serve this array. `[[targets]].agent` is the host-to-kit
bearer for `/v1/mesh/...` and `/v1/kit/...`. It is the wrong credential
and the wrong port for this health read.

**Credential.** The credential is one launcher pairing bearer
(`LauncherPairing` in `internal/hostapi/launcher.go`): one token bound
to one `target_id`. Issuance is the pairing model that exists today
([`mesh-vnext.md`](mesh-vnext.md) §2.1 and §2.4b,
`scripts/prepare_launcher.py`). The runner, as the listener, stores it
in its private `launcher-host.json` (`listen`, `token`, `target_id`),
the file `fogcast-api -launcher-config` loads. The shell, as the
client, stores the matching private `launcher.json` (`api`, `token`,
`target_id`). `api` is the enrolled origin for that bearer.
`target_id` in both files is the runner's node id.
`prepare_launcher.py` mints the bearer when the pair is absent and
keeps it distinct from the agent token, and `fogcast-api` refuses to
start when a launcher bearer equals a configured `[[targets]]` agent
token. Today that script writes the selected kit's `target_id`.
Writing a runner id is prerequisite 3 below. This proposal does not
change the script. The bearer is not a DNS-SD field.
Discovery of an unknown node does not enroll it. The v-next PAKE flow
stays unimplemented and is not this issuance. There is no Host/Kit
enum.

The shell sends `Authorization: Bearer` and `X-FogCast-Target-ID` set
to the node id stored with that bearer. The runner admits the read only
when the bearer matches one pairing (`matchLauncherToken`) and the
header equals that pairing's `target_id`. A wrong bearer is 401 and the
body is dropped. Presenting a `[[targets]]` agent token as this bearer
is that same 401. A bearer that matches a pairing for a different
`target_id` than the header is 403 and the body is dropped.

**Identity binding.** The shell keeps `software_backends` only when the
health body echoes `node_id` and that echo equals the node id stored
with the bearer. A missing `node_id`, or any other echo, drops the
body. A dropped body is not provenance. The node stays unavailable
with `node identity or address is ambiguous`.

Today's `healthResult` (`internal/hostapi/server.go`) has `host`,
`mesh`, `target`, and `software_backends`. It has no `node_id`. On a
paired read the handler may copy `X-FogCast-Target-ID` into
`target.connection.target_id`. That value is the caller's header.
`software_backends` is the serving process's own cores. A matching
bearer shows the caller knows a pairing secret. It does not show which
installation produced the array.

The id stored with the bearer, sent in the header, and echoed in the
body is #298 item 2: a random installation id from the CSPRNG,
owner-only, stable across restart, not a hostname or a MAC. This
proposal does not mint that id and does not add the echo. A kit keeps
the `target_id` it already has and does not wait on this list. Kit
`fpga_native` eligibility is unchanged.

**Prerequisites.** Remote `native_emu` stays unavailable until #298
and every item below is done. This proposal does not implement them.

1. **Host installation id (#298).** The runner has the #298 item 2 id,
   the pairing stores it as `target_id`, and health echoes it. Until
   that id exists there is nothing to bind, and no remote
   `software_backends` is provenance. Acceptance: a missing `node_id`,
   or an echo other than the id stored with the bearer, drops the body
   with `node identity or address is ambiguous`. With no #298 id the
   node stays unavailable even when the pin would match.
2. **Runner admission.** `kitTarget` admits the launcher header only
   when it is an enabled `[[targets]]` row on the serving process. A
   runner's installation id is not such a row. The prerequisite is a
   runner enrollment record, or an equivalent admission rule, that
   admits that id on the launcher health read. This proposal does not
   add the record and does not retarget `kitTarget`. Acceptance: with
   the record, the runner id is admitted; a missing or foreign record
   is 403 and the body is dropped. An enabled kit `[[targets]]` row
   still admits that kit.
3. **Provisioning.** `prepare_launcher.py` writes the selected kit's
   `target_id` only. The prerequisite is provisioning that writes the
   runner's node id into the runner's `launcher-host.json` and the
   shell's `launcher.json` (`api`, `token`, `target_id`). This
   proposal does not change the script. Acceptance: a runner
   enrollment produces that pair for the runner id. Preparing a
   selected kit still writes the kit id and does not invent a runner id.
4. **Authenticated health, end to end.** With items 1–3 done, the
   shell reads `GET /api/v1/health` at the enrolled `api` origin with
   the launcher bearer and `X-FogCast-Target-ID` equal to the stored
   node id. The runner returns 200, the body echoes that node id, and
   `software_backends` is kept as provenance. Acceptance is that read.
   Until items 1–3 are done, the same fixture stays unavailable and
   the array is not provenance.

**Enrolled origin only.** Any bearer, including the launcher bearer
and the kit agent bearer, goes only to the origin recorded at pairing
or in configuration. For this runner read that origin is
`launcher.json`'s `api`. For a kit it is the address recorded on the
configured target. A discovered address, or a changed address for the
same node id, never receives credentials automatically and gets no
`Authorization` header. The shell does not adopt it. The node stays
unavailable with `node identity or address is ambiguous`, and the
owner-facing sentence is `node moved; re-pair or confirm the new
address`. That sentence is not a new library `reason` string.
Confirmation is an explicit config edit or a re-pairing step by the
owner, which replaces the recorded origin. After that confirmation,
reads send the bearer to the new recorded origin. There is no
challenge. A credential that is never sent cannot be relayed.

A configured hostname is not a fresh lookup on each request.
`scripts/prepare_launcher.py` (`validate_address`) accepts a hostname
and writes an `http://` origin. Today that write does not record a
resolved IP. At enrollment or configuration the implementation resolves
the hostname once and records that IP beside the hostname. Bearer
requests connect only to the recorded IP and send the configured `Host`
header. If the hostname later resolves to a different address, that is
the same `node moved; re-pair or confirm the new address` case: no
`Authorization` header until the owner confirms, which records the new
IP. An IP-literal origin is unchanged. It is the connect address, and
it is not re-resolved. When a lookup returns several addresses, the
shell still connects only to the recorded IP.

This defends against discovery spoofing and against DNS or mDNS
spoofing that points a configured hostname at an attacker after
enrollment. An on-path LAN attacker who ARP-spoofs the recorded IP,
while the bearer is plain HTTP, is out of scope until the pinned-TLS
option.

Origin authentication by TLS, with a key pinned at pairing, is a
possible later way to accept a new address without that manual step,
and the way an on-path attacker would be in scope. It is tracked
separately and is out of scope.

`placementReadAddress` (`fogcast/mesh_place_wire.go`) still prefers a
unique discovered origin, and `readPlacementNodeFacts` still sends the
kit agent bearer there. That leak is
[#396](https://github.com/DeanoC/fes/issues/396), which applies this
same enrolled-origin-only rule on the kit path. #379 does not change
`placementReadAddress`. The runner read above must not copy that
preference. Decision 2's provenance must not ship before #396.

A remote node is eligible for an option only when provenance was
accepted and every line below holds. Pin, system, `available`, and
mesh checks run only after the body is kept. Until the prerequisite
list is done they are not reached, so a pin that would match still
leaves the node unavailable.

- The read is the enrolled pairing above: bearer, header, echoed
  `node_id`, and the origin recorded at pairing or in configuration.
  For a hostname, the connection is the IP recorded then, with the
  configured `Host` header. A discovered address, a changed address, or
  a hostname that now resolves elsewhere is not that origin until the
  owner confirms it. DNS-SD `node_id` equals that enrolled id. Two
  claims of one node id in a single browse stay ambiguous.
- Mesh major matches. `discovery.MeshMajorCompatible` on the DNS-SD
  mesh string, and health `mesh.major` of 1. A newer minor is
  compatible. Unknown optional minor fields are ignored. An omitted
  mesh and a different major both fail closed with `mesh protocol major
  mismatch`, which the #361 switch already checks before the emulator
  reason.
- One `software_backends` element has `execution` `native_emu`,
  `system` equal to the title system, `available` true, a non-empty
  `core_id`, and `core_sha256` equal to the single `sha256` on this
  shell's `[host_emulator] cores` entry for that system
  (`fogcast/config.go`). That loader rejects a duplicate platform and
  stores one `sha256` on the entry. Comparison is exact match to that
  one lowercase hex value. There is no second pin. The M1 sms pin
  recorded for Genesis Plus GX is
  `051cb96ad3d1a98809c103b3836e2830e269de43f9f1943d482749873082bc49`.
  `core_version` is the configured label (Genesis Plus GX reports
  `" c2838c7d"`, including the leading space) and is not a gate.
  `emulator` is reported on that health element (`retroarch` today) and
  is not a second gate. An empty local pin does not authorize a remote digest. The
  shell's own health array does not make a remote node eligible. The
  facts have to come from that runner's health.

Linux and Mac runners whose `core_sha256` values differ are not both
eligible under this contract. The runner whose digest equals that one
configured pin matches. The other keeps `remote emulator system and
version unverified`. A separate remote pin list, so more than one
digest could match, is an open follow-up. This proposal does not add
it. No issue is filed for it.

Any failed pin, system, or `available` check keeps the node reason
`remote emulator system and version unverified`. The option reason
stays `no compatible executor in inventory` when no node on that option
is eligible, and `no advertised executor in inventory` when no
`native_emu` node is in inventory. A wrong bearer, a missing or forged
`node_id`, a spoofed address, or a failed health read fails closed as
`node identity or address is ambiguous`. Every remote candidate stays
unavailable until all of #298, runner admission, and runner
provisioning are done, and until the mesh major and the pin match hold. Phase 2 must
not mark a remote node available on a weaker check.

The host-local emulator option stays the #361 rule: the catalog source
is available and its root is online. The remote pin does not apply to
it. Local launch continues to enforce the pin inside the RetroArch
adapter.

**Alternatives.** Calling `GET /api/v1/health` with the `[[targets]]`
agent token does not match the listener that serves
`software_backends`. Treating `target.connection.target_id` as the
runner id would trust a header the caller sent. Sending any bearer to
a DNS-SD address would give a forged advertisement the credential. A
nonce challenge answered with the pairing key can be relayed to the
enrolled origin, so this proposal does not use one. Resolving a
configured hostname again on every request would follow a spoofed DNS
or mDNS answer and send the bearer there. Implementing
#298, the runner admission record, runner provisioning, or PAKE inside
#379 would be a second enrollment design. Matching `core_version`
would treat a display label as identity. An allow-list of several
`sha256` values for one system is not this contract: `fogcast/config.go`
rejects a duplicate platform and stores one `sha256` on the entry. A
separate remote pin list is an open follow-up, and no issue is filed
for it. Trusting a runner against an empty local pin would accept any
observed `.so`. Putting
`software_backends` on DNS-SD would publish pins on an unauthenticated
advertisement.

**Out of scope.** Remote launch, picture, pads, a second lease, busy
and Stop races (#363), PAKE, minting the #298 id, adding the health
`node_id` echo, the runner admission record, changing
`prepare_launcher.py`, TLS origin authentication with a key pinned at
pairing (tracked separately), ARP spoofing of the recorded IP on
plain HTTP until that option, changing `placementReadAddress` (#396),
new reason strings, a remote pin list of more than one core digest
per system (open follow-up), and any change to host-local
availability. Ensure and placement stay off.

**Acceptance tests, phase 2.** A wrong bearer on the runner listener
returns 401. The shell ignores `software_backends` and the node reason
is `node identity or address is ambiguous`. Presenting the agent token
as that bearer is the same failure. An authenticated body whose
`node_id` is missing, or differs from the id stored with the bearer,
is dropped with that same reason. A spoofed advertisement names the
enrolled node id at a new origin. That request carries no
`Authorization` header. No credential is sent, so there is nothing to
relay. Health taken from that origin does not make the node eligible.
The owner-facing sentence is `node moved; re-pair or confirm the new
address`. Hostname rebinding: enrollment resolved the configured name
to the owner's IP and recorded that IP. A later lookup returns the
attacker's address. No bearer request is connected there, and no
`Authorization` header is sent. An IP-literal origin has no lookup to
move. After the owner confirms that origin by a config edit or
re-pair, a health read to the new recorded origin sends the bearer
and, once the prerequisite list is done, can be provenance. A
duplicate node id in one browse stays ambiguous. Until all of #298,
runner admission, and runner provisioning are done, the remote node stays
unavailable even when the pin, the system, `available`, and mesh major
1 would all match. The end-to-end health read in prerequisite 4 is the
test that a kept `software_backends` array requires those three. After
provenance is accepted, `core_sha256` must equal the single configured
`sha256` for that system. A different digest, including a Linux build
and a Mac build of the same core, keeps `remote emulator system and
version unverified`, as does a system or `available` failure. Mesh
`2.0` or an omitted mesh keeps `mesh protocol major mismatch`. A
different `core_version` with that same one pin stays eligible once
provenance is accepted. The host-local option's availability is
unchanged. Ensure and placement defaults stay off.

### Named legal two-node test titles and evidence

Only use titles whose distributable bytes are in this repository or whose
license is documented. Never use copyrighted commercial ROMs in acceptance.
Standalone FES Pong is in-repository homebrew source with
`GPL-2.0-or-later` SPDX headers in `sources/misteross/cores/fes-pong/`; its
package/library path is documented in
[`sources/FogCast/docs/core-package-library.md`](../sources/FogCast/docs/core-package-library.md).
It is a legal kit-backend candidate, but it has no software backend.

**M1 dual-backend title (decided 2026-10-01, #360/#364):** *Data Storm* 1.00
by Haroldo de Oliveira Pinheiro (haroldo-ok), a Master System homebrew entry in the SMS Power 2016
coding competition. The game is Apache-2.0
([haroldo-ok/datastorm](https://github.com/haroldo-ok/datastorm), tag `v1.00`
at `ce420df088840ba7e3a8e2960a18dd936f2da053`). Its bundled SMSlib is
public-domain (Unlicense) and PSGlib is BSD-3-Clause. The ROM comes from release
asset `DataStorm-SMS-1.00.zip`
(sha256 `d9161932007b397147a2f4bbd3331c28fcd4ba6bbff4e357eb66feed5b8af518`)
as `datastorm.sms`: 32768 bytes, sha256
`4b0fc42c8ab3d6d073dbc0f902b0fe35709e804613740ab52cb122bdb5082d4f`,
sha1 `73d6f05c6603723ff49ff4925be405ab417c32cb`, crc32 `37b775d6`. It needs no
BIOS and no mapper, and it is an exact 32 KiB fixed-map image, so it fits the
`fes.sms` slice (`sources/misteross/cores/fes-sms/README.md`) without padding.
- **Kit backend:** `fpga_native` on `fes.sms`. The core source is at
  `e88c0426b40a81f090811dfe4e0d9fdcde96f13c`. The package digest is recorded
  with the #364 evidence. The kit Mode 4 raster runs slower than 60 Hz, so
  game speed can differ from the software backend. That difference is known and
  is not an acceptance failure.
- **Software backend:** `native_emu` using the Genesis Plus GX libretro core on
  Powerboat (Debian 13 x86_64, headless), using RetroArch 1.20.0 from the
  Debian package and the upstream
  [libretro/Genesis-Plus-GX](https://github.com/libretro/Genesis-Plus-GX)
  build at `c2838c7dc4236fc2fe94e5dbd08b41486067918e`. The core reports
  `library_version` `" c2838c7d"`; the built `.so` sha256 is
  `051cb96ad3d1a98809c103b3836e2830e269de43f9f1943d482749873082bc49`.
  The core uses its own non-commercial license and is not redistributed here.
- **Fallback, if Data Storm fails on either backend:** *2048* for SMS by
  grz0zrg (BSD-3-Clause,
  [grz0zrg/2048-SMS](https://github.com/grz0zrg/2048-SMS), also 32 KiB).

The ROM bytes are fetched from the upstream release and verified by hash. They
are not committed here. Do not assume FES Pong has a
RetroArch core. The repo also contains a MIT-licensed Coleco controls
diagnostic (`sources/misteross/cores/fes-coleco/README.md` and
`diagnostic/LICENSE`), but it is not evidenced as a RetroArch software title.

| Acceptance claim | Current evidence | M1 standing |
| --- | --- | --- |
| Kit can execute FES Pong; target/runtime launch and Stop work | Existing target/runtime tests and dated Pong HIL records; `docs/mesh-vnext.md` §5 HIL2 is two-kit placement diagnostic, picture-only, not designated-kit acceptance. | Implemented for the kit path; exact M1 artifact acceptance pending #364. |
| Software runner can execute a named legal title | RetroArch host execution adapter/tests (`sources/FogCast/internal/hostexec/retroarch.go`, `_test.go`) establish a local host-only path, not a remote mesh runner. | Title and backend chosen (Data Storm 1.00 on Genesis Plus GX, above). Runner implementation is #360; exact-artifact acceptance is #364. |
| Same title on both backends (dual-backend) | Data Storm 1.00 is chosen for `fes.sms` and Genesis Plus GX; neither run is recorded yet. | GAP #361/#364; do not claim dual-backend coverage until both runs are recorded. |
| Two nodes browse and show truthful availability | `GET /api/v1/mesh/nodes` and placement tests cover advertised kits; HIL2/HIL3 records are diagnostic as scoped in `docs/mesh-vnext.md` §5. | Kit inventory evidence exists; coherent combined library/UI remains #361/#362. |
| Independent play, scoped input/Stop, busy/version mismatch, reconnect | Existing kit lease and launcher ownership tests cover individual rules; no exact two-node software-runner acceptance record. | GAP #363, then exact-artifact HIL #364. |

Open implementation dependencies: #359 local kit host/shell and measured
limits; #360 Linux/Mac software runner and backend negotiation; #361 shared
library/backend projection; #362 room availability and return behavior;
#363 independent sessions and failure handling; #364 exact-artifact
acceptance. #288 (per-kit concurrent launcher sessions/Stop) and #289
(launcher health/cache still describe the selected target) remain separate
launcher gaps where M1 requires them; #287 already supplies per-kit launcher
credentials and ownership checks. #358 freezes no wire schema: if a shared
wire definition becomes necessary, coordinate it with mister-packages rather
than duplicating it here.

## Decisions

Recommended defaults, written 2026-09-23. Items marked Caster or Foggy
replace the earlier strawman on that point. Deano has not signed each
bullet; Decision 7 is signed (2026-09-27). Implementation work should
treat the paragraph in this file as
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
one of those objects. The content-id text, `sha256:` plus 64 lowercase
hex, was approved in #394 on 2026-10-02. See
[Combined library contract (#379)](#combined-library-contract-379).
Shape: [`mesh-node-protocol.md`](mesh-node-protocol.md).

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

**Locked by Deano, 2026-09-27**, as reconciled from Foggy's product
review and Caster's review, 2026-09-23. Phase 3. The general rule is
not "a display near the shell."

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
6. For now, when the rules above still leave several kits that can run
   the title, or several `native_emu` nodes, the first in the host's
   node inventory wins. That inventory is in node-id order. A defined
   order or a selection replaces this later.

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
| **3 — placement** | Automatic placement, plus an optional advanced override. Policy is Decision 7. Execution: [`mesh-phase3.md`](mesh-phase3.md). | Routable I/O as the normal path; a capture preview counted as DisplaySink |
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

**Placement order.** Locked by Deano, 2026-09-27: Decision 7 as
written. Household display preference or last play sink first
(living-room kit HDMI over a Mac shell). FPGA execute uses that kit's
DisplaySink. Near the shell only for the same seat or a Phase 4
captured remote sink. A V4L2 or ShadowCast-class preview is not a
DisplaySink. When several kits, or several `native_emu` nodes, remain,
the first candidate wins for now; a defined order or a selection comes
later.

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
the catalog id. One hash of the whole launch is not the model. The
content-id text, `sha256:` plus 64 lowercase hex, was approved in #394
on 2026-10-02. See
[Combined library contract (#379)](#combined-library-contract-379).

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
numbers, the later kit and native-executor order, and host node-id minting are
follow-ups inside the protocol doc once Deano locks the paragraphs
above. Advertisement TTL never frees a play lease. Only lease expiry
does.

## Pointers

| Doc | Why |
| --- | --- |
| [`docs/mesh-node-protocol.md`](mesh-node-protocol.md) | Capability, lease, content-id, session, failure classes |
| [`docs/mesh-phase1.md`](mesh-phase1.md) | Phase 1 execution, closed on `3d34b6e0` |
| [`docs/mesh-phase2.md`](mesh-phase2.md) | Phase 2 execution, content plane |
| [`docs/mesh-phase3.md`](mesh-phase3.md) | Phase 3 execution, placement (Decision 7 locked) |
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
