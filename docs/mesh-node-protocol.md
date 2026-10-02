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

Present tense, verified against the FES source at this worktree's tip.

Control today is the FogCast host session API to a configured target
agent, then `/run/mister-runtime.sock` protocol 2. The agent is the kit
lease authority. A lease lasts 90 seconds; clients renew every 20
seconds; expiry and explicit takeover are in
[`docs/kit-sharing.md`](kit-sharing.md). The runtime decides physical
transitions, including idle recovery and `reboot_required`.

Identity today is a persistent `target_id` per configured kit. The current
kit DNS-SD TXT carries `protocol=1`, repeats that ID as `node_id`, and
advertises `mesh=1.0` plus its capability bag. The TXT encoder emits no
`ttl` field; the parser tolerates an optional `ttl`, while DNS-SD browse
expiry removes silent nodes from future inventory. Advertisement expiry
does not release a lease. Advertisements carry no credentials, title list,
or lease secrets. Health must confirm
the ID and target API version before the host adopts a new address.
Phase 0 peers may omit mesh fields and remain directly bindable.

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

Host `[mesh] ensure` defaults off. With the key unset or false,
`fogcast-api` and the `fogcast` CLI still call `EnableMeshContent`,
and that call leaves the ensure seam off. A package-backed FPGA
launch then stays on the Phase 0 and Phase 1 path, including when the
kit's content source does not advertise the title. `ensure = true`
turns the seam on. The default stays off until #177 (legacy fallback
when the source does not advertise) and #172 (kit home host) land.
The agent `mesh_content` switch stays default on. With the host seam
off, that kit store is not consulted before launch.

Host `[mesh] placement` also defaults off. With it on, the host builds
Place candidates from its node inventory and reads `abis` from the
node document of each configured kit it may place on. A launch that
names `target` does not ask for placement. Placement does not need
the ensure seam: with the seam off, a rebind onto another kit streams
the launch on the Phase 0 path. See [`mesh-phase3.md`](mesh-phase3.md)
Slice 8.

## M1 interoperability matrix

This matrix separates the existing kit protocol from the proposed remote
software-runner contract. A capability is an execution fact, never a
permanent Host/Kit identity. Current DNS-SD fields are implemented; runner
advertisement and pairing are not.

