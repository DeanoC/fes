# Mesh v-next: onboarding/pairing, two-kit placement, hardening slices, HIL2 acceptance

**Status:** draft. Not a wire freeze, not an implementation claim, and
not a default flip for `[mesh] ensure` or `[mesh] placement`. Builds on
#131 ([mesh LAN](mesh-lan.md), [node protocol](mesh-node-protocol.md))
and [phase 3](mesh-phase3.md): Slices 1–6 (#212–#221) plus the
Slice 7 tie-breaks and Slice 8 host wiring, all behind
`[mesh] placement = false`. The Slice 6 two-kit HIL (HIL2) is open.

**Owners:** Caster owns the technical design. Foggy owns UX and copy
for setup and pairing. **UX: Foggy to fill/refine** is a placeholder,
not copy. Bob coordinates and reviews. Deano approves and merges.
Kit HIL needs Deano present. An unattended agent does not start it.
Audience: FogCast host and agent, FES image, rooms UX. See
[phase 1](mesh-phase1.md), [phase 2](mesh-phase2.md),
[kit sharing](kit-sharing.md), [bootable media](bootable-media.md),
[getting started](getting-started.md), and
`docs/superpowers/specs/2026-09-07-target-discovery-design.md`.

---

## 1. Context

Two kits are on Powerboat's LAN. The host is `192.168.10.202`, config
`~/.config/fogcast/config.toml` (owner-only). Neither mesh switch
defaults on. A second kit still needs a hand-written token.

| Kit | Address | MAC | `target_id` | Notes |
| --- | --- | --- | --- | --- |
| A, name `dev` | `192.168.10.84` | `02:46:43:00:00:84`, hand-set `ethaddr` | `73dc9f5f-1a12-4a95-a820-a9b4e600769a` | Image `fb069f29`. Capture: GENKI ShadowCast. |
| B, name `kit2` | `192.168.10.85` (reservation `mister-b`) | `02:46:43:9d:ac:d6`, CID-derived, `S15fes-ethaddr` (IMG-1, #274) | `67c5f4e2-d288-49bb-9049-39ecf39cf6f6` | Runtime `6bf46d84`. Capture: ASUS 4KPRO. |

Both advertise hostname `mister` (IMG-3). A capture preview is not a DisplaySink ([Decision 7](mesh-lan.md#7-placement-policy)).

### What changes vs #131

#131 stays the floor. This draft changes enrollment, which those pages left as "configured target".

| #131 docs / today | v-next changes |
| --- | --- |
| `target_id` identifies a provisioned target. The host mints it; `make media` copies it into `agent.toml`. | Identity is per card: random `target_id` minted on the kit when absent, CID-derived MAC (done, #274), distinct hostname (IMG-3, not done). |
| The agent token is written on the host and baked into the card. | The kit generates the token at first boot. Images ship without a token. |
| Discovery never enrolls an unknown kit. Operators hand-edit `[[targets]]`. | Discovery still does not auto-trust. Pairing enrolls. Address plus token stays the fallback. |
| Placement is Decision 7, behind `[mesh] placement`, default off. Slice 6 HIL is open. | Two-kit placement is concrete: reconciled endpoint, lease before bind, HIL2 closes Slice 6. |
| Ensure stays default off until #177. Other nits are not a phase. | Lane 1 is one small PR per issue. Defaults stay off until the §3 gates. |
| Acceptance is named in the phase briefs. | HIL2 (C0–C7) accepts placement and lease on already provisioned kits. P-pair is later. A green HIL2 does not claim pairing. |

**Decision:** do not reopen Decision 7, the single kit lease, Phase 0
as the floor, or "discovery carries no credential". Advertisement
silence is not a lease release.

**Decision:** do not flip `[mesh] ensure` or `[mesh] placement` in
this work. §3 is the gate list. A later review flips a default only
when that list is green.

---

## 2. New-kit onboarding and pairing

Kit B is the first real case. Kit A stays the already enrolled kit.

### 2.1 What is true now

The token originates on the host. There is no kit-side token and no
pairing handshake.

A `[[targets]]` row holds `target_id`, `name`, `enabled`, `address`,
and `agent`. The token is inline. `config.go` (`fileMesh`,
`fileTarget`) uses `DisallowUnknownFields`. There is no file or env
indirection. A settings patch that omits `agent` keeps the stored
token (`mergeTargetAgentsLocked`). Writes are a hand-edit,
`PATCH /api/v1/library/settings`, or the tenfoot settings UI. `GET`
redacts the token to `agent_configured`
(`internal/hostapi/library.go`).

`prepare_target` mints a `target_id` with `discovery.NewID` when that
target has none (`PatchLibrarySettings`). `make media` reads
`selected_target` (or `FES_HOST_CONFIG`) and bakes `/fogcast/agent.toml`
(`listen_address`, `token`, `target_id` when set) plus
`/fogcast/launcher.json`. The launcher token is separate
(`scripts/prepare_launcher.py`; `scripts/media.py`
`_host_provisioning`, `generate_agent_config`,
`resolve_launcher_config`). One image is one selected target. A second
kit needs another selected target, or another host file, at media time.
See [bootable media](bootable-media.md).

If `agent.toml` has no `target_id`, `mister-agent` mints one at
`/media/fat/fogcast/target-id` (`loadOrCreateTargetID`). That file is
the node id.

Discovery is mDNS `_fogcast._tcp`. TXT is an opaque `target_id` and a
protocol version, not a credential. `AdoptEndpoint`
(`fogcast/discovery.go`) may re-find a configured target after
authenticated health matches the id. It does not enroll an unknown kit.
An unconfigured node is ineligible: `placementNodeABIs` returns no
ABIs (`fogcast/mesh_place_wire.go`).

How these two were enrolled:

- Kit A: the `dev` row (token and `target_id`) lives in Powerboat's
  config, written on the host by an operator (hand-edit or settings
  PATCH; which one is not recorded). `make media` copied the token and
  id into the card's `agent.toml`; the ids match.
- Kit B: its card carries its own token (kit A's gets 401), so its
  image was provisioned from a different host target. A separate
  `kit2-image/config.toml` exists on Powerboat, which fits
  `FES_HOST_CONFIG` at media time; not verified. Deano's main config
  has a hand-added `kit2` row (id `67c5f4e2…`, file mtime 16:49 on
  2026-09-28) holding that token: authenticated GETs on `.85` return
  200. A 17:54 read-only check that reported kit B absent was wrong.
  The row's address is still `192.168.10.212` (the pre-reservation
  lease), which is exactly #259.
- Kit B's menu footer read `Offline - local library`: `launcher.json`
  points at `192.168.10.202:8789`, where no launcher listener was
  running with that launcher token. Separate from the agent token.
- So the only sanctioned path today to give a host a kit's token is:
  write the `[[targets]]` row (address, `agent`, `enabled`,
  `target_id`) on the host, then either build the card from that row
  or copy the token already on the card into that row. Both need
  someone with the token in hand; nothing transfers it.

**Decision:** cloning a provisioned card stays forbidden. A clone
copies token and `target_id`. #131 already refuses to pick between two
live nodes with one id. v-next also refuses to pair or place (§2.6).

### 2.2 Per-card identity

- **MAC.** Done (#274). `S15fes-ethaddr` writes `02:46:43:` plus three
  SHA-256 bytes of the SD CID, unless `ethaddr=` is already set. Kit B
  is derived. Kit A is hand-set. The address follows the card.
- **`target_id`.** Minted on the kit when `agent.toml` omits it.
  **Decision:** it stays a random UUID (`discovery.NewID`), not a hash
  of CID, MAC, hostname, or token. The CID is only a clone witness.
- **Hostname.** Not done. Both send `mister` (§6, IMG-3).

### 2.3 Per-card agent token

**Decision:** on first boot, if no agent token exists, the kit draws
one from the CSPRNG and stores it mode `0600` on the FAT. The image
has no token. A later image does not replace an existing token file.
Logs never print it.

**Decision:** once pairing ships, kit images are unprovisioned.
`FES_UNPROVISIONED=1` is that path today; making it the default is
**Q7**. `CI=true` already skips host pickup. `AGENT_CONFIG` stays for
a nonstandard kit. Until then the explicit-add fallback may still bake
a card, and that card is still not cloned. The launcher token is not
part of this decision (**Q10**).

### 2.4 How the host learns a kit

Discovery lists unpaired nodes: `target_id`, MAC, hostname, observed
address. No credential. Listing is not trust.

**Decision:** pairing is explicit. The idle kit, in pairing mode, shows
a short code (strawman 6–8 characters; TTL and rate limit are **Q2**).
"Add kit" lists unpaired kits; the operator confirms the code. The kit
then hands over the token on an exchange bound to that code. Strawman:
a PAKE, or an HMAC of the code (**Q1**). Do not send the token to any
other discovered service, and do not log it. The host writes one
`[[targets]]` row on the existing private path, mode `0600`: name,
address, the kit's id, inline token, `enabled`. The host does not mint
the id.

**UX: Foggy to fill/refine.** "Add kit", the confirm step, and the kit
screen while it waits. The sofa must not look enrolled early.

Physical presence (a button or pad combo within a few seconds) is an
alternative, not the choice. Code, presence, or both is **Q3**.
Explicit-add (address plus token) stays the multicast-blocked
fallback, not the default. A kit that already has a token and is not
pairing is not listed. Another host's kit is **Q8**.

### 2.5 Token storage, redaction, rotation

The host token stays inline in the owner-only config. GET returns
`agent_configured` only. Manifests store a digest, never the token.

**Decision:** rotation is host-initiated on the authenticated channel.
The kit replaces its file; the host replaces the inline token in the
same private write. Revoke deletes the row and, by a local kit action,
returns the kit to pairing and discards the token. Optional
`agent_file` indirection is **Q6**, not this slice.

### 2.6 Clone detection

**Decision:** two live nodes with the same `target_id` or the same MAC
are refused for pair and for place. The UI shows the collision. The
host does not pick one.

**Decision:** the identity file records the CID it was written with.
A different CID means the file did not come from this card: delete id
and token, mint new ones, return to pairing. Do not keep serving the
cloned credential. A reflashed card with no identity file is a normal
first boot (§2.3). Today's TXT has no MAC; where the host learns it
is **Q9**. Adding MAC or hostname is not a byte freeze.

### 2.7 Failure modes

| What happened | What the host does |
| --- | --- |
| Multicast blocked | No unpaired list. Explicit-add still works. Do not scan the subnet. |
| Code expired or wrong | Pairing fails. No token stored. A new code waits out the rate limit. |
| Reimage (FAT wiped), same card | New token and `target_id`; same MAC. Old row fails auth; host flags "same MAC, new id" and offers re-pair. |
| Stale address (#259) | Read ABIs and health on the reconciled endpoint. Check token and node id. A games GET does not rewrite the address. |
| Both hostnames `mister` | Do not key off hostname. Use `target_id` and MAC. Until IMG-3, show MAC or the short id. |
| Config write fails | Fail closed. Kit stays unpaired. No half-written token. |
| Paired to another host | **Q8.** Do not take its token. |
| Duplicate id or MAC | Refuse pair and place (§2.6). |
| CID witness mismatch | Kit regenerates and waits. The old row will 401. |

**UX: Foggy to fill/refine.** One sentence per row. Do not collapse
these into "offline".

---

## 3. Placement with two kits

Decision 7 is unchanged. This is that order when both kits are real.

`display_preference` is a canonical lowercase UUID (`discovery.ValidID`):
the DisplaySink in front of the person. For `fpga_native` that id is
the kit node id, because the picture stays on the kit. The menu shell
does not win by hosting the menu. A preview is not a preference target.

**Decision:** first match wins. `placement_override` only if that node
can already run the title (a miss does not fall through); else
`display_preference` when it is an eligible DisplaySink; else the
last-play DisplaySink, same test; else the first eligible kit in
node-id order. Last-play is host memory. With two kits and no
preference, the first play after a host start goes to the first kit
(maybe not the selected target); later plays follow the last sink.

An explicit `target` on `POST /api/v1/session/launch` skips placement.
Rooms, tenfoot, the browser, and the CLI omit `target`, so they place.

**Eligibility.** Paired (configured, enabled), reachable, mesh-major
OK, and node-document `abis` match. The read is
`GET /v1/mesh/content/node` with that kit's token. No ABIs means not
eligible: failed read, wrong node id, disabled, or unconfigured.

**Decision (#259):** read at the endpoint `AdoptEndpoint` reconciled,
and still check token and node id. Leave `s.targets` until a settings
write changes it. Today the read uses the stale address, the client
adopts, `s.targets` does not, ABIs are empty, and the kit is
`fail_closed`. Kit B at `.85` with `kit2` still set to
`192.168.10.212` is that case. HIL2 v2 sidesteps it by writing `.85`
into its temporary configs, so HIL2 can run without the fix, but it
then does not exercise the reconcile path and Deano's everyday config
stays broken for kit B (**Q13**). The fix does not turn placement on.

**Lease.** Claim the chosen kit before the bind moves. Fail closed on
a foreign holder. Confirm does not steal.

`POST /v1/kit/claim` is bearer-authenticated. The body is exactly
`request_id`, `owner`, `purpose`. `request_id` is 32–128 hex
characters. `owner` and `purpose` are non-empty, at most 160
characters, no control characters. Malformed is 400
`KIT_LEASE_INVALID` before the busy check. A retired id is 403
`KIT_LEASE_REQUIRED`. Blocked is 503 `KIT_LEASE_BLOCKED`. Busy is 409
`KIT_LEASE_BUSY`. Renew and release use `X-FogCast-Kit-Lease` and an
empty body. Powerboat's owner string is `fogcast@powerboat`.

`POST /api/v1/session/stop` is separate. An empty body releases.
`{"retain_lease":true}` keeps the grant. `{"release_idle":true}`
releases the idle grant.

**Known gap:** rebind does not release the kit it left. That release
is a gate (§4), not a default flip. #270 P2-4: an idle Stop on a menu
kit reprograms the FPGA (HDMI blip, generation revoke), so a release
done as that Stop has a picture cost (**Q5**). A busy chosen kit is
**Q4**. #270 P2-6: runtime and agent stay image-lockstep. `placement =
true` does not turn `ensure` on. Both default off.

### Gates before either default turns on

Not in this draft's PRs.

**Placement default-on** needs all of: HIL2 green (§5), replacing the
2026-09-25 `ACCEPTED_PARTIAL`; #259 fixed; rebind releases the old
lease, with the #270 P2-4 cost answered (**Q5**); #163 inside a
budget (**Q11**); pairing shipped; clone detection shipped.

**Ensure default-on** needs those, plus #177 (unreachable
`launcher.json` host falls back to legacy launch, not
`ContentMissingError`; the issue names #172, the kit home host, as a
dependency), #175 (`mesh_content = false` must not open and register
an empty mesh store the host can dial), #178 (dial the explicit `LaunchOn`
target when the selected target is offline), and a separate ensure
HIL with Deano. HIL2 leaves `ensure` unset, so it does not count.
#174 is not a gate: pass the `LaunchOn` context into
`activateMeshExecutor` (callers use `context.Background()`; the nearby
probe timeout is 5 seconds).

---

## 4. Lane 1 hardening

One small PR each, in order. No PR flips a default or writes media.

| Slice | Issue | Scope | Depends on | Tests |
| --- | --- | --- | --- | --- |
| 1 | #259 | ABIs from the reconciled endpoint; still check token and node id. | None. First, for HIL2/everyday two-kit. | Stale address, adopted node id, ABIs present. Wrong id stays ineligible. |
| 2 | #177 | Missing content source falls back to legacy launch, not `ContentMissingError`. | #172 (issue open; pad work is on main per phase 3). | Host down: Phase 0. Source up, ensure on: unchanged. |
| 3 | #163 R-PERF | Stop per-game, per-variant synchronous kit HTTP with no context or timeout on `GET /api/v1/games`. Cache or short TTL, context, client timeout, `ctx.Err()`. Prefer batched `/v1/mesh/content/slots`. | None for the cache. | Hung kit returns inside the timeout. Two kits stay bounded. |
| 3b | #163 R2–R7 | Optional. R2 lost lease reported free. R3 lease view compares the selected target. R4 `meshReadyClient` must not fall back to that client. R5 `FillCopy` / `applyMeshFacts`. R6 document `next_action`. R7 hoist `meshMajorOK`. | The ready path. | One case per nit that changes behavior. |
| 4 | #175 | `mesh_content = false` does not register the mesh store. | None. | Flag false: no mesh routes. Flag true: store serves. |
| 5 | #178 | Ensure dials the named `LaunchOn` target when the selected target is offline. | #177 if both touch activate. | Selected down, named up. |
| 6 | #174, optional | Pass the `LaunchOn` context into `activateMeshExecutor`. | None. | Cancel does not outlive the caller. |
| 7 | To be filed | Rebind releases the previous lease. Not #270 P2-4's idle Stop unless **Q5** accepts the blip. | #259. | Old grant gone, new held. A failed claim keeps the old grant. |

---

## 5. Acceptance

HIL2 is the 2026-09-28 plan, pinned to main `d7e13eaa`. One release:
`pre0` on kit B, `pre1` on kit A, via `fes-update`. Configs stay in a
mode `0600` run root, not the repo. `placement = true` is temporary.
`ensure` is unset. Deano is present. This accepts placement and the
lease on the hand-written `kit2` row. It does not accept pairing.
Green replaces the 2026-09-25 `ACCEPTED_PARTIAL` as the Slice 6
close-out and does not default placement on.

| Step | What happens | Required? |
| --- | --- | --- |
| C0 | Host up. Labelling launch names kit A. | Yes |
| C1b | Foreign claim on B fails closed. No-target launch returns 403 `KIT_LEASE_DENIED`. | Yes |
| C1 | No-target launch rebinds to B. B held by `fogcast@powerboat`, renewing. A free. | Yes |
| C1-pad | Deano moves kit B's own pad. | Optional, chosen |
| C2 | Second claim on B is 409 `KIT_LEASE_BUSY`. | Yes |
| C3 | Soft-stop `{"retain_lease":true}`. B stays held. | Yes |
| C3b | Relaunch reuses the lease. | Optional |
| C4 | Empty stop. B is free. | Yes |
| C5 | `GET /api/v1/mesh/nodes` is exactly A and B. | Yes |
| C1-back | `display_preference` is A, selected target is B, no-target launch places on A. | Optional |
| C6b | Placement off keeps today's bind. | Yes |
| C6a | Ensure never ran. | Yes |
| C7 | Restore: session idle, host stopped, both leases free, boot ids unchanged, Deano's config sha unchanged. | Yes |

If #259 is merged before the run, add one cheap step (strawman
**C0-stale**): a copy of the pref-b config with kit B at the stale
`192.168.10.212`; C0's games read must show kit B eligible. Without
#259, HIL2 runs as written on `.85` configs (**Q13**).

### P-pair, after pairing exists

Deano present. Scratch host config, mode `0600`, outside the repo.
Remove `kit2` only from that file. Do not print tokens.

1. Pair kit B with the new flow.
2. The row's id and token are kit B's, not kit A's.
3. Mode stays `0600`. The token is absent from logs and from GET.
4. A second scratch row that repeats B's `target_id` is refused.
5. Re-run C1 on the paired row, not the hand-written token.

HIL2 without P-pair does not close §2.

---

## 6. Image-side dependencies

Image slices, not pairing PRs. They do not write a block device.

**IMG-2 and IMG-3, one `S20mister-network` change.** Line 32 is
`udhcpc -f -q -t 5 -T 2 -i eth0 -x hostname:mister` (also backgrounded,
pidfile `/run/udhcpc.pid`). `-q` exits after the lease, so there is no
renew. The hostname is the literal `mister`. **Decision:** daemon
`udhcpc` (drop `-q`, keep a pidfile, `-b` or the existing `-s` script)
and `-x hostname:mister-<last three MAC bytes>` after
`S15fes-ethaddr`. Kit B becomes `mister-9dacd6`. Never override an
explicit hostname. Kit A's hand-set `ethaddr` stays.

**First-boot token.** A script beside `S15fes-ethaddr`, after the FAT
is mounted, for §2.3. Only when the token file is absent. Never print
it. Failure leaves a diagnostic, not a shared default token.

**#278.** `fes-update` does not prune. Kit A staging failed 422
`UPDATE_INVALID` with 14 stale 64 MiB images. **Decision:** keep good,
previous, and factory; say when the failure is space; never touch
`u-boot.txt`. Loop-mounted images are **Q12**.

After pairing, new cards ship without agent credentials (**Q7**).

---

## 7. Open questions and sequencing

Strawmen live in this file. A **Q** item is not a lock.

- **Q1.** Pairing crypto: PAKE, HMAC-of-code, or another. Review first.
- **Q2.** Code length, alphabet, TTL, rate limit. Strawman 6–8 characters. Numbers unset.
- **Q3.** UX: on-screen code, physical presence, or both. Foggy. Explicit-add stays.
- **Q4.** Busy lease: next eligible candidate, or fail closed on the current bind?
- **Q5.** Rebind release: lease API only, or an idle Stop? #270 P2-4 reprograms on that Stop.
- **Q6.** Is `agent_file` worth a slice, or does the inline token stay?
- **Q7.** After pairing, does `make media` default to unprovisioned, with opt-in bake?
- **Q8.** Kit paired elsewhere: refuse, or local re-pair that rotates the token?
- **Q9.** Where the host learns MAC: discovery TXT, authenticated health, or both.
- **Q10.** Does pairing stop baking `launcher.json`? That token is not the agent token.
- **Q11.** R-PERF budget: `GET /api/v1/games` p95 under N ms, two kits, one hung, timeout-bounded. N unset.
- **Q12.** #278 keep-set: good, previous, factory only, or also any loop-mounted image?
- **Q13.** Run HIL2 now on `.85` configs (workaround), or land #259 first and add C0-stale?
- **Q14.** Until pairing ships, is "copy the card's token into a hand-written host row" the blessed interim path for new kits (as done for `kit2`)?

### Sequence

Relative weeks, not dates. Kit steps wait for Deano. IMG-2/IMG-3
and the first-boot script land before P-pair. #278 does not block HIL2.

| When | What | Does not include |
| --- | --- | --- |
| Week 0 | #259 PR in parallel with HIL2 prep; HIL2 per **Q13**. | Pairing. A default flip. |
| Next week | Pairing review with Foggy (Q1–Q3, Q8, §2.7 copy). | Enrollment code. |
| Following two weeks | Kit first-boot token, then host pairing. P-pair on a scratch config. | `placement` default on. |
| Next | §4 in order, one PR each. File the rebind issue first. | Ensure default. |
| After the gates | Review. Flip a default only in a follow-up that shows every gate green, plus an ensure HIL. | A silent switch. |
