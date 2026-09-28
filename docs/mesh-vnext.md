# Mesh v-next: onboarding/pairing, two-kit placement, hardening slices, HIL2 acceptance

**Status:** draft. Not a wire freeze, not an implementation claim, and
not a default flip for `[mesh] ensure` or `[mesh] placement`. Builds on
#131 ([mesh LAN](mesh-lan.md), [node protocol](mesh-node-protocol.md))
and [phase 3](mesh-phase3.md): Slices 1–6 (#212–#221) plus the
Slice 7 tie-breaks and Slice 8 host wiring, all behind
`[mesh] placement = false`. The Slice 6 two-kit HIL (HIL2) is GREEN,
picture only, as diagnostic evidence; it is not hardware acceptance
until kit B is designated (§5). Follow-up edits (UX copy, §7 decisions, pairing
slices, #280 Codex findings) are marked where they change a decision.

**Owners:** Caster owns the technical design. Foggy owns UX and copy
for setup and pairing; §2.4, §2.7 and §3 carry Foggy's copy
(2026-09-28); *[tech fix]* marks Caster's corrections to it. Bob
coordinates and reviews. Deano approves and merges.
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
| Placement is Decision 7, behind `[mesh] placement`, default off. Slice 6 HIL was open. | Two-kit placement is concrete: reconciled endpoint, lease before bind. HIL2 is diagnostic evidence; Slice 6 closes once kit B is designated (§5). |
| Ensure stays default off until #177. Other nits are not a phase. | Lane 1 is one small PR per issue. Defaults stay off until the §3 gates. |
| Acceptance is named in the phase briefs. | HIL2 (C0–C7) observed placement and lease on already provisioned kits (diagnostic until kit B is designated, §5). P-pair is later. A green HIL2 does not claim pairing. |

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
one from the CSPRNG and writes it under `/media/fat/fogcast/`, the
only writable persistent store (the rootfs is a read-only loop image).
The image has no token. A later image does not replace an existing
token. Logs never print it.

*[#280 Codex P1, adopted]* `/media/fat` is FAT/exFAT: no per-file
owner or mode, so "mode `0600`" means nothing there. Protection is a
mount-wide policy: root-owned with `fmask=0177,dmask=0077` (or
tighter). The agent checks the effective mode at start and reports a
boolean in authenticated health; a bad mount is a diagnostic, not a
token print. Threat model unchanged from today's `agent.toml`: whoever
holds the card holds the credential.

**Decision:** once pairing ships, kit images are unprovisioned by
default (Q7). `AGENT_CONFIG` stays for a nonstandard kit. Pairing
also provisions the launcher token (Q10, §2.4b).

### 2.4 How the host learns a kit

Discovery lists unpaired nodes: `target_id`, MAC tail, hostname,
observed address. No credential. Listing is not trust.

**Decision:** pairing is explicit and code-only (Q3). An unpaired idle
kit shows a code; a paired kit enters pairing only by a local action.
The host proves knowledge of the code in a **PAKE** (Q1: CPace, or
SPAKE2 if CPace fails the license/maintenance check); the kit's agent
token and the host-minted launcher token travel only inside the
PAKE-keyed AEAD channel. The transcript binds kit `target_id` and host
id. Code: 6 characters shown 3+3 from a 31-symbol alphabet (`2-9`,
`A-Z` minus `I L O`), case-insensitive, 5 min TTL, 3 wrong tries then
a new code and a 30 s lockout that doubles on each consecutive lockout
up to 10 min; counters are per kit, not per client address (Q2). The
host writes one `[[targets]]` row on the existing private settings
path (owner-only file): name, address, the kit's id, inline token,
`enabled`. The host does not mint the id. **Locked (Q15):** no machine
is ever added without its pairing code, including the first kit on a
new computer. Explicit-add under Advanced (multicast-blocked networks)
is address **plus the TV's code**, the same PAKE run at a typed
address; raw token entry is not a product path once pairing ships.
**Locked (Q16):** one computer per machine; a paired kit refuses
pairing with a second host (Q8).

**UX (Foggy).** The TV says "machine", never "kit", "node", "target"
or "executor". The main line shows no UUIDs, IPs, tokens, owner strings
or error codes (those go to host diagnostics). Each cause gets its own
sentence; nothing collapses into "offline". A machine never looks
ready or paired until the host has saved its row and an authenticated
call to it has succeeded.

*On the kit's TV*
- An unpaired, idle kit shows its pairing code on its home screen by
  itself. A paired kit enters pairing only through Settings → "Pair
  with a computer"; that is also the revoke and re-pair path.
- Waiting: "Pair with FogCast" / "On your computer, choose Add machine
  and enter this code:", the big code (e.g. `K7M 4QX`), "New code in
  {m} min."
- After the host sends the code: "Pairing…" (not success).
- Only after the row is saved and authenticated health succeeds:
  "Paired with {host name}" for about 3 s, then the home screen.
- Failure: "Pairing didn't finish" / "Nothing was saved. A new code
  will appear in {n}s."

*On the host (Settings → Machines)*
1. "Add machine" opens the list of unpaired machines: label and
   address. Label = hostname, or "MiSTer …9dacd6" (last three MAC
   bytes) until IMG-3 lands. No UUIDs on the main line.
2. "Enter the code shown on that machine's TV." Case-insensitive,
   with or without the space.
3. "What do you call this machine?" Prefilled "Machine 2", editable;
   saved in the same write as the code confirmation.
4. Success: "{name} is ready" / "Games can now play on {name}." Done.
- Nothing listed: "No new machines found" / "Check it's switched on
  and showing a pairing code. Still missing? Add it by address." The
  link opens explicit-add under Advanced (address plus the TV's code),
  never the default.
- Rows with a duplicate id or MAC are shown but not selectable (§2.7).

### 2.4b Launcher token and kit B's "Offline – local library"

Kit B's footer came from its `launcher.json`: it points at
`192.168.10.202:8789`, where no launcher listener ran with a matching
launcher token. That is a second credential (`prepare_launcher.py`,
separate from the agent token). *[tech fix]* The host listener today
authorizes **one** paired kit (`LauncherConfig`: one token, one
`target_id`) and refuses non-content operations unless that kit is the
host's selected target (`TARGET_MISMATCH`). So a second kit's menu
cannot work even with a token.

**Decision (Q10):** one pairing provisions both. The host mints a
per-kit launcher token and sends it, with its launcher URL, inside the
PAKE channel; the kit writes `launcher.json`. The listener accepts a
set of per-kit launcher credentials, and a launch from a kit's own
menu targets that kit (slices P4a/P4b, §4).

**UX (Foggy), split the footer causes:**
- Host unreachable or no listener: the kit can't tell these apart, so
  it shows the #275 offline banner "Offline, showing your saved list".
  The host's Machines list carries the cause: "Can't reach this
  computer's game service".
- Reachable but auth refused (401/403): "FogCast on {host name} didn't
  accept this machine" / "Pair it again from your computer."
- Never paired: the §2.4 pairing screen, not "Offline".
- Host Machines list status words, one sentence each: Ready, In use
  ("Someone else is playing on {name}."), Can't reach, Needs pairing
  again, Needs an update, Setting up, Copied card.

### 2.5 Token storage, redaction, rotation

The host token stays inline in the owner-only config (Q6: no
`agent_file` indirection). GET returns `agent_configured` only.
Manifests store a digest, never the token.

**Decision:** *[#280 Codex P1, adopted]* rotation is staged, because
two machines can't share one atomic write. (1) Host asks the kit to
stage a new token (authenticated with the current one). The kit then
accepts old and new. (2) Host writes the new token to its config.
(3) Host authenticates with the new token and calls commit; the kit
drops the old one. An uncommitted staged token expires after 10 min
and the old one stays valid, so a crash at any step is recoverable.
Revoke deletes the row and, by a local kit action, returns the kit to
pairing and discards its tokens.

### 2.6 Clone detection

**Decision:** two live nodes with the same `target_id` or the same MAC
are refused for pair and for place. The UI shows the collision. The
host does not pick one.

**Decision:** the identity file records the CID it was written with.
A different CID means the file did not come from this card: delete id
and token, mint new ones, return to pairing. Do not keep serving the
cloned credential. A reflashed card with no identity file is a normal
first boot (§2.3).

*[#282 Codex P1, adopted]* A cloned FAT also copies
`/fogcast/launcher.json` and its per-kit launcher bearer. CID-mismatch
recovery deletes `launcher.json` together with the id and agent token.
The menu then shows the pairing screen, not "Offline", until pairing
(P4b) or the P4a interim step (§4) issues a new launcher bearer. When
the host drops or replaces the old row, it also revokes that row's
launcher bearer, so a clone cannot reach the launcher listener as the
original machine.

*[#280 Codex P2, adopted]* A cloned FAT also copies
`linux/u-boot.txt`, and `S15fes-ethaddr` keeps any existing `ethaddr`.
So CID-mismatch recovery must also drop the `ethaddr` line **when it
equals the MAC derived from the recorded CID**, then re-derive from
this card's CID. A line that doesn't match the recorded CID's
derivation is hand-set (kit A) and is kept; a clone of a hand-set card
still collides on MAC and is refused by the host. `S15fes-ethaddr`
therefore records the CID it derived from.

**Decision (Q9):** the host learns the MAC both ways. TXT carries the
non-secret MAC tail for the unpaired label (the MAC is on the LAN
anyway); authenticated health carries the full MAC and the CID-witness
state for clone checks. Adding TXT keys is not a byte freeze.

### 2.7 Failure modes

| What happened | What the host does | What the user sees (Foggy) |
| --- | --- | --- |
| Multicast blocked | No unpaired list. Explicit-add works. No subnet scan. | Host: "Can't see new machines on this network. You can still add one by address." |
| Code expired or wrong | Pairing fails; nothing stored. Lockout per §2.4. | Host: "That code didn't match or has expired. Use the code on the TV now." After the limit, both screens: "Too many tries. New code in {n}s." |
| Reimage (FAT wiped), same card | New token and id, same MAC. Old row 401s; host flags "same MAC, new id". | Host: "{name} was reset and needs pairing again." Action "Pair again" keeps the name and replaces the row. TV shows a code. |
| Stale address (#259) | Read ABIs and health at the reconciled endpoint; check token and id. | Nothing when reconcile works. Else: "Can't reach {name} at its last address. Looking for it…" |
| Both hostnames `mister` | Never key off hostname; use `target_id` and MAC. | No copy. Label is the user's name, or "MiSTer …{MAC tail}". |
| Config write fails | Fail closed; kit stays unpaired; no half-written token. | Host: "Couldn't save {name}. Nothing was changed. Try again." TV: "Pairing didn't finish." |
| Paired to another host | Refuse (Q8; one computer per machine, Q16). Never take its token. | Host, greyed row: "Already paired with another computer." |
| Duplicate id or MAC | Refuse pair and place (§2.6). | Host: "Two machines are claiming to be the same one. This usually means an SD card was copied. Set one of them up fresh before playing." |
| CID witness mismatch | Kit regenerates id, token, derived MAC and deletes `launcher.json` (§2.6); old row and its launcher bearer 401. | TV: "This card was copied from another machine, so it's being set up fresh." Then a code. Host's old row shows the reimage sentence. |

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
`192.168.10.212` is that case. HIL2 ran GREEN with `.85` in its
temporary configs, so it did not exercise reconcile; #259 (slice L1,
in progress) adds optional C0-stale. The fix does not turn placement on.

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

**Known gap (#281, slice R1):** rebind does not release the kit it
left. That release is a gate, not a default flip. #270 P2-4: an idle Stop on a menu
kit reprograms the FPGA (HDMI blip, generation revoke), so a release
done as that Stop has a picture cost; the release uses the lease
API, not a Stop (Q5, #281). A busy chosen kit fails closed; the host
never moves the game silently (Q4). #270 P2-6: runtime and agent stay image-lockstep. `placement =
true` does not turn `ensure` on. Both default off.

### UX: which machine a game plays on (Foggy)

- **At a machine's own TV menu:** the game plays on that machine with
  no question (the Slice 5 no-prompt rule). *[tech fix]* This is an
  explicit target on the launch from that kit's launcher, which skips
  placement; it does not write the household `display_preference`.
  It needs P4a (today only the single selected kit's menu can launch).
- **From the computer (browser, rooms, CLI):** a "Plays on {name}"
  chip beside Play, only with two or more eligible machines. It shows
  the real placement answer, never a guess *[tech note: the games row
  `placement` field, computed per request, is that answer]*, which
  also covers the first-play-after-restart surprise. Tapping it offers
  "Just this time" *[tech fix: an explicit `target` on that launch,
  not `placement_override`, which is a host-wide config key]* or
  "Always play here" (household-wide: the single `display_preference`,
  locked Q17; needs a settings write route, cf. #211).
- **Override miss:** "{title} can't play on {name}." If another
  machine is eligible, offer "Play on {other}" explicitly. Never fall
  through silently.
- **Busy (Q4):** from the computer "{name} is in use. Play on {other}
  instead?"; at the TV plain "In use" (#275).
- **During play:** "Playing on {name}".
- **Not eligible:** #275 §4 state copy. Version skew: "{name} needs an
  update before it can play this."

### Gates before either default turns on

Not in this draft's PRs.

**Placement default-on** needs all of: HIL2 green (§5: ran 2026-09-28, picture only, diagnostic
until kit B is designated; C1-pad still owed); #259 fixed; rebind releases the old
lease via the lease API (#281); #163 inside the Q11 budget; pairing shipped; clone detection shipped.

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

## 4. Implementation slices (ordered)

One small PR each. No PR flips a default or writes media. Kit HIL
waits for Deano.

| # | Slice | Scope | Depends on | Tests / acceptance |
| --- | --- | --- | --- | --- |
| P1 | Kit identity and code (image + agent + `fogcast-kit`) | First-boot token under `/media/fat/fogcast/` with the mount check; CID witness; clone recovery incl. derived `ethaddr` (§2.6); unprovisioned kit shows the pairing code on its idle screen; local "Pair with a computer". | IMG-2/IMG-3 can land alongside. | Image test: never overwrite token or hand-set `ethaddr`; clone fixture regenerates and deletes `launcher.json`; token absent from logs. |
| P2 | Pairing handshake (kit endpoint + host API) | Kit pairing endpoint live only in pairing mode; PAKE; TTL, tries, lockout/backoff per §2.4; host writes the row via the private settings path; duplicate id/MAC refused; staged rotation (§2.5). | P1. | Unit: wrong code, expiry, lockout doubling, duplicate refusal, crash-at-each-rotation-step recovers; no token in logs or GET. |
| P3 | Settings UI hookup (Foggy/Luna) | Machines list, Add machine, code entry, name, status words, §2.7 copy. Hand-edited TOML stays developer-only. | P2. | UI tests per state; copy review by Foggy. |
| P4a | Multi-kit launcher listener (**pulled forward, in progress**) | Listener accepts both #287 credential forms (below). *[#282 Codex P1, adopted; aligned with #287 as built]* The host still has ONE foreground session (`sources/FogCast/docs/ARCHITECTURE.md:70-75`). The launcher serves the game list, platforms, health, attract and cache reads to any enabled paired kit, whatever target is selected. A kit-menu launch explicitly targets the kit that asked and never goes through placement. `GET /api/v1/session` shows the real session only to the kit that owns it; every other kit sees its own idle view. Stop, status and input are OWNER-ONLY: a non-owner gets 403 today (a distinct `NOT_SESSION_OWNER` code is a parked nit). A launch from kit X while another kit's session is active returns 409 `SESSION_BUSY_OTHER_KIT` and does not preempt; browser and API launches that name a target still preempt, as today. **Not in P4a:** per-kit concurrent sessions and per-kit Stop (#288). Launcher health, `rom_cached` and the library cache still describe the selected target (#289). Independent of the PAKE slices; interim credentials come from the operator step below (Q14 covers only agent tokens). Fixes kit B "Offline". | None (interim credentials below). | Two kits' menus both browse the host and each launches on its own TV. Stop and status are owner-only: a non-owner gets 403 and the owner's session is untouched. A kit launch while another kit's session is active gets 409 `SESSION_BUSY_OTHER_KIT` and the other session keeps running. A non-owner's `GET /api/v1/session` returns its own idle view. Wrong token 401; no token in logs. |
| P4b | Launcher provisioning via pairing | Host mints the per-kit launcher token in P2's channel; kit writes `launcher.json`. | P2, P4a. | Pairing alone makes the kit menu work. After clone recovery and re-pair, the old launcher bearer is rejected (401). HIL: P-pair (§5). |
| R1 | #281 | Successful rebind releases the old kit's lease via the lease API (best-effort, logged); failed rebind keeps it. | None. | Unit, both paths. |
| L1 | #259 (in progress) | ABIs from the reconciled endpoint; keep token and node-id checks. | None. | Stale address → eligible; wrong id ineligible. Optional HIL **C0-stale** (§5). |
| L2 | #177 | Missing content source → legacy launch, not `ContentMissingError`. | #172 (issue open; pad work on main per phase 3). | Source down: Phase 0 path. |
| L3 | #163 R-PERF (+R2–R7 optional) | Snapshot/TTL, ctx, per-kit timeout, `ctx.Err()`; batch slots read preferred. R2 lost lease ≠ free; R3/R4 lease view and client use the bound node; R5–R7 as filed. | None. | Q11 budget with one hung kit. |
| L4 | #175 | `mesh_content = false` does not open/register the mesh store. | None. | Flag false: no mesh routes. |
| L5 | #178 | Ensure dials the named `LaunchOn` target when the selected one is offline. | L2 if both touch activation. | Selected down, named up. |
| L6 | #174 (optional) | Pass the `LaunchOn` ctx into `activateMeshExecutor`. | None. | Cancel doesn't outlive caller. |

P4a is pulled forward and runs now, in parallel with P1–P2. Lane 1 can
also run in parallel where owners differ; the order above is the merge
priority.

**P4a credential forms (#287).** The listener accepts both forms:
- One shared listener token mapped to a set of `target_id`s. Both kits
  carry this today.
- Per-kit bearers, each mapped to its own `target_id`. These arrive
  through the interim step below (#290) and later P4b; none is
  deployed yet.

**P4a interim launcher credentials.** *[#282 Codex P1, adopted;
confirmed (Caster/Bob, 2026-09-28); replaced by P4b]* Q14 only puts an
agent bearer in a `[[targets]]` row; it cannot supply launcher bearers.
Until P2/P4b ship, an operator runs a private provisioning step per
kit. It is operator-run only, and it is the implementation of #290
(`scripts/prepare_launcher.py` currently rejects the `pairings` form):
1. Mint a per-kit launcher bearer from the CSPRNG, at least 128 bits
   (for example 32 random bytes in base64url).
2. Write it into the owner-only `launcher-host.json` as a `pairings`
   entry keyed by the kit's `target_id`. The tool refuses a duplicate
   `target_id`, and refuses a bearer that equals the host token or
   another kit's bearer. The file is written atomically with mode
   `0600` and stays an owner-only regular file, as
   `sources/FogCast/docs/launcher-host.md` requires today.
3. Write the matching card `launcher.json` (API URL, bearer, identity)
   to media or a staging output. The tool writes to a live card's
   `/media/fat/fogcast/` only when the operator passes an explicit
   flag. The card is protected by the FAT mount-wide policy and health
   check of §2.3 (#280), not a per-file mode.

Tokens are never printed or logged; output shows only a sha256 prefix.
A per-kit bearer is never shared across kits or baked into an image.
To rotate: re-mint, update both files, and restart the host. The old
bearer stops working when the host restarts. Existing shared-token
entries keep working, because #287 is backward compatible; moving a
kit to its own bearer is optional until P4b. P4b later replaces this
path with pairing-provisioned bearers and migrates existing
`launcher-host.json` entries: keep the kit's key and re-issue its
bearer at first pairing. This interim path was chosen over making P4a
depend on P4b because today's single-kit launcher setup already uses
exactly these two files; the step only generalizes it to a set.

This does not conflict with Q15. It is operator tooling in the Q14
interim window, not a way to add machines in the product.

**Known issue (#294, kit-side; found in HIL3).** When the host goes
away or returns errors, the kit menu keeps a stale "active" session,
so HDMI stays black until `fogcast-kit` restarts.

---

## 5. Acceptance

**HIL2: GREEN, picture only; HIL-observed diagnostic evidence, not
hardware acceptance** (2026-09-28 18:37–18:40 Sofia, main
`d7e13eaa`, both kits on image `f449886f` via `fes-update`, `.85` in
the temporary configs, `ensure` unset). All required steps green;
C1-back and C3b green; **C1-pad deferred** to a session with Deano
(kit B's pad must enumerate first). Evidence: Caster's
`fogcast-MESH-P3-S6-HIL2-RESULT.txt` and the Powerboat run root.
*[#282 Codex P1, adopted]* Kit B (`.85`) is not a designated fixture:
the "Dedicated fixture" section of `sources/FogCast/docs/DEVELOPMENT.md`
(:203-214) names only `192.168.10.84`. So this record shows placement
and the lease working on the hand-written `kit2` row as diagnostic
evidence. It does not replace the 2026-09-25 `ACCEPTED_PARTIAL` as the
Slice 6 acceptance, does not accept pairing, and does not default
placement on.

**Pending: designate kit B.** Once `.85` is designated in that
DEVELOPMENT.md section (a separate owner change, not made here), this
record, or a re-run on the designated kits, can be promoted to the
Slice 6 acceptance.

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

**C0-stale (optional, #259 acceptance):** a copy of the pref-b config
with kit B at the stale `192.168.10.212`; the games read must show kit
B eligible via the reconciled endpoint.

**HIL3, P4a (#287): GREEN; HIL-observed diagnostic evidence, not
hardware acceptance** (2026-09-28 19:25–19:31 Sofia, combined build
`5342f4a1`). With the host's selected target on kit A and both kits
on the shared listener token, kit B's menu came online and launched on
kit B; a kit-A launch then got 409 `SESSION_BUSY_OTHER_KIT` without
preempting, kit-A Stop and status got 403 while its session read was
its own idle view, and kit B's own Stop released its lease. Evidence:
Caster's `fogcast-MESH-HIL3-RESULT.txt`. Same caveat as HIL2: only
`.84` is designated, so this does not accept P4a on hardware.

**HIL hygiene (from HIL2):**
- HIL hosts use an isolated data dir, never the operator's
  `~/.local/share/fogcast`: HIL2 added five FES Pong plays to Deano's
  recents and play counts because the host resolves data dirs from
  `$HOME`.
- Wait about 3 s after a launch reports active before grabbing frames
  (at 1 s the Pong background shows before sprites).
- Never compare brightness (YAVG) across capture cards: ASUS shows
  Pong black as ~0, ShadowCast as ~16. Compare frames per card.

### P-pair, after pairing exists

Deano present. Scratch host config, mode `0600`, outside the repo.
Remove `kit2` only from that file. Do not print tokens.

1. Pair kit B with the new flow.
2. The row's id and token are kit B's, not kit A's.
3. Mode stays `0600`. The token is absent from logs and from GET.
4. A second scratch row that repeats B's `target_id` is refused.
5. Re-run C1 on the paired row, not the hand-written token.
6. Kit B's menu leaves "Offline" and browses the host (P4a, then P4b).
7. Run the deferred C1-pad in the same session.

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
previous, factory, pending/trial if set, the running image, and
*[#282 Codex P1, adopted]* every image currently backing a loop device
(isolated root-switch or watchdog diagnostics), all protected before any
oldest-first deletion; say when the failure is space; never touch `u-boot.txt` (Q12). Test: an older image attached to a
loop device but otherwise stale survives pruning.

After pairing, new cards ship without agent or launcher credentials (Q7).

---

## 7. Open questions and sequencing

Triage 2026-09-28 (Caster, with Foggy/Bob views and Deano's Q14).
DECIDED items change only by editing this file.

| Q | Status | Decision | Why |
| --- | --- | --- | --- |
| Q1 | DECIDED | PAKE (CPace; SPAKE2 fallback). Tokens only inside the PAKE-keyed AEAD. | A ~30-bit code under HMAC is brute-forced offline in seconds from a sniffed transcript; a PAKE allows one online guess per try. |
| Q2 | DECIDED | 6 chars 3+3, 31-symbol alphabet (no `0 O 1 I L`), 5 min TTL, 3 tries → new code + 30 s lockout, doubling to 10 min, per kit. | ~29.7 bits; ≤3 guesses per code. Backoff (Caster's addition to Foggy's numbers) caps a LAN attacker near 430 guesses/day, ~5·10⁻⁷ per day. |
| Q3 | DECIDED | Code only; explicit-add under Advanced. | A code on the TV already proves presence; pad combos are undiscoverable and clash with games. |
| Q4 | DECIDED | Fail closed; offer the other machine as an explicit choice. | A silent move puts the picture on a TV nobody is watching. |
| Q5 | DECIDED | Release via the lease API (#281), never an idle Stop. | Stop reprograms a menu kit (#270 P2-4): visible HDMI blip. |
| Q6 | DECIDED | No `agent_file`; token stays inline in the owner-only config. | One secret file, one private writer; indirection adds a second file to protect. |
| Q7 | DECIDED | Yes: `make media` defaults to unprovisioned once pairing ships; baking is opt-in. | Stops new cards inheriting a host token. |
| Q8 | DECIDED | Refuse; move via local "Pair with a different computer" ("This disconnects it from {old host}. Continue?"), which rotates the token. | Ownership changes need someone at the machine. |
| Q9 | DECIDED | Both: MAC tail in TXT for labels; full MAC and CID state in authenticated health. | Unpaired label needs it pre-auth; clone checks need trusted data. |
| Q10 | DECIDED | One pairing provisions agent and per-kit launcher tokens (P4b; listener P4a). | Kit B's "Offline" came from the second token. |
| Q11 | DECIDED | `GET /api/v1/games` p95 ≤ 300 ms warm with 2 kits; ≤ 1 s with one hung kit (per-kit read timeout 500 ms, snapshot TTL); cached list shown at once. | Foggy's sofa target: full list within about a second. |
| Q12 | DECIDED | Keep good, previous, factory, pending/trial, the running image, and every loop-backed image; delete others oldest-first only as staging needs. | Never delete anything boot or rollback can reach. |
| Q13 | DECIDED (moot) | HIL2 ran GREEN on `.85` configs; #259 stays slice L1 with optional C0-stale. | Done. |
| Q14 | DECIDED (Deano) | Interim onboarding: a host `[[targets]]` row with the card's **own** token via the settings UI or PATCH, not hand-edited TOML. Never clone tokens across cards; never log them. | Uses the private write path and redaction until pairing lands. |
| Q15 | LOCKED (Deano, 2026-09-28) | Never add a machine without its pairing code, including the first kit on a new computer; explicit-add is address plus code. | No silent trust on the LAN. |
| Q16 | LOCKED (Deano, 2026-09-28) | One computer per machine; no multi-host pairing. | One owner per kit; moving is the Q8 local re-pair. |
| Q17 | LOCKED (Deano, 2026-09-28) | "Always play here" is household-wide: the single host `display_preference`. | Matches today's config; no per-seat state. |

No questions are open.

### Sequence

Relative weeks, not dates. Kit steps wait for Deano. IMG-2/IMG-3
and the first-boot script land before P-pair. #278 does not block HIL2.

| When | What | Does not include |
| --- | --- | --- |
| Done | HIL2 GREEN (picture only, diagnostic until kit B is designated); #281 filed. | C1-pad; Slice 6 acceptance. |
| Week 0–1 | L1 #259 (in progress) and R1 #281; Foggy review of this copy. | A default flip. |
| Now | P4a multi-kit launcher listener (in progress). | Pairing. |
| Weeks 1–3 | P1 → P2 → P3/P4b (in parallel); IMG-2/IMG-3 alongside P1. | `placement` default on. |
| Week 3–4 | P-pair HIL with Deano (incl. deferred C1-pad). | Ensure default. |
| Then | L2–L6 in order; gates review; flip a default only in a follow-up showing every gate green, plus an ensure HIL. | A silent switch. |