| Field | Kit today (implemented) | Linux/Mac software runner (proposed; gap #360) |
| --- | --- | --- |
| Identity | `node_id` and `target_id` are the same persistent kit ID. Configured-target authentication and per-kit launcher credentials bind it; user pairing is not implemented, and discovery alone does not enroll. | The #379 proposal reads a runner's `software_backends` on that runner's launcher listener with a launcher pairing bearer, and only at the origin recorded at pairing. The bearer is bound to the runner's node id (#298 item 2), which is not minted here. Runner admission and provisioning are further prerequisites in decision 4. Until #298 and those prerequisites, remote `native_emu` stays unavailable. Do not add a Host/Kit enum. PAKE is not this enrollment. |
| Protocol | `mesh=1.0`; Phase 0 peers may omit it and remain directly bindable. A needed mesh major mismatch fails closed. | Negotiate the same mesh major.minor; advertise only after support is implemented. |
| Execute | `fpga_native`; TXT has no ABI families. Placement separately reads authenticated node content/ABI data. | DNS-SD carries execute kind `native_emu` only (with `node_id` and `mesh`). Systems, emulator name, core id, core version, and core pin come from authenticated `software_backends` on `GET /api/v1/health`, read at the enrolled origin. They are not DNS-SD fields. Current `host_only` is a host-local execution label, not a remote advertisement. Eligibility is **PROPOSED (needs Deano/Bob sign-off)** below. Until that sign-off, #298, and the decision 4 enrollment prerequisites, every remote candidate stays unavailable. |
| Display/input | `display_sink=true`, `input_source=true` mean the kit can present and supply local input; they do not attest live picture or multiple players. | Advertise only functions the runner owns. Remote video/input routing is not part of M1 evidence. |
| Availability | DNS-SD TTL controls inventory presence only. Kit lease status and composition determine admission/readiness separately. | Heartbeat/health, backend readiness, and single-session busy state need an implementation and honest UI. |
| Ownership | Existing target-agent kit lease is the FPGA admission authority (90-second grant, 20-second renewal); one owner, explicit release/expiry cleanup. | Runner serializes its own session lifecycle and rejects busy/incompatible requests. No second FPGA lease. |
| Catalog/content | No title list or credentials in DNS-SD. Host library and per-kit mesh content API are separate. | Reuse title IDs and backend options through host library surfaces; do not add titles, paths, or secrets to advertisements. Content transport is outside this minimum M1 contract. |

**Title/backend rule (proposed):** retain catalog `game_id` as title
identity and represent executable options separately. A session chooses one
backend and executor; backend compatibility/version, required package or
media composition, availability, and ownership must all admit before launch.
Current kit and host-local paths are separate implementations; federated
runner negotiation and dual-backend evidence are GAP #360/#361. The evidence
matrix and legal test-title limits are in [`mesh-lan.md`](mesh-lan.md).
The contract that closes those gaps is proposed in the next section and
is not implemented.

### Combined library contract (#379)

**PROPOSED (needs Deano/Bob sign-off).** Phase 1 of #379 records these
four decisions. Phase 2, after sign-off, extends
`ProjectMeshBackendLibrary` and adds the tests named here. There is no
second inventory and no second discovery path. `[mesh] ensure` and
`[mesh] placement` stay default off. The current Data Storm projection and the phase-2 remote-option
fragment are in [`mesh-lan.md`](mesh-lan.md#combined-library-contract-379).
This page is the rule those examples follow. The two copies of the
field list are the same object.

#### 1. Combined library wire shape

**Proposal.** One new host read, `GET /api/v1/library/titles`, returns
the grouped view `Service.MeshBackendLibrary` already computes. No query
and no body. The kit launcher consumes that route as a paired library
read (bearer and `X-FogCast-Target-ID`, the same admission as
`GET /api/v1/games`). It is not added to the mesh content reads. The
route does not launch and does not report Ready.

| Field | Rule |
| --- | --- |
| `titles[].title_id` | Canonical catalog game id. #361 link: same primary-media content-id and system; package row wins, else lowest game id. |
| `titles[].system` | Browse system. For `fes.sms`, `sms`. |
| `titles[].content_ids` | Slot content-ids in projection order. Package / ABI is omitted. Text form is decision 3. |
| `titles[].content_sources` | One entry per content-id. `node_ids` is decision 2. Empty means no remote supplier. |
| `options[].source_game_id` | `Entry.TitleID` of that option. Distinct from `title_id` when rows were linked. |
| `options[].execution` | `fpga_native` or `native_emu`. `host_only` is not on this object. |
| `options[].host_local` | True only for this host's emulator option. |
| `options[].available` | Host-local: true when the option reason is empty. Otherwise true when one node is available. Not session Ready. |
| `options[].reason` | Omitted when empty. Otherwise an option reason from the table below. |
| `options[].core_id` | Catalog core id (`fes.sms`) on a package-backed option only. |
| `options[].package` | `package_id` (64 lowercase hex), `abi`, `major`. Package-backed options only. |
| `options[].nodes[]` | `node_id`, `available`, and `reason` when the node is unavailable. Always present, possibly empty. |

#361 reason strings, unchanged:

| Where | Strings |
| --- | --- |
| Option | `local source unavailable`, `no advertised executor in inventory`, `no compatible executor in inventory` |
| Node | `inventory retained after browse error`, `node identity or address is ambiguous`, `mesh protocol major mismatch`, `remote emulator system and version unverified`, `package unavailable on node`, `package ABI incompatible on node`, `executor unsupported` |

A host-local `native_emu` option lists remote `native_emu` nodes. Those
nodes stay unavailable and do not clear the option.
`Service.MeshBackendLibrary` does not emit a further option:
`resolveExecution` returns only `fpga_native` or `host_only`, and
`host_local` is true only for `host_only`. The current Data Storm
projection in `mesh-lan.md` is the kit option plus that host-local
option. Phase 2 adds the remote option inside
`ProjectMeshBackendLibrary`, after the local titles are linked, as one
synthetic `MeshTitle` with the same game, system, and primary-media
digest and with `Execute` set to `native_emu`. The existing loop keeps
it because `host_local` differs. `source_game_id` stays the raw ROM
catalog id. The option's nodes are the inventory nodes that advertise
`native_emu`. Until decision 4 accepts a node's provenance, the node
reason stays `remote emulator system and version unverified` and the
option reason stays `no compatible executor in inventory`. The
host-local option stays as #361 built it, nested remote nodes included.
That extra option is the phase-2 fragment in `mesh-lan.md`. Paths,
bytes, tokens, and `ready_here` are not fields. Skipped titles are not
elements of `titles`.

**Alternatives.** Extending `GET /api/v1/games` would change the row
rooms already bind, including `ready_here`. Extending
`GET /api/v1/mesh/nodes` would put titles on the inventory. A
mister-packages copy is not required for a host route the launcher
reads; #358 freezes no shared wire. Coordinate with mister-packages
only if a kit parses this object without the host.

**Out of scope.** Session selection, Ready, filters, a skipped-title
array, and any new reason string. Ensure and placement stay off.

**Acceptance tests, phase 2.** The current Data Storm document in
`mesh-lan.md` round-trips: one title, the `fes.sms` option, the
host-local emulator option, the real ROM content-id, and no path. That
body has no `host_local: false` emulator option. A second test runs
the synthetic `Execute: native_emu` title through
`ProjectMeshBackendLibrary` and expects the phase-2 option in
`mesh-lan.md`, with those reason strings, still unavailable. Launcher
admission is the paired-read rule, and a missing bearer is 401.
`GET /api/v1/games` is unchanged. Ensure and placement defaults stay
off.

#### 2. Remote source-provenance contract

**Proposal.** Supply is an optional `content_ids` array on the
authenticated node document, `GET /v1/mesh/content/node`. Each element
is a canonical content-id string from decision 3. The host accepts the
array only when the document's `node_id` equals the configured
`target_id`, the check `readPlacementNodeFacts` already applies to
`abis` and `packages`. The array rides that same read and its cache.
An omitted or empty array supplies no content-ids. An element that
does not parse is dropped. A dropped element does not fail `node_id`,
`abis`, or `packages`.

The read is `readPlacementNodeFacts`. Today `placementReadAddress`
prefers a unique DNS-SD origin and sends the kit agent bearer there
before `node_id` is checked, so this extension inherits that leak.
[#396](https://github.com/DeanoC/fes/issues/396) is the kit-path fix.
It applies decision 4's enrolled-origin-only rule: any bearer goes
only to the origin recorded at pairing or in configuration. A
discovered or changed address never receives credentials
automatically. The owner-facing sentence is `node moved; re-pair or
confirm the new address`. That sentence is not a new library `reason`
string. Confirmation is an explicit config edit or re-pairing
step by the owner. **Phase-2 provenance must not ship before #396
lands.** This proposal does not change `placementReadAddress`.

`GET /v1/mesh/content/source`, `GET /v1/mesh/content/slots`, and the
host `GET /api/v1/mesh/content/source` stay the per-id `advertises`
check Ensure uses. They are not a title inventory.

The library reconciles that array with the local catalog:

- Remote ids do not create titles. The local catalog is the title list.
- Linking stays the #361 rule (primary-media content-id and system).
- A matching slot id adds that `node_id` to `content_sources[].node_ids`,
  inventory order, deduped.
- A package id is not a content source. Packages stay the `packages`
  list on this same document.
- This host's catalog row is not given a node id. Any host node id is
  #298 item 2 (random installation id, owner-only, stable across
  restart). Until that id exists, a remote host is not a source. A kit
  uses its existing `target_id`.
- Paths stay off the document and off the library view.

**Alternatives.** Using only the per-id source probe would keep today's
Ensure check and would not let the node state its set. Listing ids in
DNS-SD would break the advertisement rule (no title list, no paths).
Creating rows for remote-only ids would be a second catalog.

**Out of scope.** Pulling bytes, enabling ensure, paging a large array,
minting the #298 id, and changing `placementReadAddress` (that change
is #396).

**Acceptance tests, phase 2.** Node document includes the Data Storm
ROM id: one library title, that `node_id` in `content_sources`, no
second row, no path. A mismatched hash or system adds neither a row
nor a source. A document whose `node_id` is not `target_id` is ignored.
Two enrolled nodes listing one id yield two `node_ids` on one slot. A
host without a #298 id adds none. Address spoof: enrolled at origin A,
advertised at origin B, the content read sends no `Authorization` to
B, so nothing can be relayed, and a document from B adds no source.
The owner-facing sentence is `node moved; re-pair or confirm the new
address`. After the owner confirms B by a config edit or re-pair,
reads use B and the document can add the source. Provenance tests do
not ship before #396. Ensure and placement defaults stay off.

#### 3. Content-id lock

**Proposal.** Lock the Phase 2 per-slot form. A content-id is the text
`sha256:` plus 64 lowercase hex digits, SHA-256 of that slot's bytes.
Primary media is the digest #361 links on: catalog content SHA-256, or
core-media `MediaID` / `ROMLink.SourceSHA256`. `ProgrammedSHA256`, a
package-archive hash, and `expansion.Asset.ID` are not this id. BIOS
and expansion slots use the same text (firmware digest, expansion
`CartSHA256`). Package / ABI stays `package_id` plus ABI id and major.

Parsers do not normalize. `ParseContentID` and `FromSHA256` reject
uppercase hex, an uppercase algorithm, a bare digest, a second colon,
the wrong length, any other algorithm name, and a filesystem path.
They do not trim and they do not downcase. One hash of the whole launch
is rejected by the catalog shape, as it is today. A title id must not
parse as a content-id.

A future algorithm is a new name before the colon, added by naming it
in `internal/meshcontent`. `sha256:` values stay valid under that name.
The algorithm name is the version. A mesh minor may add an optional
field; an unknown algorithm still fails closed (`ErrAlgorithm`). This
proposal adds no second algorithm.

The unsigned-strawman sentences in this file and in
[`mesh-phase2.md`](mesh-phase2.md) stay until Deano signs this
paragraph.

**Alternatives.** Coercion would alias two spellings of one id. Numbering
algorithms by mesh minor would make a slot id depend on session
version. Replacing the per-slot ids with one launch hash is the model
Decision 3 of `mesh-lan.md` already refuses.

**Out of scope.** Rehashing stored bytes, rewriting the catalog, and
accepting any algorithm other than `sha256`.

**Acceptance tests, phase 2.** The Data Storm ROM digest parses. Each
rejected form above fails. A different `ProgrammedSHA256` does not
link the title. A title id does not parse as a content-id. An unknown
algorithm fails closed.

#### 4. Remote `native_emu` eligibility and identity

**Proposal.** DNS-SD for a remote runner carries `node_id`, `mesh=1.0`,
and execute `native_emu`. `display_sink` and `input_source` are present
only when that runner owns them. Pins, titles, paths, and tokens stay
off the TXT record. The runner advertises `mesh` only once it
implements this contract. The same rules are in
[`mesh-lan.md`](mesh-lan.md#combined-library-contract-379).

Eligibility facts are the #360 health `software_backends` array:
`execution`, `emulator`, `core_id`, `core_version`, `core_sha256`,
`system`, `available`. The array counts as provenance only after the
endpoint, the credential, and the runner `node_id` below are bound.
Until then the shell drops the body and the node stays unavailable.

**Runner endpoint.** The shell reads that array with
`GET /api/v1/health` on the runner's paired launcher listener
(`hostapi.NewLauncherHandler`). The loopback host API (default
`127.0.0.1:8787`) has no bearer and is not a remote read. The kit
agent port does not serve the array. `[[targets]].agent` is the
host-to-kit bearer for the agent routes. It is the wrong credential
and the wrong port for this read.

**Credential.** One launcher pairing bearer (`LauncherPairing`): one
token, one `target_id`. Issued as launcher credentials are issued
today ([`mesh-vnext.md`](mesh-vnext.md) §2.1 and §2.4b,
`scripts/prepare_launcher.py`). The runner stores the listener copy in
private `launcher-host.json` (`listen`, `token`, `target_id`), the
file `fogcast-api -launcher-config` loads. The shell stores the client
copy in private `launcher.json` (`api`, `token`, `target_id`). `api`
is the enrolled origin for that bearer. `target_id` in both files
is the runner's node id. `prepare_launcher.py` mints the bearer when
the pair is absent and keeps it distinct from `[[targets]].agent`;
`fogcast-api` refuses to start when they are equal. Today that script
writes the selected kit's `target_id`. Writing a runner id is
prerequisite 3 below. This proposal does not change the script. The
bearer is not a DNS-SD field. Discovery does
not enroll. PAKE is not this issuance. There is no Host/Kit enum.

The shell sends `Authorization: Bearer` and `X-FogCast-Target-ID` set
to the node id stored with that bearer. The runner admits the read
only when `matchLauncherToken` hits that pairing and the header equals
its `target_id`. A wrong bearer, including a `[[targets]]` agent token
presented as this bearer, is 401 and the body is dropped. A bearer
paired to a different id than the header is 403 and the body is
dropped.

**Identity binding.** The shell keeps `software_backends` only when
the body echoes `node_id` and the echo equals the id stored with the
bearer. A missing echo, or any other echo, drops the body. A dropped
body is not provenance. The node reason is `node identity or address
is ambiguous`.

Today's `healthResult` has `host`, `mesh`, `target`, and
`software_backends`, and no `node_id`. A paired read may copy the
caller's `X-FogCast-Target-ID` into `target.connection.target_id`.
That header is not a node id the runner proved. `software_backends`
is the serving process's cores. The id to store, send, and echo is
#298 item 2 (random CSPRNG installation id, owner-only, stable across
restart, not a hostname or MAC). This proposal does not mint it and
does not add the echo. A kit keeps its existing `target_id` and does
not wait on the list below. Kit `fpga_native` eligibility is unchanged.

**Prerequisites.** Remote `native_emu` stays unavailable until #298
and every item below is done. This proposal does not implement them.

1. **Host installation id (#298).** The runner has the #298 item 2 id,
   the pairing stores it as `target_id`, and health echoes it. Until
   that id exists, no remote `software_backends` is provenance.
   Acceptance: a missing `node_id`, or any other echo, drops the body
   with `node identity or address is ambiguous`. With no #298 id the
   node stays unavailable even when the pin would match.
2. **Runner admission.** `kitTarget` admits the launcher header only
   when it is an enabled `[[targets]]` row on the serving process. A
   runner installation id is not such a row. The prerequisite is a
   runner enrollment record, or an equivalent admission rule, that
   admits that id on the launcher health read. This proposal does not
   add the record and does not retarget `kitTarget`. Acceptance: with
   the record, the runner id is admitted; a missing or foreign record
   is 403 and the body is dropped. An enabled kit `[[targets]]` row
   still admits that kit.
3. **Provisioning.** `prepare_launcher.py` writes the selected kit's
   `target_id` only. The prerequisite is provisioning that writes the
   runner node id into `launcher-host.json` and the shell's
   `launcher.json` (`api`, `token`, `target_id`). This proposal does
   not change the script. Acceptance: a runner enrollment produces
   that pair for the runner id. Preparing a selected kit still writes
   the kit id and does not invent a runner id.
4. **Authenticated health, end to end.** With items 1–3 done, the
   shell reads `GET /api/v1/health` at the enrolled `api` origin with
   the launcher bearer and `X-FogCast-Target-ID` equal to the stored
   node id. The response is 200, the body echoes that node id, and
   `software_backends` is kept. Acceptance is that read. Until items
   1–3 are done, the fixture stays unavailable and the array is not
   provenance.

**Enrolled origin only.** Any bearer, the launcher bearer and the kit
agent bearer, goes only to the origin recorded at pairing or in
configuration. For this read that origin is `launcher.json`'s `api`.
For a kit it is the address recorded on the configured target. A
discovered or changed address never receives credentials automatically
and gets no `Authorization` header. The node stays unavailable with
`node identity or address is ambiguous`. The owner-facing sentence is
`node moved; re-pair or confirm the new address`, and that sentence is
not a new library `reason` string. Confirmation is an explicit config
edit or a re-pairing step by the owner, which replaces the recorded
origin. After that confirmation, reads send the bearer there. There is
no challenge. A credential that is never sent cannot be relayed.

Origin authentication by TLS, with a key pinned at pairing, is a
possible later way to accept a new address without that manual step.
It is tracked separately and is out of scope.

`placementReadAddress` still prefers a discovered origin, and
`readPlacementNodeFacts` still sends the kit agent bearer there. That
leak is [#396](https://github.com/DeanoC/fes/issues/396), which applies
this same enrolled-origin-only rule. #379 does not change
`placementReadAddress`. The runner read must not copy that preference.
Decision 2 must not ship before #396.

Eligible means provenance was accepted and every line below is true.
Pin, system, `available`, and mesh checks run only on a kept body.
Until the prerequisite list is done they are not reached.

| Check | Failure reason (existing #361 string) |
| --- | --- |
| Enrolled launcher pairing: bearer and `X-FogCast-Target-ID` match, health echoes the node id stored with that bearer, DNS-SD `node_id` equals it, and the origin is the one recorded at pairing or in configuration. A discovered or changed address is not that origin until the owner confirms it. A duplicate node id in one browse fails this row. | `node identity or address is ambiguous` |
| DNS-SD mesh passes `MeshMajorCompatible` (major 1; newer minor allowed; omitted mesh fails) and health `mesh.major` is 1 | `mesh protocol major mismatch` |
| One backend has `execution` `native_emu`, `system` equal to the title, `available` true, non-empty `core_id`, and `core_sha256` equal to a non-empty `[host_emulator] cores` `sha256` for that system | `remote emulator system and version unverified` |

The pin list is the shell's configured pins. Several pins are the
allowed list; match is exact lowercase hex. The recorded Genesis Plus
GX `.so` pin for M1 sms is
`051cb96ad3d1a98809c103b3836e2830e269de43f9f1943d482749873082bc49`.
`core_version` (the label `" c2838c7d"`) is not compared. `emulator`
(`retroarch`) is not a second gate. An empty local pin authorizes no
remote digest. The shell's own `software_backends` do not qualify a
remote node; the facts come from that runner's health. A failed read
fails closed as `node identity or address is ambiguous`.

The #361 reason order stays: identity, then mesh major, then the
emulator pin. An option with nodes and none eligible keeps
`no compatible executor in inventory`. An option with no `native_emu`
node keeps `no advertised executor in inventory`. Every remote
candidate stays unavailable until all of #298, runner admission, and
runner provisioning are done, and until mesh major and the pin match
hold.
Phase 2 must not mark a remote node available on a weaker check.

Host-local availability stays the catalog source check from #361. The
remote pin does not move onto that option.

**Alternatives.** `GET /api/v1/health` with the agent token does not
match the listener that serves `software_backends`. Treating
`target.connection.target_id` as the runner id would trust the
caller's header. Sending any bearer to a DNS-SD address hands it to a
forged advertisement. A nonce challenge answered with the pairing key
can be relayed to the enrolled origin, so this proposal does not use
one. PAKE, minting #298, adding the runner admission record, or
changing `prepare_launcher.py` inside #379 would be a second
enrollment design. A `core_version` match would pin a label. An empty
local pin would accept any `.so`. DNS-SD `software_backends` would
publish pins without authentication.

**Out of scope.** Remote launch, I/O routing, a native mesh lease,
#363 races, new reason strings, host-local availability, minting the
#298 id, the health `node_id` echo, the runner admission record,
changing `prepare_launcher.py`, TLS origin authentication with a key
pinned at pairing (tracked separately), and any change to
`placementReadAddress` (#396). Ensure and placement stay off.

**Acceptance tests, phase 2.** A wrong bearer on the runner listener
is 401, `software_backends` is ignored, and the node reason is
`node identity or address is ambiguous`. Presenting the agent token as
that bearer is the same failure. A forged `node_id` in an otherwise
authenticated body, and a missing `node_id`, drop the body with that
same reason. A spoofed advertisement claims the enrolled node id at a
new origin. That request carries no `Authorization` header. No
credential is sent, so there is nothing to relay, and health from
that origin does not make the node eligible. The owner-facing sentence
is `node moved; re-pair or confirm the new address`. After the owner
confirms that origin by a config edit or re-pair, a health read to the
new recorded origin sends the bearer and, once the prerequisite list
is done, can be provenance. A duplicate id in one browse stays
ambiguous. Until all of #298, runner admission, and runner
provisioning are done, the remote node stays unavailable even when
the pin, system,
`available`, and mesh major 1 would match. Prerequisite 4 is the
end-to-end test that a kept `software_backends` array requires those
three. After provenance is accepted, pin,
system, or `available` failure keeps `remote emulator system and
version unverified`, and mesh `2.0` or an omitted mesh keeps `mesh
protocol major mismatch`. The same pin with a different `core_version`
stays eligible once provenance is accepted. The host-local option is
unchanged. Ensure and placement defaults stay off.

## Planes, and which contracts appear when

Names match [`mesh-lan.md`](mesh-lan.md). A cell says the contract is
in force for new mesh behavior. Phase 0 columns are the current
system, described so later phases have a floor.

| Phase | Control | Content | I/O |
| --- | --- | --- | --- |
| **0 — compatibility floor** | Host session API, named `target`, kit lease. Legacy Phase 0 peers may omit mesh fields; the current kit advertises mesh `1.0` and capabilities. | This host's library and that target's cache. | Kit HDMI and audio. Kit pad. Host input on the foreground session. |
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
shell keeps that id. It does not mint a second one. Legacy Phase 0 nodes
that only speak the current target API may omit mesh-protocol version and
stay directly bindable. The current kit advertises `mesh=1.0`; Phase 0
names the compatibility floor, not the current kit's advertisement.

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
secrets. That matches today's DNS-SD discipline (node id, mesh version
and capability flags; no credentials or title data).

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
  a mesh wire freeze. `next_action` is `wait`, `supply_content`,
  `fetch_here`, `wait_for_lease`, `resolve_version`, `bind_executor`,
  `browse`, or `unavailable`. Lease-free for that view is this
  session's grant and generation on the bound node, or an unleased kit
  whose client can claim. A lost grant is not free. A foreign holder
  is not Ready. A true ReadyHere result still passes catalog admission. When the session is not installed the fields
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
locked it. A lock of that form is **PROPOSED (needs Deano/Bob sign-off)**
in [Combined library contract (#379)](#combined-library-contract-379).
This draft does not freeze bytes.

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
slot, and source reads are not. Host placement reads Node for
`fpga_native` eligibility. Those `abis` come from packages installed
or staged on that kit.

| Call | Request | Response |
| --- | --- | --- |
| Node | `GET /v1/mesh/content/node` | `node_id`, `abis` (`id`, `major`), optional `packages` (described package ids, 64 lowercase hex). An empty or omitted list is not eligibility. Optional `content_ids` is **PROPOSED (needs Deano/Bob sign-off)** in [Combined library contract (#379)](#combined-library-contract-379); the current response has no such field. |
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

### Kit content source reads the host

The kit's content `Source` reads the host, which is the content node,
with the provisioned launcher credential (`Authorization: Bearer` and
`X-FogCast-Target-ID` from `launcher.json`). This is not a second pull.
`POST /v1/mesh/content/pull` still has an empty body. The kit then reads
its source. Executor method signatures are unchanged.

| Call | Request | Response |
| --- | --- | --- |
| Advertises | `GET /api/v1/mesh/content/source?id=sha256:<64 hex>` | `{"advertises": true\|false}` with status 200 |
| Object | `GET /api/v1/mesh/content/object?id=sha256:<64 hex>` | `application/octet-stream` body |

The host serves core-media whose media id is that digest, or an
expansion cart payload whose `CartSHA256` is that digest. A library
path is not the id. These two GETs are launcher operations so the kit
can use the existing launcher listener. They are not kit-lease mutations.
The host admits them for any enabled configured kit, including when that
kit is not the foreground selected target.

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
not locked it. The #379 proposal in this file asks to lock that form;
it is not signed.

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
