# FogCast architecture

The corepackage reader recognizes format 4's two ordered ROM requirements and
sealed map. Launch and target activation of that format require the separate
two-source transport and linked-load path before it becomes selectable.
Two-source staging retains its parsed sealed map only within that operation,
reusing it for receipt verification and linking. Publication and restart adoption
independently validate their retained package bytes.

This is the canonical description of the working system.

## Normal FPGA game launch

```text
Browser UI, kit launcher, or `fogcast launch|status|stop`
  -> GET /api/v1/session, POST /api/v1/session/launch, POST /api/v1/session/stop
  -> host session service
  -> target described-package library endpoints
  -> mister-agent session/transfer coordination
  -> /run/mister-runtime.sock, protocol 2
  -> libmister-runtime physical lifecycle
  -> FPGA package and declared media/input contracts
```

The entry points are `internal/hostapi`, `fogcast`, `internal/agent`,
`internal/httpapi`, and `internal/misterruntime`. `internal/systems/table.go`
retains library platform metadata; it does not describe raw-core launch recipes.

The browser, kit launcher, and ordinary CLI send a game ID to the same
persistent host session. `hostclient` owns the UI-independent GET
`/api/v1/session` response model and decoder, and catalog launch
eligibility (`Game.LaunchEligible`) so `ListGames` variant selection and
sofa and kit catalog admission share one predicate. Browser display and launch
dispatch use the same catalog check, matched against the Go rule by
`hostclient/testdata/launch-eligibility.json`. Available state and explicit
`launchable` and `root_online` true are required; missing flags do not grant
eligibility. Catalog eligibility does not replace session or target readiness
gates, nor the host's authoritative launch validation. Tenfoot maps those block codes to
sofa copy. Artwork handles use `hostclient.NormalizeHandle` so host
transport, kit disk cache, and UI retain share one 64-hex rule.
The browser play pane resolves every listed option before enabling Play. A
malformed option in the selected title blocks Play. A backend picked in the
current browser session stays authoritative across an older preference read;
confirmed choices come only from preference reads or successful saves. Saves
for one title run in selection order. A failed save restores the latest
confirmed choice, or leaves the title needing a choice, and Play remains
blocked until detail is retried. Preference reads cannot replace a locally
confirmed choice with an older response.

Shared session decoding and Kit capability checks use
`hostclient.SessionCoreInterface.IsKeyboard` for exact `fes.keyboard` 1.0
recognition. Unsupported versions remain ineligible; input readiness,
generation and session ownership checks still apply separately.
Tenfoot and the kit launcher consume that package for session
polling; the browser keeps its own `parseSession` and shares the common
success/rejection matrix in `hostclient/testdata/session-contract.json`.
That fixture is not a claim of full decoder equivalence. In paired tenfoot
mode, launch and kit mutations require a known, non-foreign scoped kit lease;
an unavailable status is shown as `Can't tell if this machine is free.` while room browsing
continues. Launch, stop,
and input attach/detach stay on their existing endpoints. Explicit input attach
returns the existing stream when it is attached, ready, and bound to the same
observed package, generation, core and input capabilities. It does not rotate
that stream or accept an old binding after a package change. The host resolves installed package entries and explicitly binds library
persistence. The runtime validates the package and declared interfaces before
programming. Bare legacy game records remain browseable but unlaunchable.
Installed FPGA `core_package` rows, and any row on the `fpga` catalog
platform, stay `launchable` even when a cartridge platform such as Coleco
has no host-emulator mapping. Rooms treat those titles as Ready once
composition flags such as `firmware_ready` pass; raw Coleco library carts
remain browse-only.
A locally expired or lost kit lease remains `KIT_LEASE_DENIED` through host
service normalization, session reads, input reconciliation, Launch and Stop.
The host API returns HTTP 403 for this ownership refusal. An ordinary claim
refused by a foreign holder remains HTTP 409 `KIT_LEASE_BUSY`, including during
input reconciliation after endpoint adoption. Neither becomes a disconnected
target or failed input bridge. Unclassified transport and
bridge failures retain their unavailable responses. A refusal does not establish
whether an earlier operation completed; clients must inspect current ownership
before issuing a new operation. Existing authenticated endpoint reconciliation
uses read-only ownership/status observations to discard stale local authority;
it never stops or releases the replacement owner's session. A subsequent
explicit launch uses an ordinary claim, which still refuses a foreign holder.
Input reconnect stops immediately on an explicit lease-denied or busy response.
A CONNECT `/v1/input/stream` refusal of 403 `KIT_LEASE_REQUIRED` — the kit has
already revoked, expired, or superseded the grant — is read from a bounded
response and mapped to the same sanitized `KIT_LEASE_DENIED` before reconnect
sees it. The host does not redial that stale handle for the network reconnect
grace period. This releases the input lock for ownership reconciliation.
Heartbeat and event-driven reconnect both record `lease_lost` rather than a
network reconnect timeout, and event delivery keeps the sanitized refusal.
Transient stream failures retain the bounded reconnect grace. No mutation is
automatically retried after lease loss.

Native FPGA Stop uses the
mutation (`upload_timeout_seconds`) deadline, not the short status request
timeout; programming idle can exceed a 5s health poll.

CLI launch and Stop preserve the caller context deadline without imposing an
additional HTTP timeout that could undercut the host's configured mutation
deadline. Interrupting the CLI cancels the request; it does not replay it or
prove that a target operation was undone. Status reads retain a two-minute
HTTP bound. CLI JSON includes the public input binding (`session_id`, `source`)
and complete typed metrics from `host.RemoteInputStatus`.

`POST /api/v1/session/launch` may include `target` to bind a live FPGA session
to a configured target without rewriting `selected_target`. Omitted `target`
uses the selected configured target and keeps the ordinary placement choice.
A host-only launch always stays on the root session coordinator, even when its
request includes a kit target; host-only session responses leave `target` empty.
Unscoped session events follow the foreground session coordinator, like status
and Stop, so a root-owned host-only session's events remain visible.
A second configured target may be
launched while the first is still playing; `GET /api/v1/sessions` lists those
live plays. `GET /api/v1/session` is the foreground session (the last launch)
and is what sofa and kit attach to for input. Stop of the foreground session
leaves the other target playing. One primary host input remains on the
foreground session; a second kit uses its local pad until surfaces attach by
session id. The kit lease remains the target-side ownership authority.
Each target coordinator scopes its service calls, including live-media
replace/eject and development-media delivery, to that target. Placement
handoff admits the destination coordinator before recording a package play;
failed admission or old-resource cleanup stops the newly placed play on that
destination and preserves the coordinator's prior ownership state.
Cast admission is also target scoped: the host media session keeps cast clients
by configured target, selects one from the coordinator's session context for
each Start, and pins that client on the returned handle for Stop and status
monitoring. A foreground change therefore does not redirect an existing cast
or the next cast for another target.

The host keeps the household display preference and the last play
DisplaySink in process memory (`internal/meshpref`). Both values are
node ids. Empty means unset. Open seeds the preference from
`[mesh] display_preference`. A library FPGA launch on a bound target
records that target's node id (`target_id`) as the last DisplaySink
once the whole launch has succeeded, including its firmware and media
delivery. A launch whose media fails leaves the remembered sink alone.
Development loads, status reads, stops, and host-only play leave that
memory unchanged. The last sink is not written to config. `PlaceOptions` copies
the two ids into `meshplace.Options`.

Production placement is off unless the host config sets
`[mesh] placement = true`. `fogcast-api` and the `fogcast` CLI call
`EnableMeshPlacement` after open, next to `EnableMeshContent`. The
switch does not turn `[mesh] ensure` on. With it on, each launch and
each games read builds the placement ask from the node inventory
(`GET /api/v1/mesh/nodes`): node id, mesh major, Execute kinds,
DisplaySink, and InputSource per advertisement. An `fpga_native` row
carries the `abis` from that node's `GET /v1/mesh/content/node`, read
with the agent token of the enabled configured target whose
`target_id` is that node id (`kitcontent.ReadNode`). The read dials one
endpoint: the node's discovered inventory address when the current
browse window saw that node at exactly one origin; otherwise the origin
the kit's target client verified and adopted (`AdoptEndpoint`) when it
differs from the configured address; otherwise the configured address.
A node id that discovery saw at more than one address is ambiguous
(`address_conflict` on its inventory row, with no `address`); the read
then uses the adopted or configured endpoint and sends the agent token
to neither advertised contender. Rows kept from an earlier window after
a browse error are not a current address, so an adopted endpoint wins
over them. The read does not rewrite the configured target, the config
file, or the target client. The document must
name the same node id. A failed read, another node id, a disabled
target, and a node this host has not configured give no `abis`, which
is not eligibility. A read is reused for ten seconds, a failure is
remembered for five, and one read is bounded at two seconds. The read
is not a kit-lease mutation. An empty inventory is not a placement
decision: the launch keeps today's bind and the games rows carry no
`placement`. `[mesh] placement_override` is the advanced override
node id; it selects that node only when that node can already run the
title. Among several eligible FPGA kits, Place takes the display
preference, then the last play DisplaySink, then the first kit whose
mesh major matches. Among several `native_emu` nodes, when no FPGA kit
can run the title, it takes the first whose mesh major matches;
preference and last sink do not reorder them. Candidates follow the
inventory's node-id order, so with two kits and no preference the
first play after a host start may go to a kit other than the selected
target, and the last sink then keeps later placements there. An
override that names no eligible node is the only unresolved result.
A launch that names `target` is the caller's executor choice,
so placement does not run for it. Sofa rooms, tenfoot, the browser
UI, and the CLI launch without `target`.

Production placement does not need the ensure seam. When no mesh
session is installed, the host projects the title itself
(`meshCatalogEntry`), so Launch can run Place and the games list can
write `placement`. Those rows omit `ready_here`: composition Ready
stays the Ready signal, and `hostclient` `LaunchBlock` still blocks
`placement_unresolved` and `placement_fail_closed`. A title the
projection does not name, including host-only and firmware-missing
rows, carries no `placement`. A test that installs an ask with
`SetMeshPlacementAsk` uses that ask as it is, and a seam-off view with
no projection ignores it.

When a launch asks for placement, `LaunchOn` calls `meshplace.Place`
with those options, the ask's candidates, and an optional override
node id. If Place selects the executor this session is already bound
to, the session records that Execute node and its local DisplaySink
and InputSource. Launch and Ensure keep that bind and do not claim
again. If Place selects a different FPGA kit, the host claims that
kit with the existing kit lease and rebinds the session to it. A
conflict rejects and does not steal. Confirm uses that same claim.
Generation takeover stays on the kit lease API. Launch and Ensure
then use the new bind. The rebind stores that kit's name, address,
TargetID, and client on the launch snapshot and notifies the
selected-target origin hook with that kit's lease, so bind and media
routing stay on that kit when `selectedTarget` later moves.
Target session invalidation and origin changes call local input and bridge
hooks after releasing the target registry lock. Remote input attach may read
the kit lease while holding its own input lock; this lock order keeps monitor
reconciliation and input attach able to finish concurrently. A fresh grant
claimed by this host remains its session even when its generation differs
from the last monitor snapshot. Media cast keeps the same lease, so
CastStart still sends the kit-lease
header, and an adopted endpoint is the cast address. The host verifies
that kit's identity before the claim. A stale configured address is
adopted from discovery when that TargetID has one endpoint, and a
different reported identity is not claimed. Bind still rejects a
replaced client or a changed address or TargetID. When Ensure needs to
dial a content executor, it uses the verified endpoint adopted by the
claimed client. A lease this launch claimed is released when execution
does not start and no other in-flight launch still holds it. A launch
that starts execution keeps the grant and the new session bind. It then
releases the grant the session still held on the kit the rebind left:
after the new bind has started execution and claim cleanup has run,
`LaunchOn` posts `POST /v1/kit/release` with that grant on the left
kit's client (`releaseLeftPlacementLease`), never an idle Stop, which
would reprogram a menu kit. The release runs synchronously before
`LaunchOn` returns, with no Service lock held and its own 5-second
deadline, independent of the caller's context. It is skipped when the
session is back on the left kit, when the left kit is the same client,
grant, or kit (node id or TargetID) as the new bind, when the session
does not hold a grant there, when that kit still has a live play, and
when another launch holds or is releasing that grant; a skipped
release leaves the grant as it was. A kit answer that the grant is
already gone (404, 409, or `KIT_LEASE_REQUIRED`) counts as released
and forgets the local grant. Any other failure is logged (target name
and node id, never the token) and does not undo the rebind or change
the launch result. The grant stays with the client, which resumes
renewing it while the lease is open, and is kept in the stopped-lease
list so a later explicit Stop retries the release. When the left kit
is unreachable, renewal fails, the client drops the grant, and the
kit's lease ends by its TTL (90 seconds in production). A launch
that does not start execution restores the previous selected target,
mesh bind, and origin hook, unless a later launch has already moved that
field, and leaves the previous kit's lease untouched. A failed launch does not release a grant another launch adopted. A release
that fails leaves the claim unsettled so it can be retried. A grant
the session already held stays held. Ensure runs on that executor only when
`[mesh] ensure` is already on. An unset key or `ensure = false`
keeps Ensure off after the rebind and still binds the claimed kit,
so the launch streams its package and media to that kit on the
existing path. Picture and the pad stay on that kit. A menu-host preview is not the
DisplaySink. A `native_emu` selection that is not the bound executor
is not applied. An unresolved or fail-closed result names no node, so
that launch keeps its bind; rooms do not offer it. A launch that is not asking for placement keeps the
Phase 0 and Phase 1 bind. The games list reads that same ask when it
builds Ready. That read does not record the decision and does not
claim a lease. When the selected kit is one this host already sees
in use, Ready is the lease-held block, so Confirm does not launch.
`meshplace` applies Decision 7 as Deano locked it, with the
first-candidate tie-breaks.
`[mesh] ensure` and `[mesh] placement` stay off unless the operator
set them.

When placement starts a play on another kit, the host admits that kit's
session coordinator before adopting the play. It rechecks the target-scoped
status after admission, then keeps the admission through old resource cleanup,
state transfer and new input/media attachment. Stop waits for an adoption in
progress; if Stop ended the placed play while launch was waiting, the host
installs no resources.

## Process ownership

The host owns the catalog, UI, user intent, content selection, and host-side
media. The target agent owns its HTTP API, transfers and session requests.
libmister-runtime owns FPGA programming, media/input delivery and recovery.

The target agent also exposes an authenticated, read-mostly diagnostic surface
on the existing kit listener: `GET /v1/kit/debug/events` returns the bounded
target event ring, and the lease-admitted
`POST /v1/kit/debug/snapshot-before-reboot` writes a diagnostic evidence
directory before an intentional reboot. The latter records the ring window and
bounded evidence for the journal, owner, `/tmp/CORENAME`, FPGA-manager state,
the native `/run/mister-runtime.events.json` dump when present, and a FAT-side
note. The native adapter drains that dump into the same ring; it does not invent
`flight_id`, `lease_gen`, or `run_id`. It reuses the current kit lease and does not create a
third process or bypass the existing runtime path. `flight_id` is the optional
canonical host UUID v4 from #205 and is retained only when a caller already has
one; `lease_gen` and `run_id` remain opaque join strings. The target never
invents host event schema or joins through the launcher listener.

These are simple process boundaries on a local, disposable development kit;
they are not a distributed ownership, failover, or recovery protocol.

### Source ownership

Appliance release manifests and immutable image storage form the independent
Go module `github.com/DeanoC/FogCast/appliance` in `appliance/`. The store lives
in `appliance/store`; both boot selection and target update handling consume
that single implementation. Format 1 is manifest format `1.0`. A manifest may
omit `manifest_format`; readers treat that as `1.0`. `manifest_format` is
`major.minor`. A release with the same major and a higher minor may include
unknown optional fields. The update path ignores those fields and logs their
names, in both mister-agent `POST /v1/update/stage` and `fes-update` when it
reads `release.json`. Ignored fields are not stored or uploaded. An unknown
major, a malformed `manifest_format`, a duplicate field, or a missing or
malformed required field is rejected. Optional `image_passes`, when present,
is a positive integer and remains in the manifest bytes that are uploaded and
stored. New releases omit `manifest_format` and `image_passes`. `release.json`
keeps the field set the already deployed agent accepts (`format`, `board`,
`boot_abi`, `version`, `kernel_sha256`, `image_sha256`, `image_size`,
`fes_revision`, `fogcast_revision`, `runtime_revision`), so the bridge update
can still stage the image. The pass count stays in release evidence. The module uses the standard library and the
existing `golang.org/x/sys/unix` dependency for filesystem operations.
The root module selects it through a checked-in relative replacement.
`make test` and `make vet` explicitly check both modules because root
`go test ./...` and `go vet ./...` do not traverse nested modules.
Target update admission remains in the root module; the fixed bootstrap and its
Linux boot implementation are owned by the FES appliance.

FogCast keeps the host applications, host services, and target agent in one Go
module, but the source tree names their ownership explicitly. The ten-foot sofa
app lives under `ui/tenfoot`; it is also the kit UI. Shared drawing, input,
theme, and library helpers live under `ui/shared`, `ui/gfx`, `ui/controller`,
`ui/inputmap`, `ui/localfeed`, `ui/menudisplay`, and `ui/theme`;
`ui/launcherconfig` reads the kit's `launcher.json`. `host` and
`internal/hostapi` own host catalog/config/library services and host-owned
remote-input bridges. `targetclient` owns the authenticated host-to-target
HTTP/cache/core/development transport and endpoint reconciliation. UI packages
may consume host, target-client, and public protocol contracts, but do not own
target handlers, runtime lifecycle, image assembly, or FPGA builds. The FES
parent selects the FogCast revision and owns image integration and release
evidence.

Tenfoot applies the paired kit-lease state to installed-core destinations as
well as FPGA titles. On a paired kit, a foreign lease displays the shared
in-use copy and refuses Confirm; unavailable lease status fails closed. Core
cartridge and firmware blocks retain priority, and the destination returns to
Ready when the lease state becomes free. A health update also clears refusal-owned
status and library launch messages when a foreign lease ends, while preserving
newer errors. Unpaired host connection state does not mark installed-core tiles in use.

The dependency direction is host/UI/`catalog`/`internal/hostapi` -> public
contracts and `targetclient`; the target executable -> target implementation
plus those same public contracts; the runtime remains the physical owner.
`protocol` is the wire schema. `corepackage` owns core-package descriptors.
`kitlease` owns kit-lease wire types. Those public contract packages do not
import FogCast `internal/` packages. `internal/agent`, `internal/httpapi`,
`internal/mister`, `internal/misterruntime`, `internal/input`,
`internal/targetcache`, `internal/applianceupdate`, and `internal/flightdiag`
remain target-owned implementation. Host, UI, `targetclient`, `catalog`, and
`internal/hostapi` must not import those packages. A later target module or
repository split is deferred until the measured target closure excludes
host/UI-only packages.

Target `GET /v1/health` may include an `artifacts` object: the SHA-256 of the
installed `/usr/share/mister-runtime/build-inputs` record, the runtime commit,
agent revision (stamped at `make build-agent`), agent and kit digests from that record, idle and catalog core digests, the
optional sealed package ID, and on an appliance boot the bootstrap ticket
`image_sha256`. The agent does not hash live binaries on each poll. Missing
fields mean there is no sealed record (for example an unsealed diagnostic image), not
that identity was rewritten. Host `GET /api/v1/health` adds a `host` identity
(`version`, `revision`, `os`, `arch`) and forwards `target.artifacts` and
`target.api_version` when the target is reachable. Authenticated target health
also reports managed software independently of that sealed base-image record.
The host projects only `target.software.active_bundle`, `base_identity`,
`selected` and `generation`; it omits the operation journal, requester labels
and diagnostic error strings. Unmanaged targets omit `target.software`, and
failed health does not retain a previous identity. Public unauthenticated target
health never exposes software status.

`GET /api/v1/kit/software/releases` reads the selected target's immutable release
inventory through the authenticated target client, without acquiring a lease or
changing selection. The response is an array of `{id, manifest}` records with
verified manifest IDs. Paired launcher requests read their authenticated kit,
not another host-selected target. Diagnostics can match `active_bundle` to a
manifest and inspect its declared interface contracts; inventory is not fetched
on the health polling path. Activation and runtime readiness remain independent
requirements. The base record's runtime commit does not identify a running
replacement bundle. Source revisions describe provenance, not runtime
compatibility. FES retains exact component pins, native input locks, hashes and
release receipts for reproducibility and exact-artifact acceptance.

Connection compatibility uses the target's existing `api_version` contract:
the host supports exactly `v1`. Missing, malformed or unsupported versions
fail closed before target mutation; different agent or runtime Git revisions
alone do not block the connection. Target identity, authentication, readiness,
lease ownership and recovery checks remain separate requirements.

API compatibility is not a promise that every operation is available. The
native adapter negotiates its versioned runtime protocol; package inspection
checks the runtime's ABI registry before activation. Media, input and
persistence retain their operation-specific interface versions, limits and
package/generation checks. Unsupported operations remain rejected rather than
being inferred from a Git revision or silently retried through another path.
Breaking API semantics require a new supported contract, not an arbitrary
source revision comparison. Configuration is not rewritten to hide failures.

## Agent runtime

The agent uses only local runtime protocol 2 through `/run/mister-runtime.sock`.
There is no backend selector, Main process probing, MGL dispatch or protocol-1
fallback. Normal FPGA launches require described FES packages; bare catalog
records and old raw-core profiles are rejected before stopping an active package.
Contained raw-RBF loads remain an explicit diagnostic with no media/input ABI.
Their generation and state are reconciled without replay; Stop restores idle.

The target HTTP request logger records successful GET/HEAD reads at debug level.
Default logs retain mutations and failed reads, including their route, status and
duration. This keeps routine health, status and lease polling from filling the
appliance's RAM-backed log filesystem; query strings, credentials and bodies
remain excluded from request logs.

A `fes.simple-computer`
package with `fes.keyboard` attaches remote input without `fes.gamepad`.
Host keyboard events map through the agent onto the runtime 40-bit ZX81
matrix (`set_keyboard`); Select+Start remains the software Stop chord. The
exact `fes.coleco` package with `fes.keyboard` 1.0 maps D-pad/left-stick
Up/Right/Down/Left to keyboard bits 0..3 and A/B to P1 Fire1 bit 4/Fire2
bit 10.
The exact `fes.sms` core with verified `fes.keyboard` 1.0 reuses this
mapping for P1 Up/Right/Down/Left and A/Fire1 (bits 0..4). SMS Fire2 is
unsupported: B remains an unmapped gamepad event and does not assert keyboard
Q/bit 10. Coleco's B mapping is unchanged.
The shared state-diff path joins keyboard, D-pad and axis holds, releases on
button-up or axis neutral, replays mapped state on transport reconnect, and
clears state on source close or Stop. Other core IDs and absent/unsupported
keyboard interfaces do not enable this mapping. This is software coverage,
not physical USB/controller or SMS hardware acceptance.
A host library entry for `fes.zx81` uses `load_library_core` like other
ROM-less FPGA cores; development `core-load` stays a separate volatile
activation and does not create that entry. The sealed shell's machine ROM
is empty. Launch requires `[zx81_machine_rom]` with `script`, `image`, and
`mistral_cv`, each a clean absolute path, and refuses the launch when that
section is absent. The host splices the 8 KiB BASIC image after package
identity is fixed, keeps that package id, and records `image_sha256` on the
launch status after firmware and media binding. A selected cart is composed
first and stays on the launch source, so the activated composition matches
the selection. The ROM splice is last.
A selected `.p` tape on the current `fes.computer` ZX81 package is role
`cassette` (`fes.media.zx81-tape` 1.0, 1..16384 bytes, unit 0) and is
inserted after Start. A setup saved earlier with role `blob` still launches
on that unit. An installed `fes.simple-computer` package keeps the mailbox
blob path. Mid-session `.p` select/load while the core stays running is a
separate product path from the ROM splice; see the FES design lock
[ZX81 tape media](../../../docs/zx81-tape-media.md).
The separate `native-dev`
image packages this composition. Its idle, visible Sonic 2 launch, one-player
input, Stop, and immediate relaunch paths are hardware-tested on the designated
kit. Native development loading is hardware-tested only for the existing
MiSTer-compatible load/Stop lifecycle and subsequent game regression; exact
two-cycle evidence is in
[native-development-rbf-baseline.md](hardware/native-development-rbf-baseline.md).
Raw development uploads still have no generic video or input guarantee.
After an explicit native session Stop has released the kit lease, a later raw
development load confirms that the target remains exactly idle and does not
submit another Stop; replacing an active native game still stops it to exact
idle before reading and uploading the development RBF.

The native agent creates one `FogCast Virtual Gamepad` during startup before
runtime reconciliation. Its Linux identity is `BUS_VIRTUAL`, vendor `0x0000`,
product `0x0001`, version `0x0001`; its capabilities are the one-player D-pad,
A/B/C/X/Y/L/R/Select/Start, signed X/Y axes, and synchronized event reports consumed by the
native runtime. Authenticated input leases only gate delivery to that retained
device: detach releases held state without destroying it, and agent shutdown
destroys it once. Package interface and generation checks gate delivery.
Exact-artifact hardware acceptance remains a separate integration gate.

Remote input retains Select wire code 107 and MD C code 108; X/Y/L/R append
codes 109/110/111/112. Linux events use BTN_X 307, BTN_Y 308, BTN_TL 310,
BTN_TR 311, and BTN_SELECT 314. Optional masks in the active runtime profile
determine which controls the core consumes. The same retained device spans
MD and SNES leases; no per-game virtual-device churn or input coordinator is
introduced. The host API exposes lease attach/detach/status; gamepad event
producers continue using the existing host RemoteInput event interface.

## Native Stop response loss

If the runtime Stop reply is lost while the operation context remains live,
`internal/misterruntime/runtime.go` makes one read-only Status request, bounded
by the existing health timeout and remaining operation lifetime. It never
replays Stop. A clean valid idle response confirms cleanup, allowing the agent
to clear active content and a later launch to proceed. Running, malformed or
retained-error idle responses do not prove successful Stop and remain unavailable.
An explicit retryable SNES save failure retains its mapped error; an explicit
reboot-required result retains the existing recovery marker. Normal Stop replies
and the existing game/development recovery policies are unchanged. Cancellation
of the operation owner also cancels observation.

This behavior has adapter, real Unix-socket dropped-response and coordinator
Stop/relaunch regression coverage. Dated diagnostic hardware validation is in
[the session recovery report](testing/session-recovery-2026-09-07.md).

## Failed native launch recovery

A failed native launch is observed once under the coordinator's exclusive
transition. A valid native idle status (including a retained launch error, but
excluding `save_failed`) permits clearing the durable active-content record.
Only successful cleanup publishes idle. The launch still returns its original
error, also retained in `last_error`; a subsequent successful launch clears it.
This observation uses the existing health timeout and operation-owner context.
It does not replay launch or issue an automatic Stop.

An unresolved native failed state remains unready, including failure to clear
the content record. Both game and development launch admission use coordinator
readiness, so callers cannot bypass that block. Explicit Stop can retry cleanup
and restore readiness. Legacy runtimes do not opt into native idle observation.

## FPGA product admission

Only installed described FES packages are FPGA products. Historical bare Pong,
SNES, NES and Mega Drive records do not grant launch eligibility. Existing
catalog rows, ROM caches and old saves are preserved; the host does not seed a
raw Pong product. Package persistence uses the described-core records below.

## Core package inspection and staging

`corepackage` is the shared, hardware-independent format-2/3/4 reader. It
inspects closed directories or restricted uncompressed ustar archives,
validates the closed typed manifest and payload bytes, and computes package
identity from the original manifest and payload. Unknown but well-formed ABIs
remain inspectable; hardware compatibility belongs to the native runtime.
`corepackage.InspectPackage` returns the package ID and closed `Descriptor`
from the same pinned read for identity-reporting consumers such as
`core-inspect`; the smaller `Inspect` wrapper returns only the descriptor.

`corepackage.Stage` accepts a caller-bounded archive stream and publishes only
validated `manifest.toml`, `core.rbf` and, for format 3 or 4,
`rom-map.json` bytes into a distinct sealed directory beneath an absolute
private root. Cancellation or validation failure removes
the incomplete directory, including cancellation observed after rename and
before ownership handoff. The caller owns the returned directory lifetime and
must release it with `Staged.Cleanup`, which reopens and verifies the retained
root and publication identities before removing the sealed directory.

Format-3 ROM staging reads one owned envelope buffer bounded by its declared
size. Package payload and map bytes borrow that immutable buffer during the
operation; receipt decoding and map validation honor the admission context.
One parsed, sealed map is reused for receipt verification and independent ROM
linking within that operation. Final private publication is revalidated, and
all framing, digest and initial-media binding checks remain required. Published
files own independent bytes after return. Restart adoption independently
validates and relinks the retained source bytes.

The target package lifecycle uses runtime protocol 2. A read-only
`inspect_package` exchange negotiates the exact ABI registry and programming
profiles before mutation. Protocol 1 is rejected without mutation; there is
no negotiation fallback.
The strict status decoder accepts the runtime's optional `menu_display`
evidence, including a failed menu, so physical idle can still be confirmed
for appliance maintenance. A `menu_display` field it does not define is
rejected. `underflows` on that object and on a frame completion
is the runtime's per-present delta. The kit painter and the Linux menu
client accept a delta of at most one 720p scanline and refuse a larger one.
The runtime and this agent must ship in the same image because decoding is
strict; capability-gated emission and tolerant decoding are not implemented.
`load_core` carries a rooted staged directory and package ID. The target retains
active and in-flight `Staged` ownership, reconciles a lost mutation reply by
observing identity plus a new generation, and retries failed cleanup only at a
safe lifecycle boundary. After staging, the adapter performs a read-only runtime
inspection and checks the exact package and descriptor before it enters the
target input replacement barrier. That barrier closes the old producer, waits
for in-flight sink writes, neutralizes the retained uinput device, and prevents
new streams or attachments until the mutation and any lost-reply observation
finish. Success or an ambiguous attempted mutation retires the old lease. A
proven pre-mutation failure reconstructs the same logical lease; failure to
pause or reconstruct it is a recovery failure and leaves input gated. Startup
adopts every still-valid publication through the same opened trusted root,
selecting only the package that exactly matches the active runtime status.
The host accepts a successful core-load reply only while it still holds the
same kit grant used for dispatch. The token match and unexpired local monotonic
deadline are checked together under the lease mutex; a retained token alone
does not authorize success. A reply received after local lease loss is a
recovery failure, even if a subsequent read observes the new active package.
The host does not replay the load or claim another grant; target expiry cleanup
still waits for the admitted operation to finish before normal Stop. A clean
idle observation confirms cleanup and retires the previous play while retaining
the launch failure. Lease renewal timing and expiry remain unchanged.
A validated explicit `reboot_required` load reply with a retained active owner
also retains the attempted package, linked ROM and initial disk sources until
a confirmed clean Stop. An identity mismatch remains ambiguous and cannot
justify deleting those sources; it does not permit adoption or a successful
activation. Failed Stop preserves the retained publications.
After an attempted activation failure, the target publishes idle only when the
runtime confirms exact operational idle and retains the structured failure in
that status. The host returns the original failure only when the observed idle
status has matching code, phase, expected, and observed evidence; any mismatch
or ambiguous transport result remains an unavailable recovery result. Confirmed
idle failure retires the old service execution, input, and media ownership; a
host-only executor must stop successfully before its ownership is retired.

The authenticated target route `POST /v1/development/core` accepts one bounded
`application/octet-stream` archive under the normal kit lease and update
exclusion. The host route `POST /api/v1/session/development-core` passes the
same mutation through the service and session coordinator. Rejected admission
leaves the prior media and input session intact. After confirmed activation,
the coordinator retires prior input and publishes the package ID, ABI, build
ID, active interfaces, generation, and derived gamepad capability in
`GET /api/v1/session`. Status reconstruction after a host restart attaches
input only for native games or active custom packages with `fes.gamepad` or
the exact `fes.keyboard` 1.0 interface; raw development RBF sessions remain
input-disabled. Manual input attachment uses the same predicate. The shared
gamepad-to-keyboard mapping is selected only for exact `fes.coleco` or
`fes.sms` together with that verified keyboard interface; SMS consumes only
the P1 directions and Fire1 subset described above.

The kit launcher opens a stream only for a nonempty input session that is ready
or reconnecting and is native or a capable custom development package. A
same-session reconnect keeps the stream identity and re-establishes the target
transport before accepting another source; capability, session, execution, or
generation changes close the old stream before another event can be sent.
`fogcast core-inspect PATH` validates locally without opening the FogCast
service. `fogcast core-load PATH` validates and streams an archive through the
running host-owned session and reports package/build identity or the public
failure phase. Its API origin precedence is `--api`, `FOGCAST_API`, then
`http://127.0.0.1:8787`. Inspection and upload derive from one bounded immutable
archive snapshot, and the command never opens or closes a target-owning
`Service`. It accepts success only when the returned active package has the
same package, ABI, and build identities, a positive generation, and a valid
descriptor-consistent active-interface set.

`fogcast launch <game-id>`, `fogcast status`, and `fogcast stop` use that same
running host session API rather than constructing a short-lived `Service`.
Launch is `POST /api/v1/session/launch` with `{"game_id":"..."}`; status is
`GET /api/v1/session`; stop is `POST /api/v1/session/stop`. Origin precedence
matches `core-load`. JSON output is the public host session object (`id`,
`game_id`, `state`, `execution`, `core_package`, `input`, and the other public
session fields). That replaces the earlier CLI launch `{status,content}`
cached-launch envelope. A failed request does not open a new service or repeat
the mutation. `fogcast health` and catalog commands (`scan`, `games`, `search`,
and the other local library commands) still open the injected host service;
agent health is not session readiness. The host service activates installed
packages through the target described-package library endpoints.

### Development media upload

`fogcast [--api ORIGIN] core-media PATH` snapshots one regular local file of
1..16384 bytes. It reads `GET /api/v1/session` from the running host and sends
the exact bytes to `POST /api/v1/session/development-media`. Like `core-load`,
API origin precedence is `--api`, `FOGCAST_API`, then `http://127.0.0.1:8787`.
The CLI never opens a target-owning service. File extensions do not select
behavior; the bytes are opaque to FogCast. No library association or durable
media record is created.

Both the host POST and target `POST /v1/development/media` require a fixed
`Content-Length` in that range, `Content-Type: application/octet-stream`,
`X-FogCast-Package-ID` (the current 64-character lowercase package digest), and
`X-FogCast-Core-Generation` (the current positive decimal generation). The host
also requires `X-FogCast-Session-ID`, copied from the current host session's
`id`, and `X-FogCast-Target`, copied from its `target` name. When the session
reports `target_id`, send that exact value as `X-FogCast-Target-ID` too.
The service checks both target name and target ID under lifecycle admission
and the target lock: switching targets cannot reuse a colliding package and
target-local generation. Generation values remain uint64 throughout, including in the CLI.
Chunked, empty, oversized, truncated and excess bodies are rejected. A host
restart, package replacement or generation change requires a fresh session read.

The service reuses lifecycle admission and the session's bound target. It
checks artifact compatibility and active package identity before upload. The
target requires the existing kit lease, rejects the hostless owner, and uses
the existing update exclusion and lifecycle serialization. This operation
never claims a new lease. Admission requires an active, error-free described
`fes.simple-computer` 1.0 package with active `fes.media.blob` 1.0; raw RBFs,
inactive cores, recovery states and stale identities are refused before
dispatch. Rejected admission preserves existing input and session ownership.

`internal/misterruntime/development_media.go` rechecks runtime identity before
staging and immediately before mutation. It creates a unique private directory
(0700), writes a regular `media.bin` (0600), and sends exactly
`{"protocol":2,"operation":"load_media","path":"<absolute staged file>"}`
over the existing local socket. The file is removed when the operation returns;
cleanup failure is reported. No network caller supplies a target filesystem
path. The target keeps its lifecycle admission and operation owner for the
bounded runtime call (15 seconds to accommodate the runtime's 10-second
deadline), even if the uploading connection disappears after admission.

Success preserves package and generation, input and host session identity.
The native adapter serializes keyboard posts with media transfer. Posts wait
while media is in flight, preventing runtime `BUSY` from closing the keyboard
stream. After successful media delivery on a keyboard-capable core, it restores
the last held-key matrix cleared by the hold/reset sequence before admitting
queued input events. That cache is bound to the package and generation in the
last successful keyboard response. A different generation or package restores
neutral, including after failed replacement neutralization. A transfer failure does not attempt keyboard restoration
through a potentially poisoned transport.
There is no automatic retry or replay: an unchanged package status cannot
prove media delivery after a lost reply. The coordinator observes runtime
state after a transfer failure. A failed transport can leave reset held and
report `reboot_required`; the leased recovery path programs idle again
(`recover_idle`) and reboots the board when that LoadIdle fails or the
runtime rejects `recover_idle` as an unknown operation. If recovery fails
closed without arming a reboot, Stop returns that error instead of waiting
for a new boot id.
Stop cannot bypass a poisoned GP transport by merely quiescing it. Physical
hold/reset, byte transfer and recovery remain runtime responsibilities.

For a separately started diagnostic host on port 8797, the operator sequence
is `fogcast --api http://127.0.0.1:8797 core-load PACKAGE.fcore`, then
`fogcast --api http://127.0.0.1:8797 core-media MEDIA.rom`. Starting that host,
deploying its matching agent/runtime, and hardware validation are separate
integration operations. FES selects the runtime revision and generates the concrete assembly lock;
live compatibility is determined by protocol and operation contracts.

Focused tests cover admission, existing leases, update exclusion, lifecycle
serialization, staging permissions and exact bytes, generation changes,
cross-target package/generation collisions, keyboard events during transfer,
recovery status, Unix request shape and no replay. The end-to-end test runs
CLI → real host API/service → real target client/lease/HTTP/controller → native
adapter and Unix socket, with only the hardware daemon simulated. This is
host-only evidence, not Coleco hardware acceptance.

### Session live media (ZX81 change-tape)

Mid-session tape arming does not hold reset. On an active `fes.computer`
generation that reports `fes.media.zx81-tape`, `change-tape` inserts the `.p`
into unit 0 and `eject-tape` ejects that unit. An active `fes.simple-computer`
generation with `fes.media.blob` still uses `replace_live_media` /
`clear_media`. Live-media capability is that active tape interface or the
legacy blob interface. The host session projection keeps those active
interfaces and media units, so the tape picker and `change-tape` form a
binding for either generation. On a menu-display kit, both that tape unit
and a legacy blob session open the media selector only
when the running package reports `fes.video.session-display` 1.0 and
`fes.memory.hps-ddr` 1.0. Home posts `visible: true`, the runtime makes menu
presentation available, and `BindSession` authorizes the launcher frames.
On a kit, Home or Select/View opens **Change media** directly over the running
machine, with transparent pixels outside the opaque picker. The launch room
is not drawn behind it. The picker offers insert and eject for the observed
unit-0 format: ZX81 `.p`, Spectrum `.tap`, Apple II `.dsk`/`.do`, C64 `.d64`
or Atari ST `.st`. Spectrum `.z80`, `.sna` and `.szx` appear only when the
active package reports `fes.media.spectrum-snapshot` 1.0, and TR-DOS `.trd` and
`.scl` and +3 `.dsk` only while a unit-1 `fes.media.spectrum-disk` is active
(without it, a `.dsk` is an Apple II disk); the host refuses a disk written
for the other drive. The runtime
reports the matching optional snapshot metadata as active; model-specific
snapshot checks remain in the host converter. It applies the active
interface and reported size limits before import; the host and runtime
validate the object again. ZX81 also
shows the existing starter-tape shelf only when its game entry identifies
`fes.zx81`; saved programs remain scoped to that game. Files come from the launcher machine
(home, mounted media and accessible library roots); saved FogCast host drives are browsed through drive-relative storage APIs. Import, insert and eject retain the session captured when
the selector opened. Session-display admission checks the captured generation,
observed DDR and display interfaces, and a supported observed unit 0 for disk
or Spectrum media. The host, target agent and runtime adapter share that
admission predicate; the native runtime already admits the observed display
interface independently of media type. Insert and eject use a bounded
450-second transport without the normal polling timeout and are never replayed
after a lost response. Starter import uses the ordinary bounded HTTP client:
it copies at most 16 KiB of embedded host bytes into the host media store,
without fetching the attribution URL or contacting the FPGA. Only the later
insert needs the physical-operation budget.
Successful insert/eject and Back at the picker roots return directly to play.
A failed display return retains the binding and Back retries that return.
Back posts `visible: false`, which quiesces the plane and restores the
machine's HDMI pixels. Library play is
native, and `change-tape` accepts that execution. Both paths are distinct from
launch-time `POST /api/v1/session/development-media` (`load_media`). See the FES lock
[ZX81 tape media](../../../docs/zx81-tape-media.md).

### Coleco title selector and cartridge swap

Factory Coleco format-4 packages link the 8 KiB BIOS and 16 KiB cartridge into
the RBF at launch and do not expose a live media unit. Home and F12 during
Coleco play open the `example.coleco-arcade` title selector over the running
session. On a kit display plane, the room is drawn and accepts pointer input
while it has focus. A selected title says that the running cartridge is fixed
and that Stop is required before choosing another; Confirm cannot launch over the active
session. This catalog route remains the Home/F12 action and Coleco does not fall
through to the ZX81-only `example.hardware` workbench. With the Coleco room
closed, Guide opens **Change media** directly on the kit display plane as a
cartridge picker. Back at the picker roots returns to play in one step without
opening the Coleco room underneath it. The cartridge is the single format-3
`rom_link` whose role is `cartridge`, or the second format-4 `rom_links` source
whose role is `cartridge` after the linked BIOS. A swap replaces only that
cartridge source. Select stays
the machine's keypad `*` while the machine owns input; when a picker has focus,
Select confirms its focused row and is not forwarded to the machine. The picker
admits `.col`, `.rom` or `.bin` files of exactly the running cartridge's size,
with no eject or live insert. Choosing one imports it and posts
`POST /api/v1/session/cartridge {media_id, name}` with the captured session and
package-generation binding. The host relaunches the same title on the same kit
with that cartridge for this one launch. The title's own cartridge binding is
unchanged, so its next ordinary launch uses its own cartridge again. The reply
is the new generation; it retires the old plane and its focus, the picker closes,
and play continues on the new load. The host separately revalidates the saved
package, firmware and cartridge snapshot, then checks the load against the
explicit replacement ID; the temporary choice does not weaken those checks.

The Coleco Arcade room also remains the catalog path. The target publishes the
activated descriptor's `core.id` in cached `/v1/status`; host polling keeps that
identity with the matching package generation and can restore it from the saved
launch record when an older target omits it. Tenfoot therefore recognizes
`fes.coleco` even when the runtime's supported-interface projection omits its
optional bus 2.0 interface. The room resolves installed Coleco titles from the
`fpga` catalog platform, constrained by `core_id=fes.coleco`; package-library
entries are not indexed on the hardware platform named `coleco`. While idle,
Tab/Search opens that same filtered platform shelf, including after Stop, so
installed Coleco packages remain visible in the ordinary library browser. The
native-pixel shell also requires its matching Direct or Scanlines video part.
The ColecoVision tile on Home opens `example.coleco-arcade`, which launches
through the library and composes the selected video part. Other local cores
that need a part still report incomplete display setup there.

Operator/diagnostics CLI:

```text
fogcast --api http://127.0.0.1:8787 --json change-tape MEDIA_ID_OR_.p_PATH
fogcast --api http://127.0.0.1:8787 --json eject-tape
```

`change-tape` arms a household core-media object into the active session. A
`.p` / `.P` path is imported through `POST /api/v1/core-media` first; a 64-hex
media id uses the already stored object. Admission requires 1..16384 bytes and
a `.p` / `.P` name. The host call is `POST /api/v1/session/live-media` with JSON
`{"media_id","name"}` and the same session/package/generation/target binding
headers as development media. The displayed filename is scoped to that launch
flight as well as its target, package and generation, so a relaunch that reuses
a package generation starts without the previous flight's name. `eject-tape` posts
`POST /api/v1/session/live-media/clear` so the next empty `LOAD ""` is `0/0`.
The CLI reads the session with a 30-second client. `change-tape` and
`eject-tape` post through the mutation client, which does not impose that
timeout, so an insert can finish inside the host's 150-second budget. A lost
response is not replayed.
The target agent uses `POST /v1/development/live-media` and
`POST /v1/development/clear-media`. A confirmed loader rejection during eject
maps to retryable `BUSY` with `no_change:true`. Numeric GP rejections during
insert are treated as uncertain because `MediaBegin` may have succeeded and
cleared readiness before a later data or commit rejection. An ambiguous or
unstable GP handshake after eject dispatch maps to retryable `BUSY` without
`no_change`, because the core may already have ejected the medium. A hard MMIO
failure or an invalid clear acknowledgement stays `MISTER_UNAVAILABLE`, even
when the runtime phase is `input`. The host retries phase-`input` busy and
does not relabel that hard failure as loader contention. Unavailable eject
does not replace the active session. The host does not inject BASIC `LOAD ""`
keys; the user types that on the ZX81 keyboard after the tape is armed. Sofa
tenfoot Load-tape chrome (Y / north while the session is active and live-media
capable) opens a local `.p` picker, imports via `POST /api/v1/core-media` when
needed, then arms with `POST /api/v1/session/live-media`. The same overlay can
eject through `POST /api/v1/session/live-media/clear`. It retries loader `BUSY`
and leaves the session active when eject reports unavailable.

Every browsed folder keeps its parent action and offers Eject and Type a path,
including folders without matching media. Directories remain visible; files
are listed only when their extension and size match the active unit. Local
mount roots are added only when they exist on the launcher device. FogCast host
drives are browsed through the host storage API, so host-only paths are never
treated as kit paths.

### Removable disks (fes.computer media units)

A `fes.computer` package projects media role `disk`
(`protocol.DeclaredCoreMediaCapabilities`) on transport
`fes-computer-media-unit-v1`, unit 0. `fes.media.apple2-floppy` 1.0 is format
`apple2-dos-order`, exactly 143,360 bytes, names `.dsk`/`.do`.
`fes.media.c64-disk` 1.0 is format `c64-d64`, exactly 174,848 bytes, name
`.d64`. `fes.media.atari-st-floppy` 1.0 is format `atari-st-floppy`, exactly
737,280 bytes, name `.st`; the required geometry extension admits the bounded
layouts described under [raw floppy geometry](#atari-st-raw-floppy-geometry). `fes.media.spectrum-tape` 2.0 is role `cassette`, format
`spectrum-tape`, names `.tzx`, `.tap`, `.pzx` and `.csw` up to 32 MiB, on the
same unit 0. The library keeps the image as imported; at insertion
`internal/spectrumtapes` converts it to the unit's 10..16,777,216-byte PZX
stream, which plays as libspectrum 1.5 (Fuse 1.6.0) plays the original. A
package that declares `fes.media.spectrum-snapshot` 1.0 (with `-128` and
`-plus3` 1.0 for 128K and +2A/+3 snapshots) also lists `.z80`, `.sna` and
`.szx`; a live insert's tape or snapshot name says which it is. Library media
and inserts by digest have no name. TZX, PZX and CSW signatures keep those
images on the tape path before snapshot probing; TAP has no signature, so the
host takes a valid snapshot as one unless every TAP block also passes the ROM
loader's checksum. Any `ZXST` signature routes through snapshot conversion
even when invalid so its snapshot error is preserved. The host converts a
snapshot by `internal/spectrumsnapshots` to the core's restore stream and
refuses a snapshot of a machine the package does not run. A shell
declares one unit-0 medium. A Spectrum shell that also declares
`fes.media.spectrum-disk` 1.0 and the optional `fes.media.spectrum-disk-trdos`
1.0 (the Pentagon's Beta 128) has role `disk`, format `spectrum-disk-trdos`,
names `.trd` and `.scl` up to 1 MiB, on unit 1 beside the cassette; with
`fes.media.spectrum-disk-plus3` 1.0 (the +3's uPD765A) instead, format
`spectrum-disk-plus3` names `.dsk` up to 1 MiB. `internal/spectrumdisks`
converts the disk at insertion to the unit's raw track image, built as Fuse
1.6.0's `disk.c` builds the disk, and refuses one for the other drive. A
launch sets the image's boot flag, so the core resets and boots it (into
TR-DOS, or through the +3 menu's Loader), and gives a TR-DOS disk without a
`boot` program Fuse's autoload loader; a live insert does neither. A live
`.dsk` goes to the Spectrum disk when the active package has one. Library selection
(`PUT …/core-entries/{game_id}/media` with `media_role:"disk"`, catalog schema
13) validates the size and source offline. Writable ST initial disks are
restored and inserted before CPU release; other home computers program the
package and then insert their selected medium into unit 0. A failed insertion
is a failed launch and follows existing library-slot Stop/recovery. The same session accepts later swaps:
`POST /api/v1/session/live-media` with a `.dsk`/`.do`, `.d64` or `.st` name inserts a
household disk, and `…/live-media/clear` ejects the unit-0 medium the active
generation declares (Apple II floppy, C64 disk, Atari ST floppy, Spectrum tape, or ZX81 tape). `.p` names
insert `fes.media.zx81-tape` when that unit is active, and otherwise keep the
legacy ZX81 blob path. A `.tzx`, `.tap`, `.pzx`, `.csw`, `.z80`, `.sna` or
`.szx` name, or no name, inserts the Spectrum cassette on unit 0, and a `.trd`
or `.scl` name the TR-DOS disk on unit 1. Clear does not eject the TR-DOS
disk, and the CLI's disk commands do not handle it; a live insert replaces it.
Library disk inserts retain the confirmed session filename on failures before
target dispatch. After dispatch, the target's `no_change` result determines
whether the host can retain that filename.
The CLI equivalents are `fogcast change-disk MEDIA_ID_OR_DISK_PATH` and
`fogcast eject-disk` for a disk, and `fogcast change-cassette` and
`fogcast eject-cassette` for a Spectrum tape. A stored
media ID is named `disk.dsk`, `disk.d64` or `disk.st` from the active unit,
so the host checks the declared disk size or discrete ST geometry against that generation. A path is imported
through `POST /api/v1/core-media` and keeps its basename.

`fogcast/media_units.go` binds each request to the package, generation, target
and unit (`protocol.MediaUnitBinding`) and admits only the observed unit's
limits from `core_package.media_units`. `targetclient/media_units.go` streams
the exact bytes to authenticated target `POST /v1/development/insert-media`
(fixed `Content-Length`, `application/octet-stream`) or
`POST /v1/development/eject-media`, with the package/generation/target headers
plus `X-FogCast-Media-Unit`, under the existing kit lease and update
exclusion. `internal/agent/media_units.go` rechecks the binding, and
`internal/misterruntime/computer.go` stages the bytes in a private 0700
directory (0600 file), rechecks identity immediately before the single
`insert_media` call (bounded to 135 s), requires the unit to report `ready`
(`empty` after `eject_media`), removes the staging, and never replays a lost
reply. The runtime never holds reset for these transfers and ejects the unit
once on failure; the agent then republishes the live unit state from a fresh
status read instead of replacing the session. Keyboard posts do not wait
behind a disk transfer.

A shell pairing `fes.media.atari-st-floppy-write` 1.0 with the Atari ST base
interface supports writable disk RAM and versioned snapshots. Library launch
uses explicit game/base-image identity in the initial ROM envelope or target
`POST /v1/library/media/insert`; development `insert-media` and `change-disk`
remain volatile. Imported catalog blobs remain immutable. Durable records live
under the runtime's `/media/fat/fogcast/core-data/media` root, within the existing
appliance data bind. This storage survives firmware/video package changes and
is isolated by core ID, game ID, unit and immutable base digest.

The target library insert route has the existing package/generation/target/unit
headers plus `X-FogCast-Media-Game` and `X-FogCast-Media-Base`; it requires one
exact admitted raw-ST octet stream, with non-720 KiB geometry requiring the
active extension and a matching immutable-source BPB. `POST /v1/library/media/save` has the unit
binding and an empty body. Both retain the existing kit lease and update
exclusion. The agent chooses the storage root; callers never send target paths.
Live status exposes optional unit `persistence` with `mode:"persistent"`,
`game_id`, `base_media_id`, and `revision` (`absent` before the first publication
or the full-record SHA-256). Whole-package persistence mode is persistent only
while such a binding exists. ST parts/ROM composition does not change this
identity or weaken composition validation.

The host routes `POST /api/v1/session/disk/insert` and `…/disk/save` retain the
captured session ID and package/generation/target/unit binding. Insert takes
JSON `{game_id,base_media_id}` and verifies the selected library entry and base;
Save has an empty body. CLI `insert-library-disk GAME_ID BASE_MEDIA_ID` restores
that explicit library disk into the running ST, and `save-disk` publishes a
checkpoint. `eject-disk`, Stop and replacement automatically save a bound disk.
Failure keeps the disk/session binding available for recovery; a lost mutation
reply is never replayed. One explicit Save may retry a retained `SAVE_FAILED`
in phase `save` only for the same active package/generation/target, with no
recovery state and the same ready, writable persistent ST game/base binding.
A confirmed successful checkpoint publishes a valid revision digest and clears
that retained error. Other errors and insert/eject/input retain their strict admission guards;
there is no automatic Save replay. There is no durable binding inferred from
filenames or raw development loads. Close files before a checkpoint/Stop: sector-atomic
writes do not make an application's multi-sector FAT update atomic.

Save-backed Eject/Stop own 135-second target operation budgets and 150-second
HTTP envelopes. Explicit Stop selects its HTTP save deadline from completed
session responses without a preflight poll; discovery Stop reuses its admission
Status observation. Replacing an already bound disk allows 405 seconds locally and
450 seconds on the host, including a possible third capture that confirms an
ambiguous failure retained the original image. Ordinary unbound operation
budgets remain in force. These calls never replay a destructive command.
A confirmed input Busy rejection retains only the latest absolute mouse button
state for zero-motion reconciliation, bounded to 450 seconds and the same
generation. Unbind, source closure or an ambiguous transport failure cancels
it; relative motion is discarded.
A bound-disk input fault captures and publishes before idle retirement; if that
fails, the runtime retains RAM and ownership in recovery instead of replacing
it with idle firmware.

## Other modes

Host-emulator execution, remote input, capture, and host-to-target media are
existing optional modes. They share the host session UI but do not replace or
precede the direct FPGA launch path. Local session preview on Linux uses the
configured absolute V4L2 device path and the installed FFmpeg command to
produce H.264 frames for the existing MJPEG preview endpoint. macOS keeps its
native AVFoundation capture adapter; other platforms report capture as
unavailable.

A kit-only host runs `fogcast-api` with `--launcher-config` and does not open
the SDL sofa window. Pass `--headless` so that process does not compose local
capture or the MJPEG preview pipeline; kit catalog, attract, session, and
input stay on the launcher listener. A kit without a sofa host boots the same
FogCast service on the existing config and catalog paths.
`fogcast.BootLocalCatalog` opens that service, scans the configured library
roots, and does not dial a remote target. An empty library says `No games in
this library yet.` A content root that cannot be found, with no saved rows,
says `The game files for this library can't be found.` Saved rows whose files
are offline say `Offline, showing your saved list`. An unfiltered
`GET /api/v1/games` includes that sentence as `notice`. Folder-watch still polls
configured library roots every thirty seconds, but it only re-opens a source when size or
mtime changed. Unchanged rows bump `seen_generation` and do not rebuild the
search index. A long scan waits a full interval before the next poll, so the
watcher cannot run back-to-back.

The `core_ids` catalog filter resolves installed entries through their
`core_entries` identities. Recents and continue apply it to play-history
candidates and grouped editions before selecting a representative; recents
also applies it before the page limit.

The kit reconnects with the existing `launcher.json` API URL on that launcher
listener. See [the host connection contract](launcher-host.md).

The launcher listener serves one or more paired kits. `launcher-host.json`
holds either the single-kit `token`/`target_id` pair or a `pairings` list of
them (the single pair, when present, counts as the first pairing). Each
request's bearer is matched in constant time against every pairing token,
and `X-FogCast-Target-ID` must be that token's kit. A bearer identifies
exactly one kit: startup refuses a configuration in which one token is
paired with more than one `target_id` (the error names the fix, mint a
per-kit bearer), so multi-kit use requires a separate bearer per kit. A
launcher bearer must also differ from the host-to-agent token of every
target in `config.toml`, enabled or not, not only the selected one. The
single-kit form (one top-level token for one target) is unchanged.

Each enabled paired kit has its own foreground session, input binding, media
handle, and scoped `POST /api/v1/session/stop`. Kit-menu launches replace only
that kit's play; its launcher input stream claims that kit's per-target input
bridge. Scoped Stop does not move the host foreground, and unscoped browser
Stop continues to use the active target, then the selected target. Paired
session and status reads are target-scoped.
On the paired listener, health, `rom_cached`, and `/api/v1/library/cache`
describe the authenticated requesting kit. Paired health probes that kit
without foreground lifecycle admission, so another kit's launch does not
stall its menu poll. The host listener keeps selected-target health and cache
semantics. Cache snapshots are isolated per target.

The launcher allowlist also admits `GET /api/v1/games/{id}` for strict game
IDs so uncached room selections can load their detail row. Setup-room Add game
admits `POST /api/v1/library/core-entries` for one installed package and title.
The listener forwards that body unchanged and pins the session target to the
requesting kit so compatibility uses that kit. Other methods on that collection
stay denied. For named ROM selection it admits
`GET /api/v1/library/core-entries/{game_id}`,
`GET /api/v1/library/core-entries/{game_id}/rom`, and
`PUT /api/v1/library/core-entries/{game_id}/rom` for a strict game ID. It also
admits `PUT /api/v1/library/core-entries/{game_id}/expansion` for the existing
single-entry expansion selection. Selection bodies are forwarded unchanged
after paired-target checks; none of these writes replaces the foreground
target. Slot writes, presentation edits, other library PUTs and settings stay denied.
It does not expose full `/api/v1/library/settings`, which contains host
filesystem roots. Tenfoot uses `GET /api/v1/launcher/kit-lease` for an authenticated,
target-ID-scoped status projection. It returns only that enabled kit's lease
state, owner, purpose, generation, expiry, reachability, and readiness.
Paired-kit lease and launch guards use this projection; selected-target host
health remains available for host-mode clients. If the projection is
unavailable, the paired kit's lease is unknown and input fails closed. The
endpoint exposes no host paths. On the paired listener the settings overlay
does not request host settings. Its host rows stay inert, local preferences
still apply, and the footer says host settings are managed on the FogCast
computer.

## Native 10-foot launcher

The embedded **Sinclair workbench** (`example.hardware`) reads host-owned hardware
setups through `GET /api/v1/library/hardware`. Its title, missing-setup notices
and setup action follow the selected ZX81, ZX80, TS1000 or TS1500 model.
The setup action lists installed packages for that model. A failed package
inspection is skipped with a notice while
usable sibling packages remain selectable; inventory and connection failures
still fail the refresh. Optional rear-socket bus 1.0 and
2.0 are supported; expansion readiness uses the same exact-package/version
admission as library selection and launch. The projection validates each
expansion against its exact installed shell, and includes ordinary session
status separately. `expansion_selection` on that response says whether this
listener accepts the single-entry next-start PUT. Fit/remove use the existing
compare-and-swap expansion selection API, including from a paired kit when
the flag is true. An older paired host omits the flag and the kit stays
inspect-only. Catalogue schema 14 adds optional household expansion labels
and descriptions; these never influence admission. The session receipt's
composition identifies running hardware independently of the saved setup.
Missing or mismatched composition receipts remain unknown. Bound Stop checks
the active package under the existing service lifecycle admission before
coordinator input/media cleanup, and holds that admission through physical
Stop. A rejected changed-session action triggers a refresh; a subsequent
explicit Stop uses the refreshed identity. Returning to a reused room calls
its resume hook immediately, retaining navigation focus while refreshing the
host snapshot. Failed saves and lost Stop responses retain their existing
retry behavior.
See [hardware rooms](../../../docs/hardware-rooms.md) for the complete scope.

When an entry has no selected expansion and its package lacks session display,
the hardware projection may offer another installed ZX81 package only when it
targets the same board, supports the rear socket and live media, and declares
session display plus HPS DDR. Start compares the entry's old package before
saving that package selection and then uses the ordinary launch path. Package,
firmware, media and persistence checks remain on the host; an exact-package
expansion keeps the existing package selected.

Paired package selection scopes compatibility and persistence checks to the kit
making the request, even while another kit remains foreground. If the selection
reply is ambiguous, Tenfoot reads the saved entry: the requested package
confirms the selection, the old package keeps retry available, and a different
package reports a conflict without overwriting it.

The embedded **Systems** room (`example.computers`) lists prepared playable
library systems and their grouped editions. Confirm uses `session.launch`.
**Set up** opens the selected entry's workbench with `room.initial_game_id`
and `room.initial_core_id`; a loaded empty library offers **Add system**. Setup
navigation stays unavailable while the shelf or its editions are loading. ZX81-family
entries use `example.hardware`; Commodore 64 and other families use
`example.system` with the selected core and game IDs. The shared
`widgets.system_workbench` implementation covers every registered playable
system and model, including the Spectrum models, Apple II, Atari ST, consoles,
ROM-less games and utilities.
Resume refreshes retain the selected game ID when title order changes and keep
the prior row position, clamped to the new list, if that game disappears. A
failed refresh clears both the shelf rows and list widget so Set up cannot open
a stale entry.

The shared workbench chooses a system family, a hardware variant, a saved entry
and a grouped title edition. `systems.families` and `systems.family_by_core`
keep the ZX81 and Spectrum models in their respective families while retaining
the actual core ID of each variant. **Next system** changes families;
**Variant** cycles that family's models and is absent for single-model families.
The family remembers its last browsed variant in navigation state. A variant
change selects that core's own entries and package requirements without copying
ROMs, media or expansion selections from another model or replacing live play.
It resolves an explicitly selected entry directly if it falls outside the
filtered library query. **Set up system** reads installed package versions and
sealed ROM requirements without needing publication; the same overlay also
links to published installation/setup. **Media for next start** reads package
capabilities and saves the selected role with package/media compare-and-swap.
Saved media labels name the selected role; package guidance still describes the
model's full media support. Adding a setup confirms only the library entry;
expansion fitting is a separate operation.
**Expansion sockets** reads the package's declared physical topology and saved
slot choices. Exact-package/socket archive import and fitting are separate;
selection checks the previous card. Library metadata shows selected cards
without inventing topology from the system label. Resume rereads both library
entries and hardware while retaining the chosen entry/edition. Each library
refresh ignores replies from older requests; a failed refresh clears the
catalog and keeps its error visible while the user changes variants or systems.
The hardware room drops an in-flight snapshot when a newer read is queued, so
the older result cannot replace the requested machine before the follow-up read.
Closing the room saves the current navigation selection. Unsupported core IDs
are shown as unsupported instead of being treated as ZX81.
All three workbench room IDs reserve the native 52 px status/hint footer
below their Lua content, keeping that chrome clear of the action rows.

Live media, Stop and Return to play use the captured active session. A failed
hardware refresh disables live controls until a later read succeeds. The kit
opens a supported live picker directly; a host window selects the active
system's workbench from the captured launch identity or a read of its exact
active package, independently of the visible library shelf. A late package read
can open controls only for the same session, target and package generation.
Active package interfaces determine which live media and
display controls are available. These host/UI paths do not rebuild or accept
core artifacts on hardware.

Rooms access this projection through optional typed `rooms.HardwareServices`
and `hostclient`, without arbitrary Lua networking. A room visited while
playing changes presentation only: it releases held computer keys, keeps the
session/lease, and opens the existing tape picker. That overlay is drawn and
hit-tested on both room and parked now-playing screens. Stop, failures and
return still use the ordinary session lifecycle. Per-room state preserves
focus and selection, not hardware configuration or running RAM.

Kit Home reuses its presented display list while core-list identity, status,
focus, shelf position, title and content size are unchanged. An asynchronous
list result, pointer or direction focus change, resize, or unpresented frame
forces a redraw. Its completed redraw uses `gfx.paint` for the background;
a soft overrun can therefore present the finished picture for reuse on the
next frame. State-save debounce continues in `update` during reused frames.

Lua room callbacks run on the launcher thread under per-callback soft budgets.
A slow update, or a slow draw that did not finish a `gfx.paint`, keeps the
previous frame and counts toward eight consecutive frame overruns. A finished
`gfx.paint` or `gfx.extend` that misses only the soft budget is presented, so
the next unchanged draw can call `gfx.reuse` and keep that frame. `gfx.extend`
appends to the presented frame, after any drawing already recorded in that
draw. A clear recorded before the extend replaces the presented clear. The
ZX81 workshop's first picture is spread over several draws: a short opening
frame, cached text metrics, then the upper and lower halves. A change to
focus, size, status or selection after the upper half paints that half again
before the lower half is appended. Eight consecutive slow paints still retire
the room. Slow input, pointer, lifecycle, and asynchronous result callbacks
count in their own group, so successful frames do not hide repeated input
overruns. Eight overruns in either group retire the
room. Each callback still has a 4x hard deadline for runaway scripts. PCall
paths restore their Lua stack after errors and return-valued callbacks also
restore it after success or soft overrun.

`cmd/fogcast-tenfoot` is a host-side 10-foot launcher with SDL3 and explicit Linux framebuffer shells (cover grid, shelf,
and list). It is another client of the public host API, not a second launch
path:

```text
Native SDL3 UI
  -> GET /api/v1/platforms
  -> GET /api/v1/library/collections
  -> GET /api/v1/library/facets for genre and year lists
  -> GET /api/v1/games (grouped=1, availability=ready, optional collection/platform/core_ids/sort/q/genre/year/region/hide_prerelease/hide_hacks)
  -> PUT or DELETE /api/v1/library/favorites/{id} for the focused title
  -> PUT or DELETE /api/v1/library/collections/{id}/{gameId} for custom-shelf membership
  -> PUT /api/v1/library/collections/{id}?name=... and DELETE /api/v1/library/collections/{id} for custom shelves
  -> GET /api/v1/library/edition-preferences and PUT /api/v1/library/edition-preferences for household room edition choice
  -> GET /api/v1/presentation/artwork/{handle} from catalog cover handles
    and focused-title screenshot handles
  -> GET /api/v1/presentation/games/{id} for the focused title (studio,
    players, summary, screenshot_ids, video_id, year, genre, attribution)
  -> GET /api/v1/library/attract (idle video then stills; artwork via the same presentation artwork GET)
  -> GET /api/v1/library/settings and PATCH /api/v1/library/settings (idle seconds, preferred regions, selected target, library roots)
  -> POST /api/v1/session/launch (optional client_ts_utc / client_mono_ms JSON or X-FogCast-Client-* headers)
  -> POST /api/v1/session/development-rbf (raw octet-stream from a local path OSK)
  -> GET /api/v1/session (poll; now-playing or DIAGNOSTIC development chrome; additive flight_id)
  -> GET /api/v1/session/events?after= (poll; sofa event list; additive flight_id plus host/client clocks)
  -> POST /api/v1/debug/ui-events and GET /api/v1/debug/ui-events?after= (sofa/tenfoot focus/nav/launch/stop stamps, plus launcher-action rejection and home-room fallback; not a kit mutation)
  -> GET /api/v1/session/preview (optional MJPEG; 404/503/inactive is unavailable)
  -> POST /api/v1/session/stop (empty body releases the kit lease; optional client stamp JSON; retain_lease true keeps it; release_idle drops idle grants without stopping a surviving play; X-FogCast-Client-* headers)
  -> GET /api/v1/health (poll; kit chrome)
  -> GET /api/v1/status (503 TARGET_UNAVAILABLE treated as kit-down)
  -> GET /v1/kit/lease on the selected target address (status-only lease strip)
  -> POST /api/v1/session/input/attach and /detach (empty body; FPGA-native now-playing)
  -> POST /api/v1/session/input/event (play-session HID; fail-closed on a foreign kit lease)
  -> host session service
  -> existing FPGA launch path
```

`GET /api/v1/session/events` keeps the existing protocol 1 event object.
The host also sets an optional `flight_id` UUID on those events: one new id
per session launch, development-RBF or described-package load, and per
orphaned stop. Related events in that flight (launch through active through
stop of that session) repeat the same id. User stop of an active session
reuses the launch id. A failed launch leaves the previous id in place. The
field is omitted until a flight has been allocated. Every event also carries
host `ts_utc` and `mono_ms`. When tenfoot or the sofa browser stamps a
launch/stop, the matching event repeats `client_ts_utc` and `client_mono_ms`.
Invalid client clocks are ignored and do not fail the mutation. The current
`flight_id` is also additive on `GET /api/v1/session` and on launch/stop
responses. Focus and nav stamps, plus a copy of launch/stop actions, go to
`POST /api/v1/debug/ui-events` (`layer=ui`, kinds `ui.launch` / `ui.stop` /
`ui.focus` / `ui.nav` / `ui.launcher_action` / `ui.home_room_fallback`); fog-flight joins those rows to host and target events
by `flight_id` when it is present. Token-like detail keys are dropped.
`ui.home_room_fallback` carries `room` and `reason` for a `home_room` that
could not be opened. `ui.launcher_action` records an id the allowlist
rejected. Neither is a kit mutation.

TV overscan insets, sofa layout (`grid`, `shelf`, or `list`), the local
attract on/off gate, reduced motion, the look name, the optional debug HUD, and Home
(`home` start screen, optional `home_room` id, plus `pinned_rooms`) are local to the tenfoot process (CLI `-safe-area` /
`-layout` / `-no-attract` / `-theme` / `-debug-hud` / `-home` / `-home-room` and optional `tenfoot.json` prefs). When `home` is `rooms` and `home_room` names a pack that can be opened, start and Home open that room as the root; otherwise the Home overlay lists pinned rooms, recently played games, installed rooms, and the library. A library or recents read that fails in the network stack shows the temporary unreachable copy and retries with the presentation backoff, from 400 ms up to 8 s, until the host answers or the load is replaced. A host API error is not retried. An empty catalog stays the empty-library copy. The retry does not launch a title. On `menu-display`, a rooms home with no room selected by flag, saved preferences or `FOGCAST_HOME_ROOM` defaults to kit Home (`example.fes-cores`, titled Home). That room uses `destination_panel = "hero"`: tenfoot draws the selected core's title, required inputs, readiness, "Plays on this TV" and its actions in a large FES-owned panel (`ui/tenfoot/room_hero.go`, geometry `rooms.HeroPanel`), the room draws the shelf, artwork motifs and the Library / Systems / Settings routes, and the footer shows full-size prompts and only actionable status. Details keeps the kit's verbatim block when the column has to drop lines to fit. On a content box under 480×360, `room.hero` is nil and the room draws a compact list of the visible cores and those same routes above the destination strip, each with a pointer hit. When those routes do not fit on one row they stack, and Direction walks Library, Systems, Settings and then the cores; Up from the first core returns to Settings. A compact row that fits, and the hero, keep Left and Right along the routes and the shelf. Confirm while the core list is still Checking is acknowledged and not queued. Changing Home from Library to Rooms in Settings applies that same default for the rest of the session. The implicit room stays out of `tenfoot.json`. An already selected room stays selected. Library home is unchanged. Load/update/draw and asynchronous room callback errors identify their callback; deadline-expiry errors also report elapsed wall time and, on Linux, the script run's wall time with the callback thread's own CPU time and its voluntary/involuntary context switches over that run, so a kit report separates slow script work from waiting for a CPU (`ui/rooms/thread_usage.go`). Elapsed covers the whole callback, including pinning and sampling its OS thread and the cleanup after the script; the callback deadline starts with it and the soft and hard budget checks compare it. A room retired after consecutive soft overruns lists each overrun's elapsed time with that script-run usage, the process-wide Go GC cycles completed during that run (context beside those times, not pause time inside the callbacks), and the most images the launcher had already been asked to fetch, so a kit report can correlate GC or decode contention with the script's own work (`ui/rooms/overrun_streak.go`). Scheduler pauses count against the unchanged callback deadline. Installed-core listing retries transient startup socket/time-out and catalog-warming failures for up to 60 seconds; leaving the room cancels those reads. Back opens the Home overlay, retaining library navigation. Opening the Library from a room, the Home overlay's Library row or Recents suspends that room and its nested parents instead of closing them (`ui/tenfoot/library_return.go`); Back from the Library resumes the same instance with its selection, and Home reclaims a suspended home room and resumes it; either way the Library's Details, filters and view overlays close first. A Library refusal is cleared on that return, so the resumed room does not show it as a launch failure. Opening another room discards the suspended one. Library Details shows the focused title's readiness pill, block headline and the launcher's own reason (`classifyLibraryGame` with the snapshot's `KitPlay`: `libraryPlayDestination` plus the paired kit's unknown status or foreign lease, checked in the same order as Confirm; in a room, a chosen edition is classified itself). The Library header's readiness word, list rows and cover cells use the same classifier. A Ready title on a paired client that is not host-only also says "Plays on this TV", because a paired launch targets that kit, the creator beside the title, and offers A only for what Confirm will do (play, or import the Coleco BIOS). A blocked Confirm opens Details for the title that was refused, including a Home recent that is not the focused Library row. Favorite, pointer confirm, and presentation for that Details follow the same title. When a paired kit cannot report its status, the headline is that unknown status. Cancelling the BIOS picker returns to that Details. A Coleco BIOS import also refreshes a suspended room's cached titles, so Back shows the updated readiness. Library cells that cannot play carry a labelled tag from that classifier, keeping a catalog reason such as Needs BIOS, and missing art names the platform. Other backends keep their existing defaults. A room may publish an allowlisted `settings` action; Confirm opens Settings when the launcher can, and otherwise shows a short status. There is no host
safe-area or layout API. Host attract idle, preferred regions, selected target, library roots, and
targets use the existing public library settings endpoints. Tenfoot can add,
edit, and remove targets from the sofa settings overlay. Agent secrets are
write-only: GET exposes `agent_configured` only, the sofa never echoes a
stored agent, and PATCH sends `agent` only when the operator edited or
cleared it.

The Home overlay's Library row clears any room-specific core filter and
reloads the full title list for the selected platform. Cycling platforms also
clears that filter before reloading the library.

Attract prefers a playlist `video` handle when present. Darwin CGO builds
decode with AVFoundation (`ui/tenfoot/attractvideo`) after streaming
`Accept: video/*` to a temp file (128 MiB cap). Linux uses the same download
when `ffmpeg` is on PATH and decodes with the ffmpeg CLI; otherwise it skips
the download and falls back to stills. Non-CGO Darwin builds also skip video.
Short clips play through, then the
playlist advances or a single-item playlist restarts from the local file
without re-fetching; clips longer than 60s are capped at 60s. Missing, failed,
or unsupported video uses backdrop, else cover, else marquee. The gfx device re-uploads
the stage texture only when `FrameSeq` changes. Hide, dismiss, park, and
process stop tear down the decoder and close any player still queued.

While a host session is `active`, tenfoot may open `GET /api/v1/session/preview`
and CPU-decode JPEG parts from `multipart/x-mixed-replace; boundary=fogcast-frame`.
The sofa labels that surface **Preview**; it is not a living-room HDMI mirror.
404 (route absent), 503 `"session preview is inactive"`, kit/decoder down, and
transport errors are graceful misses and never block Launch or Stop. Park,
unpark, Stop, app close, attract entry, and GPU-using overlays cancel the HTTP
stream, close the reader, and drop the preview texture so no background
goroutine holds the stream.

Source entry points are `ui/tenfoot/` and `cmd/fogcast-tenfoot`. UI draw
helpers use `ui/gfx.Device` (begin/clear/present, RGBA8 textures,
textured quads, fill rects, CGO-free `DrawText` / `DrawTextWeight` with
embedded Go Regular and Go Bold, `DrawMask` for text coverage, and
`DebugText` for the 8×8 HUD / FC2D opcode). Text is laid out as coverage
masks, one byte a pixel (`gfx.LayoutText`): each glyph is rasterized once per
face and copied into a mask per string, and a mask draws in any colour with
alpha `A·m/255`. Room text, labels and `DrawText` share that path. Window, events, gamepad, mouse, and text input remain
SDL in `ui/tenfoot/sdl.go`. USB keyboard is first-class browse/nav on that
path (`CommandFromKey` in `ui/tenfoot/keyboard.go`): arrows, Enter, Esc, and
Tab drive shelf, detail, and search without a gamepad. USB mouse/pointer is
first-class on the same path (`PointerMove` / `PointerClick` in
`ui/tenfoot/pointer.go`): hover moves focus, primary click activates
select/launch/confirm on shelf, detail, and search, and it coexists with
keyboard nav. Hints and focus ownership follow the last-used keyboard, mouse,
or gamepad (`ui/tenfoot/affinity.go`). SDL keyboard, mouse, and gamepad
add/remove events claim affinity on plug without restarting the process, and
unplug restores a remaining device. Letter shortcuts already
patterned stay (`/` or `f` search, `o` settings, `g` filters, `l` layout). While
an attached play session is live (`ForwardsPlayHID`), USB keyboard events go to
`POST /api/v1/session/input/event` instead of the sofa focus graph and do not
steal browse or ZX81/session affinity; pointer browse stays off that session.
Esc and Backspace remain session-stop chrome. Letter `s` stays a core key.
A `fes.computer` session with `fes.keyboard.hid` 1.0 (`CoreKeyboardHID`)
is the exception: `HandlePlayHIDScancode` forwards every SDL scancode that is a
HID Keyboard/Keypad usage, including Esc, Backspace and `/`, with no chrome
keys; Stop stays on the session chrome and the controller Select+Start chord.
A `fes.keyboard` core maps those keys onto the ZX81 matrix. For exact
`fes.coleco`, D-pad/left-stick and A/B events are mapped onto the Coleco P1
keyboard bits while overlapping keyboard, D-pad, and axis holds remain joined.
Exact `fes.sms` and `fes.sg1000` reuse that path for directions and A/Fire1
only; B does not provide Fire2.
Native SNES/MD
encode USB keys as gamepad buttons (codes 100–112) so the target mux does not
route them to `set_keyboard` and reconnect replay does not treat matrix codes
as axes. Foreign kit leases and `recovery-required` connections fail closed
and drop HID. On the kit,
USB keyboards join the play-session input stream with gamepads; `fes.keyboard`
packages are eligible without `fes.gamepad`. `TENFOOT_GFX` / `Options.GFX` / `-gfx` may select
`software`, `fpga`, or `fpga-stub` for tests; the production sofa path stays SDL3.
Explicit `-gfx linuxfb` dispatches to `ui/tenfoot/linuxfb_linux.go` before
SDL initialization, including in CGO-free builds. Explicit `-gfx menu-display`
dispatches to `ui/tenfoot/menudisplay_linux.go` the same way: it reuses that
app, evdev input, and smoke loop, forces 1280×720, and submits through the
runtime menu socket with change-driven presents. The direct CPU loop records
complete scenes through `gfx.FrameCache`: identical draw commands and unchanged
textures skip rasterization. Labels are keyed by slot, width and font
size; unchanged source strings bypass fitting, and source changes reuse the
mask when the fitted visible text is identical. A clipped lease countdown
therefore cannot invalidate a scene whose visible header has not changed.
Texture mutations preserve draw order and invalidate
reuse. Settings marks a backdrop checkpoint after dimming and painting its
fixed panel border, background and title. CPU devices retain one RGBA
framebuffer copy (3.52 MiB at 1280×720). When only the panel changes, the cache
restores the bounding rectangle touched by the previous overlay commands and
draws the rows, status and later overlays. Fill and texture bounds use the
rasterizer's clipping; text and other operations without certain bounds fall
back to restoring the full framebuffer. Prefix command changes, framebuffer
geometry changes and updates/destruction of textures used by the prefix rebuild
the checkpoint. Changing panel-only textures preserves it. A frame without a
checkpoint invalidates reuse, so closing and reopening settings cannot retain
stale pixels. Identical whole frames still do no pixel copies. Other devices
replay their normal commands; the Device and runtime wire contracts are
unchanged.
The grid CLI and tenfoot CPU kit backends (`menu-display` / `linuxfb`) default
to a 96 MiB soft Go memory limit, honoring an explicit `GOMEMLIMIT`. This
collects and scavenges temporary decode heaps sooner; it is not a hard RSS cap.
Desktop SDL retains the normal runtime policy. Wheel summaries are rebuilt on
catalog replacement and volatile-field refresh; idle prefetch reads those
representatives without catalog scans, and frame construction follows the paint
gate. Bounded wheel refreshes compare the complete frame content and reuse
the rendered revision when it is identical; motion bypasses reuse and forces
a final settled paint. Input polling, App updates, scripted rooms and snapshots continue at 30 Hz.
Rendered-frame revisions skip framebuffer comparison in `MenuDisplay`; ordinary
callers retain byte-identical change detection. An unchanged submission that has
not failed or been dropped is skipped. Once a second after a successful present, that skip
still reads menu status. A new menu generation is submitted in full; the
same generation presents nothing.
The first frame is submitted because nothing has been queued yet. Status
errors, scanout underflow, and present failures wait 250ms, doubling up to
5s. An unavailable menu uses that schedule but never waits longer than the
1s probe, so a Stop redraws within about a second. A generation mismatch
does not start a new wait. The next present after a wait is submitted. The
menu-display backend takes no kit lease and only talks to the runtime menu
socket; the tenfoot
app keeps its existing host session client and existing status reads (for
example the kit-lease status read). The SDL, linuxfb and
menu-display shells all use `configuredApp` and the shared App, rendering,
catalog and session API paths.
The software rasterizer classifies opacity at texture upload/update. Opaque
unscaled sprites copy clipped rows; scaled sprites precompute exact float32 sample
columns and cache repeated source/destination resizes within an 8 MiB/64-entry
budget. Cached bitmaps cover only visible raster bounds, preserving fractional
position and sampling. First-use and continuously updated textures use the
general sampler to avoid creating a resize bitmap every video frame. Texture update/destroy invalidates those entries. Large constant-color
alpha fills use an ARMv7 NEON kernel when the OS advertises NEON and at least
eight pixels fit in a row. The kernel blends eight RGBA pixels at a time with
exact integer division by 255; scalar tails preserve clipping and destination
alpha. Other CPUs, narrow rows and small fills retain scalar arithmetic or
channel lookup tables. Opaque fills repeat one painted row using Go's copy;
pixel copies retain Go's implementation. The path remains CGO-free. Text blits borrow their bitmap for
the synchronous draw. Menu submission buffers return to a bounded pool only when
neither a queued/in-flight frame nor the remembered submission references them;
status probes share immutable bytes. The runtime frame protocol is unchanged.
`fogcast-tenfoot -cpu-profile PATH -heap-profile PATH` writes optional local
profiles. On exit, CPU sampling stops before heap GC and serialization.
Benchmark recipes and measurement limits are in
[native CPU rendering](native-tenfoot-launcher/CPU.md).

The framebuffer shell reads native-width evdev records for US keyboard text
and gamepad buttons plus range-normalized stick/hat axes, merges held commands across devices,
and applies each pad's saved profile and the shared hold gate. `nativeInputs.poll` takes those
axes from `ui/controller.Mapper`. A held D-pad hat owns each shared
left axis: stick reports on that axis are dropped until the hat returns to
centre, which restores the stick's latest position. Automatic discovery filters keyboard
capabilities and physical gamepad identity, excluding virtual-bus devices and
the FogCast virtual gamepad, and seeds startup affinity. Zero supported devices is not
fatal: the UI paints, and automatic mode rescans evdev about once a second
with the same classifier so a hotplugged keyboard or gamepad is opened
without a restart. Unplugging a device drops it without turning held buttons
into synthetic click actions; it releases that device's held input and leaves the UI running. Explicit input paths remain
strict. It restores the mapped display bytes on normal exit. Pointer input is
outside this development-testing slice. The framebuffer smoke
uses no physical input and only verifies library loading and rendering; it
is separate from the broader SDL smoke.

| Backend | Construction | Role |
| --- | --- | --- |
| SDL3 | `gfx.WrapSDLRenderer` (`ui/gfx/sdl3.go`, build tag `sdl3`) | Default production path: wraps the process `SDL_Renderer` with letterbox logical presentation and VSync. |
| Software | `gfx.NewSoftware` (`ui/gfx/software.go`) | CGO-free RGBA8 rasterizer with portable Go and an optional ARM NEON alpha-fill kernel (no SDL). Nearest blit, `Snapshot` for golden pixels. Cover/screenshot/still downscale is Catmull–Rom at decode. |
| FPGA | `gfx.NewFPGA` (`ui/gfx/fpga_device.go`) | Records the versioned FC2D command stream (`ui/gfx/fpga_protocol.md`) and rasters through Software. `BackendName` is `fpga`. `IsStub` is true until a programmed 2D core exists; this slice has no mailbox/RBF and is not HDMI FPGA UI. |
| FPGA stub | `gfx.NewFPGAStub` (`ui/gfx/fpga.go`) | Thin Software wrapper without a command stream, kept as `fpga-stub`. `IsStub` is true. Does not talk to kit, runtime, or RBF. |
| linuxfb | `gfx.OpenLinuxFB` / `gfx.NewLinuxFB` (`ui/gfx/linuxfb.go`) | Software rasterizer whose `Present` blits RGBA8 to a 32bpp Linux framebuffer (`/dev/fb0`) with destination stride and BGRX byte order. Used by `fogcast-tenfoot -gfx linuxfb`. Look tokens live in `ui/theme` and are consumed by the sofa `Clear` sites. Chrome uses typography roles `title_px` / `body_px` / `caption_px` / `status_px` through `Theme.TitlePx` and siblings; when a role is unset, `header_scale` / `label_scale` / `status_scale` still map to pixel size `8*scale`. Title and chrome header use Go Bold when `title_bold` / `header_bold` are set (built-ins default true); body, caption, and status stay Regular. `DebugText` stays the FPGA/debug path. |
| menu-display | `gfx.NewMenuDisplay` (`ui/gfx/menu_display.go`) | Software rasterizer that submits full 1280×720 frames on the runtime menu socket. `fogcast-tenfoot` settings dims only the backdrop outside its opaque panel, avoiding hidden alpha overdraw. `fogcast-tenfoot -gfx menu-display` uses `FrameCache`, rendered revisions and `SetChangeDriven(true)`: an unchanged rendered revision that has not failed or been dropped is skipped. Callers without revisions compare framebuffer bytes. Once a second after a successful present, that skip still reads menu status. A new menu generation is submitted in full; the same generation presents nothing. The first frame is submitted because nothing has been queued yet. Status errors, scanout underflow, and present failures wait 250ms, doubling up to 5s. An unavailable menu uses that schedule but never waits longer than the 1s probe, so a Stop redraws within about a second. A generation mismatch does not start a new wait. The next present after a wait is submitted. The menu-display backend takes no kit lease and only talks to the runtime menu socket; the tenfoot app keeps its existing host session client and existing status reads (for example the kit-lease status read). The base-image supervisor starts `fogcast-tenfoot` on this socket. On this backend tenfoot submits one indeterminate loading frame with phase and elapsed time and waits until that frame has been presented. A failed or unpresented loading frame aborts dispatch. A presented frame pauses presents, then the launch is dispatched so the core owns HDMI. Late adoption of a core that already owns HDMI submits no loading frame: outstanding menu work is drained and presents pause, and an older menu failure does not block that pause. On the HPS framebuffer, loading overlays continue animating and show elapsed time. Launches return to the menu after the local 60 second load bound plus five seconds grace, without adopting a later same-package session after a lost launch reply. Select+Start, F12 or Guide during local loading defers stop until the exact launch reply supplies its claim generation. A capable library computer opens a generation-bound media selector on the session plane while execution continues; pixels outside the selector remain transparent. |

`gfx.Recorder` remains a call-order test double and does not draw pixels.
`gfx.Replay` / `ReplayBytes` apply a decoded FC2D stream to any Device.

The kit display loop records an always-on frame trace (`ui/tenfoot/frame_trace.go`): per tick the poll, `Tick` with that iteration's room Lua async/update/draw time (zero when the room did not step), `Snapshot` and render; per input the command; per menu frame the worker's copy, queue, status and present phases plus the runtime's write-back, submit and scanout wait from `menu_frame.timing`. Tick time starts when the loop begins the iteration. Each `SIGUSR1` writes a new JSON Lines file. `scripts/frame-trace-summary.py` reports where displayed frames spend their time and counts failed presents separately. See [frame trace](frame-trace.md).

`gfx.MenuDisplay` rasters RGBA8888 in
software and queues only the
newest complete frame. Its
local `ui/menudisplay` client reads the runtime's menu generation and fixed
1280×720 geometry, maps the generation's grant of both slots once, and per
frame writes into the slot the runtime names: this frame's damage and the
previous frame's when that slot holds the frame before, otherwise the whole
frame, converted to the scanout's BGRX order with the pixel's alpha as X,
which a session plane blends with. It commits the rectangles it
wrote and waits for displayed-sequence completion through protocol 2. The UI
receives no DDR address or FPGA register access; stale generations and
unavailable menu firmware reject frames at the runtime, and a fault from a
grant revoked mid-frame fails that frame. Submission runs off the pad loop.
When the idle menu's grant includes a minor 1 or later menu blitter,
`MenuDisplay` records each frame as blitter commands instead of rastering it
(`ui/gfx/menu_blit.go`): fills, copies, blends and text-mask expands that draw
the pixels `Software` would, with scaled draws and debug glyphs made into
cached bitmaps. The runtime does not issue a blitter grant below minor 1; an
unexpected unsupported grant is rejected before any frame is submitted.
The client uploads each bitmap or mask a frame reads into the grant's texture
heap once, keeps it there while it is reachable and the heap has room, writes
the list and commits it; the fabric draws the slot (`ui/menudisplay/blit.go`).
A first frame drawn before the grant is known goes as one copy. A blitter
grant accepts CPU frames only before its first list. A session plane or a menu
without the selected blitter gets CPU pixels, rasterizing a list that reaches
it; a list too large for the blitter is rasterized and copied whole.
`fogcast-tenfoot -gfx menu-display` is the client of that socket and
opts into change-driven presents. An unchanged frame reads menu status once
a second and is submitted again when the generation changes. Status errors,
underflow, and present failures back off from 250ms to 5s. An unavailable
menu backs off on that schedule but no longer than one second, so a Stop
redraws within about a second. A generation mismatch does not start a new
wait. A change-driven worker with a damage-capable client also skips
the per-frame status read for up to a second after a successful present, and
hands the client only the row bands that differ from the frame the runtime is
showing (at most 16 rectangles). If a shortcut no longer holds, for example
after a new generation, the frame is retried once with fresh status and
complete pixels; Pause, Resume, rebinding and any failure drop both
shortcuts. On this backend tenfoot opens
the kit-local control socket and submits one room launch overlay frame with
phase and elapsed time. Dispatch waits until that frame has been presented,
then pauses presents and dispatches the local launch. A failed or unpresented
frame aborts dispatch. A status probe acknowledges only the submission
sequence that was presented, so a healthy status for an older frame does not
accept a newer frame that never reached the display. Late adoption submits no
overlay: it drains outstanding menu work and pauses, and an older failure
does not block that pause. On the HPS framebuffer the overlay keeps animating and shows
elapsed time. A loading Select+Start chord stops after the runtime reports
`running`.
The configured FogCast service still supplies library/setup operations and
the host session path still launches games. Cached offline titles remain
browse-only. This option does not install or select menu firmware in an image.

The built-in room set contains Home, Systems, System setup and Sinclair setup.
The last two are internal templates opened with an exact model/entry, excluded
from room and default-home pickers. Home routes directly to Systems. The old
Lobby, Amiga Workbench, Mario, SNES, TMS and Coleco demo rooms and redundant
C64 wrapper are removed; the installed-room framework and public widgets stay.
Systems rereads its playable shelf on resume after nested setup and ignores
callbacks from earlier refreshes. Sinclair setup queues a refresh requested
during a read and performs it after the current snapshot arrives.
Missing saved-home IDs use the existing fallback without replacement aliases.

`internal/corenames` supplies plain display names for 18 exact first-party
core IDs. Unknown systems retain their supplied name; this table does not admit
packages or media. Installed-core protocol metadata stays verbatim. The room
adapters project machine names and recognize only known model aliases followed
by reserved HIL issue/timestamp or generated semantic-version/8-hex suffixes.
A private UI matcher recognizes the historical generated-label shape; package
admission and version validation stay unchanged.
Genuine game/custom titles, raw catalog rows/candidates, stored titles and all
launch/ROM/media/expansion/session identities remain intact. Classify and saved
edition projections retain that same visible name. Native Library paints only
visible row titles through the helper; Snapshot keeps its shared raw catalog,
without a full-catalog naming pass. Setup default titles use the exact core ID's
plain name, while installed-version rows retain actual version/hash detail.

Tenfoot's shared Library and Details chrome uses the warm FES paper, ink and
forest-green action roles, proportional Go faces, opaque title plates and a
focus outline separate from readiness. The native Library header reserves
116 px for identity, metadata and readable hints. Its footer reserves space for
the selected title and visible actions. Default/Night Library backgrounds use
warm paper; Arcade and custom resolved backgrounds keep their look. Authored
rooms retain their own palettes, with larger label plates and footer-safe
content. Workshop secondary and disabled actions use neutral surfaces; Start
(or Set up for an empty system) is the dominant action. Running and saved
next-start setup remain distinct.

`ui/tenfoot/browse_actions.go` owns launcher-local action focus. Direct Confirm
on a Ready item still launches. Down from Library's bottom visible row enters
Play/Details; Left/Right selects an action, Up/Back returns to the same item.
On Home, Up from the shelf enters hero actions and another Up reaches the
header; Down/Back returns to the shelf. `Snapshot.BrowseActions` feeds paint and
contextual hints. The captured destination, readiness intent, room/client and
session generation must still match before activation. The builtin Home room
accepts private hover IDs to synchronize its shelf outline, without changing
the public Lua API or destination schema. Opening Details preserves the browse
selection and position.

Tenfoot looks are data-driven. `ui/theme` loads colour, spacing,
typography roles, cover-chrome, vignette, bezel, and cabinet tokens from a built-in name (`default`,
`arcade`, `night`) or a JSON/TOML file. Roles are explicit pixel sizes
(`title_px`, `body_px`, `caption_px`, `status_px`). Paint calls `TitlePx`,
`BodyPx`, `CaptionPx`, and `StatusPx` so fallback math stays in the theme
package: an unset role uses `gfx.ScalePx` of `header_scale` / `label_scale` /
`status_scale` (the former 8× DebugText hierarchy). Built-in `default` and
`night` use 20/13/12/14 on a 640×480 grid; `arcade` uses 22/13/12/15.
`default` still preserves the sofa/attract clear colours. Built-in themes
mark `title_bold` (and chrome `header_bold`) true so grid headers and detail
titles raster with embedded Go Bold; body/caption/status stay Regular unless
the matching `*_bold` token is set. Incomplete files inherit those defaults.
`fogcast-tenfoot` resolves its look with `theme.Resolve` (`-theme`, then
`tenfoot.json` `theme`, then `FOGCAST_THEME`). There is no scripted theme VM
or font-family picker; derived colours stay in Go.

The browser shell remains the default UI. Mac is the primary sofa target; Linux builds
with the same `make build-fogcast-tenfoot` target (`CGO_ENABLED=1` and
pkg-config `sdl3`). Build and run notes are in
[native-tenfoot-launcher/README.md](native-tenfoot-launcher/README.md).

Tenfoot can load a development RBF from a gamepad path OSK (type or paste a
local file path; no browser file picker and no host file-list API). That POST
is the same public `application/octet-stream` session endpoint. Sofa chrome
labels the result DIAGNOSTIC: HDMI and input may be down, and it is not a
playable game session. Stop uses the ordinary session Stop-to-idle path.
Household Coleco BIOS import is a separate overlay: rooms/library Confirm on
a firmware-required title opens a local file picker and posts
`POST /api/v1/core-media` plus `PUT /api/v1/library/firmware`, including while
the selected kit is busy. That is not a development RBF load and does not
claim the kit. Mid-session ZX81 Load-tape is another overlay: while an
active session can take a `.p` tape (`fes.media.zx81-tape` on `fes.computer`,
or `fes.media.blob` on `fes.simple-computer`), Y opens a `.p` picker that
arms or ejects through the session live-media API without relaunch or
hold-reset `load_media`.

## FES appliance releases

FES owns compatible source selection, release/media assembly, and the fixed
bootstrap. FogCast supplies the public appliance module, target update API, and
`cmd/fes-update`. Before uploading, `fes-update` reads the image's
`/etc/fes/software-base.json` with `internal/ext4image` and refuses an image
whose menu interfaces the kit's selected software bundle does not accept. A
bundle built for another base contract cannot go first, so it only reports
that the image will hold that bundle back. Online releases replace
only a content-addressed read-only ext4 system image. The locked kernel, U-Boot,
and fixed `/linux/linux.img` bootstrap stay outside that operation. Configurations,
target identity, ROM cache, launcher catalog/cover cache, and SNES saves remain
on FAT outside every rootfs. Replacing the system image does not wipe
`/media/fat/fogcast/cache` or `/media/fat/fogcast/launcher-cache`.

When leftover card capacity has already been formatted as partition 3 with
label `FESDATA3`, agent startup bind-mounts that ext4 volume over
`/media/fat/fogcast/{cache,saves,core-data,launcher-cache,evidence}`. Existing
files are copied onto p3 first and never overwritten there. Releases,
`agent.toml`, `launcher.json`, `target-id`, and known-good images stay on the
1 GiB FAT. The helper does not create, grow, or format partitions and does not
run from the fixed bootstrap. Bind is refused while `GET /v1/update` shows trial,
pending, or corrupt; the agent still starts and keeps those trees on FAT.

Kit UI state stays on that FAT tree at `/media/fat/fogcast/state`. The software
supervisor creates the directory when `/media/fat/fogcast` is already present
and sets `XDG_CONFIG_HOME` and `XDG_STATE_HOME` to it for the runtime, agent
and launcher. Tenfoot writes `FogCast/tenfoot.json` and
`FogCast/rooms/<room-id>.state.json` there. Home's last selected package is
`package_id` in `FogCast/rooms/example.fes-cores.state.json`, restored when that package is still installed. A fresh visit, or a saved id that is missing or unavailable, focuses the first installed core. Selecting an omitted core does not replace the saved id. The directory is
not a FESDATA3 bind mount. Tenfoot keeps the user config directory
(`XDG_CONFIG_HOME`, otherwise `$HOME/.config`) when that directory is
writable, when a missing path can be created because its nearest existing
ancestor is writable, or when it already contains readable settings, room
state, or a room pack (`FogCast/rooms/<id>/room.toml`). A read-only
directory with those files is still loaded, so the default room search path
stays beside them. The FAT directory is used when the user directory cannot
be read and cannot be created, including `HOME=/` on the read-only root and
a read-only directory that has no saved state and no room pack. A save that
cannot be written is logged once and the menu keeps running.

The kernel loop-mounts the bootstrap as before. Its PID 1 verifies the selected
image, consumes a pending trial durably, attaches another read-only loop, and uses
`pivot_root` followed by exec of the selected `/sbin/init`. The old bootstrap
remains at `/.fes-bootstrap`. Preparation failures select verified known-good or
factory; failures after starting root switching require reboot. No trial is
repeated on a later boot without another explicit activation.

For a trial, an independent bootstrap process first verifies the ARM DE10-nano
device tree and prepares Cyclone V warm reset: it marks the completed preloader
valid and disables the retained-OCRAM boot enabled by the locked U-Boot. Ordered
32-bit SYSMGR writes and matching readback are required before opening the
watchdog, so a reset reloads the same valid SD preloader instead of retained RAM
or the next preloader copy. This does not confirm the appliance trial. The process
then opens the DesignWare hardware watchdog and acknowledges arming before
candidate init executes. Its separate
mount namespace retains the bootstrap and FAT views through pivot. Its deadline
is 180 seconds. Only a durably synced confirmation matching the actual boot ID
and selected image permits magic-close. The agent records that confirmation
only after the image's splash is on FAT, so a failed splash publish leaves the
watchdog armed. Failure, deadline, or process death
leaves reset armed. Known-good boots do not require the host to be online.

`appliance/store` owns bounded raw-image admission, immutable publication,
cross-process locking, checksummed state, consumed-trial selection, and
removal of unreferenced images after an upload has been verified. When the
release volume is short, the upload is verified on the mounted FESDATA3
volume, or a directory bind-mounted from it, before any image is deleted.
tmpfs and ramfs are not used. Factory, known-good, previous, pending, trial,
the `boot.json` image, and loop-backed images in the release store stay. A
volume that is still too small, or that has nowhere outside itself to verify
the upload, fails the stage with HTTP 507 `UPDATE_NO_SPACE`
(`insufficient release storage`) and deletes nothing.
`internal/appliancedata` owns the optional FESDATA3 bind at agent startup.
The FES bootstrap owns fallback ordering, Linux mounts, loops, and the
watchdog. The bootstrap records its selected image in
`/media/fat/fogcast/releases/boot.json`; the native agent accepts that ticket only
for the current kernel boot ID and retained factory manifest.

`internal/applianceupdate` uses the existing kit lease and coordinator transition.
Activation drains launch/development/input/cast requests, stops the runtime so
battery saves persist, neutralizes peripherals, records pending selection, flushes
the HTTP response and requests reboot. During an unconfirmed trial these normal
operations are blocked; status, Stop, lease management and confirmation remain
available. A failed response/reboot leaves pending visible and releases admission
on the current known-good image. Confirmation requires actual native idle.
`internal/appliancesplash` checks the running image's idle core against
`idle_sha256` in its build-inputs and stages it under the release store.
Release admission keeps a 1 MiB margin plus room for that splash: an idle core
up to 8 MiB, its sidecar, and a same-directory temporary of the core, so an
accepted release can be confirmed. Confirmation first promotes the trial to
known-good in the release store without telling the watchdog, then atomically
replaces FAT `/idle.rbf`, and only then records the confirmation the watchdog
reads and clears the `boot.json` trial flag. A failed or cancelled publish
leaves the watchdog armed. A boot-ticket rewrite failure after the release
record is durable does not undo confirmation: admission opens, the error is
logged, and the rewrite is retried once on a later idle status read. A failed
splash publish can be retried by the same boot. A later
boot of that image, including one that is no longer a trial, republishes from
the running image's verified idle core even when the staged copy was removed.
An unconfirmed trial does not modify FAT, so watchdog fallback still programs
the previous splash. Factory fallback records the selected image and does not
itself write FAT; the agent publishes that image's splash when the non-trial
boot starts. U-Boot programs whatever is on FAT at the following boot.
Confirming a rollback writes the restored image's splash. Prune removes the
staged splash with the image. Status reports `fat_idle_sha256` and
`idle_sha256`. A non-trial splash repair failure is logged and returned in
`splash_warning` without hiding that status. A trial stays unconfirmed until
the publish succeeds.

The host uploads once and activates once. It resolves response loss by reading
authenticated boot/image identity, rediscovers a changed address using the existing
target ID, waits for startup lease cleanup, obtains a new lease and confirms only
the expected trial. The operator client does not add a UI or take over another
owner. The [operator guide](appliance-updates.md) describes routes and commands.

Host tests and a real isolated ext4-to-ext4 root-switch test cover this composition.
They do not establish physical watchdog reset or exact-image kit acceptance.

## Target image

Native Buildroot and rootfs assembly live in the FES `image/` recipe.
FogCast supplies the agent, kit, package selector (`cmd/target-image-lock`,
`internal/targetimage`) while FES owns external-artifact policy in
`image/build/native-inputs.toml`. FES adds its selected runtime revision to a
disposable assembly copy;
FogCast has no expected-runtime SHA constant. The explicit hardware diagnostic accepts
that concrete copy through `NATIVE_RUNTIME_INPUT_LOCK`.
FES produces:

- `image/build/output/target-image/native-dev/linux.img`: the reproducible native
  runtime candidate image.

The only image variant is `native-dev`. Its base-image software supervisor
starts the selected immutable runtime, agent and kit launcher group. It installs the locked splash/idle artifact
and the closed FES package set selected by
[the default profile](../../../profiles/native-integration-dev.toml): `fes.menu`,
`fes.pong`, `fes.zx81`, `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.spectrum`,
`fes.ramtest`, `fes.atari-st`. ST retains its sealed firmware ROM map; firmware
is separately selected at launch. The selector also admits `fes.c64`, which has a frozen HIP
seal but stays out of the factory image until it has image acceptance. The menu package is idle firmware,
not a playable library entry. Image inclusion and selector admission do not
establish playability; [core status](../../../docs/core-status.md) records the
exact-artifact kit evidence.
The image selector validates and copies the closed `manifest.toml` and
`core.rbf` set, including `rom-map.json` for formats 3 and 4, beneath exact
package IDs. It separately retains factory video assets beneath
`/usr/share/mister-runtime/core-video-parts`. Its canonical `index.json` binds
each marked shell to one direct and one scanlines archive by profile, exact part
ID, relative path, size and digest. The selected `FES_VIDEO_PARTS_DIR` tree has
0555 directories and 0444 files. The selector rejects unlisted members,
symlinks, mutable objects and missing coverage of a selected video-marked shell,
then independently composes every indexed part against the exact shell before
copying its original producer archive bytes. Canonical core package members
and identities stay unchanged. Older unmarked package-only selections need no
video tree. The external `fes-core-video-parts.json` is byte-identical to the
installed index; both reproducible image passes retain and compare that record.
The installed selection copy and canonical build-input record bind the index
digest, each selected shell/profile/part/archive identity and its install path.
Image verification reconstructs the same projection from the closed installed
tree and the external selected index.
The selector
retains the external producer/package selections beside the image,
has no Main startup or legacy Menu-configuration helper, has no
`/dev/MiSTer_cmd` wait, and retains the same read-only root with volatile
`/run`, `/tmp`, and `/var/log`. Its build-input record identifies the runtime
commit, agent binary and idle RBF provenance. For each format-2, format-3, or
format-4 package selected, the record identifies the exact selection digest,
package and payload IDs, producer/schema revisions, and install path.
The verifier reconstructs that projection from the installed package and
external selection; it does not infer selection from cache or image contents.
The per-filesystem Buildroot copy retains the installed 0555 package directory
and 0444 member modes through image creation, then an external rootfs hook makes
only the copied directories removable when the fakeroot command exits. Before
reusing a retained Buildroot output, the image builder applies that same bounded
directory-only cleanup to its exact `target` copy. Image verification preserves
the sealed modes while checking them, then inode-binds its disposable extraction
root, rejects symlinked or mismatched package entries, makes only extracted
directories writable, and removes the tree without hiding verification or
cleanup failures.
Its QEMU
smoke proves only root filesystem and init packaging; it does not emulate FPGA
programming, prove target readiness, or establish game or development-RBF
support. Package playability is recorded per exact artifact in
[core status](../../../docs/core-status.md); the earlier Mega Drive
launch/input/Stop/relaunch gates belong to the retired raw-RBF lane. The image contains no development RBF at either production
path and relies on volatile `/tmp` staging for an admitted upload. Native
development-RBF loading is hardware-tested for the existing MiSTer-compatible
lifecycle. Exact hashes and dated physical observations are in
[native-development-rbf-baseline.md](hardware/native-development-rbf-baseline.md);
the retired lane's game baseline remains in
[native-megadrive-baseline.md](hardware/native-megadrive-baseline.md).

## Contained development RBF diagnostic

`POST /api/v1/session/development-rbf` sends one bounded raw upload through the
agent to protocol-2 `load_development_rbf` with `development-contained-v1`.
The runtime contains the FPGA, programs once and publishes a generation with
no core/system/package identity. It supplies no package ABI, media, video or
input guarantee. Stop restores the locked idle artifact. Ambiguous mutations
are observed once and never replayed. An actual recovery failure retains
`reboot_required`; a separately requested reboot is verified by changed boot
identity and fresh idle. On the native agent that marker is HTTP 200
`state=stopping`, `development=true`, and `recovery=reboot_required`, and only
when the session was already development and runtime Stop returned
`reboot_required`. A Stop reply that is still `starting`, or a lost Stop,
stays `failed` with `development` set and no `recovery`, and does not arm
`POST /v1/development/reboot`. The decision table is the FES
[soft-restart Path B](../../../docs/soft-restart-path-b.md) note. Development uploads remain volatile.

### Target kit ownership

The production target agent enforces a renewable kit lease across game and
native development sessions. Authenticated clients read `GET /v1/kit/lease`,
claim with `POST /v1/kit/claim` (`request_id`, `owner`, `purpose`), and carry the
returned secret in `X-FogCast-Kit-Lease` on every hardware mutation and input
CONNECT. Mesh content pull and link are hardware mutations on that list.
Node, slot, and source reads are not. Request IDs are random hexadecimal strings of at least 32 characters;
retries reuse the same ID. Cache transfer, status inspection, and
`GET /v2/hostless/identity/{game_id}` do not reserve the kit. A Stop
ends the current runtime session but retains ownership for another launch.
The host session service owns lifecycle mutations; the kit launcher never
creates a second hostless owner when the configured host is unavailable.

Renew and release use empty POST bodies at `/v1/kit/renew` and
`/v1/kit/release`. The production lease lasts 90 seconds; active clients renew
before expiry. Expiry closes input streams and interrupts incomplete uploads,
then waits for admitted operations and invokes input neutralization, cast Stop,
and runtime Stop. Ownership becomes available only after successful cleanup;
failed recovery leaves it blocked. Agent startup performs the same cleanup.
The retained uinput device survives lease release.

An operator using the configured bearer credential may explicitly request
`POST /v1/kit/takeover` with `request_id`, `owner`, `purpose`,
`expected_generation`, and a nonempty `reason`. Takeover revokes the previous
lease through the same cleanup path; it never aborts FPGA programming halfway
through a transition. Clients retry a busy takeover using the same request ID.
Old lease credentials cannot stop or send input to the replacement owner.
This protects agent API operations; direct root SSH or runtime socket access
remains outside agent admission.

### Target maintenance ownership

`GET /v1/kit/maintenance` reports a public boot-scoped operation. An authenticated
`POST /v1/kit/maintenance/start` carries an owned ordinary kit grant and
`request_id`/`purpose`. The existing lease manager drains admitted operations,
neutralizes peripherals and stops through its existing runtime cleanup. It
persists the reservation before changing admission; only `ready` authorizes a
software transaction. Runtime-unreachable cleanup is a maintenance failure.

The private `/run/fogcast/kit-maintenance.json` retains the operation, generation,
boot ID and reservation credential across agent replacement on the same boot.
Public responses omit stored credentials. Ordinary claim, takeover, renewal and
mutation cannot bypass maintenance; kit-local launches share the same manager.
The host connection reports maintenance as busy with its owner and a maintenance
message, and blocked reservations as recovery-required, even while runtime idle
health is ready. Endpoint adoption accepts those lease states without granting
ownership and drops a stale local grant, so the kit stays busy or
recovery-required instead of looking disconnected.
Maintenance status returns unavailable until its persisted marker loads; it
never reports absent ownership during startup. A surviving active marker is
blocked before replacement-agent startup cleanup,
so startup never temporarily reopens execution. Failed/corrupt state remains
blocked rather than granting an automatic takeover.

`POST /v1/kit/maintenance/resume` and `/v1/kit/maintenance/end` carry the reservation
credential plus `operation_id`/`expected_generation`. Resume explicitly rechecks
idle before marking the same reservation ready. End rechecks idle before opening
ordinary admission. A completed private receipt supports lost-response
reconciliation; stale credentials cannot end a newer reservation.

Diagnostics use private hosts and immutable software bundles; returning to a
prior bundle requires explicit generation-checked activation.

Marker transitions take `/run/fogcast/kit-maintenance.lock`. The replacement
agent loads its marker asynchronously while ordinary admission stays revoking,
allowing process health to respond without claiming the kit. The agent's
`--print-maintenance-contract` flag exits without initialization and prints
`fes-kit-maintenance-v1`.

## Host kit lease

The production service owns a renewable target kit lease, shared explicitly
with its game/development client and target input bridge (including CONNECT).
The first hardware mutation claims ownership. A core-package launch whose
target claim is refused with `KIT_LEASE_BUSY` is returned as HTTP 409 with
that code and a fixed ownership message.
Mesh content pull is one of
those acquiring mutations, so a fresh session's first FPGA mesh launch on a
free kit claims before Ensure. Mesh content link requires the grant already
held. Renewal runs every 20 seconds
against the target's 90-second timeout. Client expiry uses the returned
remaining duration and local monotonic time, so a kit without an RTC works;
request round-trip time counts against that duration. Status and cache transfers do not claim
hardware. Stop, input detach and reboot require an existing grant and never
claim someone else's active session. Replacement operations retain the grant.
Explicit public Stop (empty body, or `retain_lease` false) releases its grant
after input/media/hardware cleanup. Sofa Soft-stop sets `retain_lease` true on
that same route and keeps the grant after idle cleanup. An idle selected-target
change may drop that client; the retained grant stays reachable for a later
explicit Stop. An already-idle explicit Stop still attempts that release when
the newly selected target's probe or Stop fails. A Soft-stopped legacy
non-package fpga_native session keeps its execution label and is still
already idle for that release. Soft-stop `retain_lease` still keeps the grants.
Invalidating one target removes only that client's retained grant. A release
that fails leaves that grant retained so the next explicit Stop can retry it. Replacement Stop retains ownership for the next launch.
Explicit release leaves a retained grant held when that lease still backs a
remaining play, so Soft-stop, relaunch, and an explicit stop of another target
do not revoke the live session. A failed explicit Stop of that surviving play
still releases the other idle grants. The tenfoot shell's rooms Soft-stop (now-playing B, Esc, Backspace, or s) is
the retain request and keeps the grant while the shell stays up. Kit Select+Start
is the library session's completed exit on that same route: `retain_lease` false
with the captured play. A package play sends that package generation. A play
without a package sends the session id, launch flight, game, and target; a
missing flight is not posted as an empty body, because an unbound stop would
release whatever play is foreground at admission. Idle cleanup releases that
play's grant. A bound stop releases only its target, and a host-only stop
releases no kit grant, so a grant retained by an earlier Soft-stop of another
target stays tracked until shell exit or Release kit. A Select+Start queued
while input is in flight keeps the play captured at chord time and does not
stop a replacement observed before the reply. A release that fails after the
play is already idle retries the idle-grant release; a failure that leaves the
original play running retries the bound stop. Unpaired sofa Soft-stop
still retains. Guide settings
includes **Release kit**. Confirm posts that same idle release and leaves the
menu open, so the host stops renewal and does not claim the kit again while
the menu remains up. The shell does not start a game while that release is
still in flight, including after settings closes. A running game keeps settings closed, so that row does
not release during play. Start, Q, or
closing the window waits for an in-flight Soft-stop, then releases that idle
grant. A confirmed idle service gets an empty-body Stop. When a play still
survives, the shell posts `release_idle` so idle grants drop without stopping
that play. A failed release is retried before the shell exits.
Application shutdown releases its grants after input/session cleanup.

Tenfoot shows foreign lease ownership from the
paired host's `/api/v1/health` target connection (`busy` plus owner). Tenfoot
uses this paired response with the launcher bearer; it does not query the
target agent's `/v1/kit/lease`, which requires the separate agent token.
Shutdown cleanup first checks local ownership: it invokes Service.Stop only for
an active host-only session or a foreground target with a held grant. Clean
idle after explicit Stop and never-owned idle skip the target Stop, while a
lost or foreign target grant fails closed. Once admitted, shutdown uses the
existing Stop timeout, retry, recovery, and error-propagation behavior.

A renewal error or expired grant invalidates local ownership and stops renewal.
The host does not automatically take over or fall back to an unguarded target
when lease endpoints are unavailable. An operator must inspect ownership and
start a fresh application session after lease loss. The target owns timeout,
revocation and serialized physical cleanup; host lease tokens stay in memory.

## Target identity and reconnection

Each named host target may have a persistent `target_id`, a lowercase UUID copied
into the target's private agent configuration during media provisioning. Legacy
`base_url`/`token` configurations may also carry a top-level `target_id`. Target
settings expose the ID without exposing the bearer credential. An explicit
`PATCH /api/v1/library/settings` with `{"prepare_target":"dev"}` creates and
atomically saves an ID only when that named target has none. Normal settings
updates and target renames preserve it. Preparing an ID requires a writable
private canonical config. The existing browser target settings provide this action.

The agent uses the configured identity, or persists one in
`/media/fat/fogcast/target-id` when no identity is configured. Authenticated
health reports it; anonymous health omits it. Advertisement runs independently
of HTTP startup, waits for an addressed multicast interface, and recreates its
listeners when interfaces or addresses change. Failed setup retries with capped
backoff. This handles the kit starting its agent before Ethernet is ready.
DNS-SD TXT keeps `protocol` and `target_id`. It also carries `node_id` (the
same stable id; the agent does not mint a second one), `mesh` (`major.minor`,
currently `1.0`), and `cap` (a capability bag). The kit bag advertises Execute
`fpga_native` and DisplaySink, plus InputSource for the kit's local pad path.
ABI or package-family suffixes are included only when the advertiser knows
them; the agent omits them because it does not inventory packages before
announcing, and an empty list is not a claim that any RBF runs. Encode
refuses a `cap` value longer than 251 octets, and parse marks that bag
unusable, so `cap=` stays inside one DNS TXT character-string of 255 octets.
DisplaySink
means the node can present. It is not HDMI or ADV liveness, and a host preview
is not this capability. Catalog, Content, Shell, and Coordinator are omitted.
The advertisement carries no credentials, title list, or lease secret. A `ttl`
key is parsed when a peer sends one and is not emitted here; the protocol
strawman leaves the seconds unsigned. Parsed TTL silence is absence for a
future placement choice only and does not release the kit lease. Phase 0
announcements that omit `mesh` stay directly bindable. The host collects those
announcements into an in-memory node inventory (`node_id`, mesh version, and
the `cap` bag) and serves it at `GET /api/v1/mesh/nodes`. The inventory has
one row per node id. Instances of one node at one address (the same kit
seen on two host interfaces) are one row; when the browse window's
instances of a node id name more than one address, the row carries
`address_conflict: true` and no `address`, and uniqueness is checked on
every instance before the rows are collapsed. The monitor observes these
advertisements even when the selected target has no Phase 0 binding. A
browse error keeps the previous rows. The inventory does
not adopt an endpoint, claim a lease, or make a title Ready. A later browse
that no longer sees a node, including a node that omitted `ttl`, drops that
row only. A mesh major other than 1 does not remove that direct bind; a
session that needs the mesh contract fails closed on that major. A random service instance and hostname distinguish
cloned identities on the same link; the persistent TXT identity remains stable
across reboots.

The host's selected-target monitor probes authenticated health, adopts the
advertised endpoint, reads kit lease ownership, then reads status before it
publishes `ready`. A kernel `boot_id` alone cannot identify a restarted agent
process. If a previously observed or locally held lease disappears or changes
generation, the host drops that target's cached play record and reconciles
against the returned status. This also covers a launch after an idle monitor
snapshot, followed by an agent restart before the next probe. A fresh host
follows the same bounded admission path without assuming
the agent's old process state.

Host-side content identity lives in `internal/meshcontent`.
`fogcast.ProjectMeshLibrary` projects the host library already stored
into that catalog shape: described package id and ABI, the household
firmware digest when that slot is required, the selected primary-media
digest, and named expansion digests. Stored SHA-256 strings pass
through `FromSHA256`. Primary media uses `PrimarySourceID`: the
format-3 source `MediaID`, which the executor records as
`SourceSHA256`. That id is not the post-link `ProgrammedSHA256`.
`MeshExpansion.Digest` is the slot-bytes digest, SHA-256 of the
expansion slot's own bytes (`expansion.Manifest.CartSHA256`). It is
not `Asset.ID`, not the archive `media_id`, and not
`ProgrammedSHA256`. `ExpansionSlotBytesID` names that digest. The
projection does not hash files again and does not link expansion bytes.
A package-backed Catch title uses the browse system `catch` while its
catalog storage platform remains `fpga`; its ROM-less entry needs only
the package/ABI slot.
A title that cannot be named is skipped. A catalog entry names the
title id (a catalog game id: lowercase ASCII slug), one execute kind,
and the required slots. A launchable `fpga_native` entry requires a
package/ABI slot. A launchable `native_emu` entry requires primary
media and carries no package/ABI slot; BIOS and expansion content-ids
are optional. A content-id is the unsigned strawman `sha256:` plus 64
lowercase hex of that slot's bytes. Deano has not locked the algorithm.
The package/ABI slot is the described package id and ABI, not a
content-id of an RBF. `PackageABI.Major` is that ABI's major, not the
mesh protocol major. `ReadyHere` requires that package id and an
eligible ABI id and major before it reports Ready. Package id alone is
not eligibility.

#### Configured host emulator

`[host_emulator]` maps catalog systems to local RetroArch cores. For the M1
Data Storm SMS runner, configure the verified core and its optional digest pin
along with headless display/audio settings:

```toml
[host_emulator]
binary = "/usr/bin/retroarch"
args = ["--appendconfig", "/etc/fogcast/retroarch-headless.cfg"]
env = ["DISPLAY=:93", "SDL_AUDIODRIVER=dummy"]

[[host_emulator.cores]]
platform = "sms"
core = "/usr/lib/x86_64-linux-gnu/libretro/genesis_plus_gx_libretro.so"
id = "genesis_plus_gx"
version = " c2838c7d"
sha256 = "051cb96ad3d1a98809c103b3836e2830e269de43f9f1943d482749873082bc49"
```

The host health response advertises mesh version 1.0 and software backend
identity, configured `core_version`, and observed core digest; `core_version`
is copied from configuration and is not queried from RetroArch. A missing or
mismatched core, or a missing/non-executable RetroArch binary, is unavailable. One
host emulator process owns the local session until Stop or process exit.
Host-only play is owned by the root host session coordinator regardless of
which kit is selected. Unscoped launch resolves execution first, and unscoped
status and Stop follow a bound FPGA foreground only when one exists; with no
bound FPGA play they use the root coordinator. Session identity, BUSY
admission, natural-exit reaping, and media cleanup therefore share one owner.
Unscoped status may report idle after target loss only when host-only execution
is active or no FPGA play/target is bound; a configured kit selection alone is
not an active FPGA binding. A bound FPGA play or explicit target continues to
report `MISTER_UNAVAILABLE` while kit state cannot be observed. Before replacing
media for another host-only launch, FogCast checks the local executor; an active
RetroArch process returns `BUSY` and retains its current media session.
M1 software-runner input uses a controller attached to the runner through
RetroArch's local joypad/udev input. Remote controller input to `host_only` is
outside M1. The #363 integration matrix
([`docs/mesh-m1-integration.md`](../../../docs/mesh-m1-integration.md)) records this non-goal.
In cast mode, host-only playback uses the managed RTP sender and target cast,
so the kit display has one owner: a host-only launch replaces that kit's package
play, and a kit core/package launch stops host-only playback. In non-cast mode
(media disabled, local MJPEG preview, or local ffplay playback), host-only and
kit FPGA plays are independent in both launch directions; kit-scoped Stop and
host Stop remain independent. `TestTwoNodeHostFirstThenKitKeepsBoth`,
`TestTwoNodeCastModeKitLaunchStopsHostOnly`, and
`TestTwoNodeHostMediaOnSameKitReplacesKitPlay` cover these rules.

`Service.MeshBackendLibrary` reads the existing local catalog and projects
package titles through the same helper as placement; raw games use the
configured execution resolver. `ProjectMeshBackendLibrary` groups entries
by catalog game id and links rows at read time when their primary-media
content id (the ROM sha256) and browse system both match exactly. The
package row's game id is the canonical row id and a matching raw ROM folds
in as its emulator option; with more rows, package rows come first, then
the lowest game id. A different hash (a patched ROM) or system never links,
and catalog rows are not rewritten. Each option keeps its source game id.
It keeps host-local and sourced options distinct, deduplicates
repeated content ids, and annotates observed node candidates. The library projection reads package and ABI facts from the configured
`[[targets]]` address, the enrolled origin as written in config. That
read is cached on the library path and times out with the placement
node-document deadline. `GET /api/v1/library/titles` does not send the
agent token to a discovered or reconciled address. The library client
does not follow redirects, so a 3xx is a failed read and does not
forward that token. Hostname and IP-literal reads each use their own
direct transport, so `HTTP_PROXY` does not receive the bearer. A configured hostname is resolved once per process
and later library reads dial only that pinned IP, keeping the configured
Host header. That lookup and the document read share one placement
node-document deadline. If a later lookup drops that IP, no bearer is sent and the
node is unavailable with `node moved; re-pair or confirm the new address`.
The pin is not persisted (#396). An IP literal is dialed as written. A failed or missing
read leaves the FPGA candidate unavailable (`package unavailable on node`). A missing, retained, conflicting, or mesh-major-incompatible
advertisement is unavailable. A `native_emu` advertisement remains
unverified for remote execution because the current capability bag has no
supported-system or emulator-version fact. The host-local `host_only`
path is the local emulator option (`HostLocal`); like launch, it carries
`local source unavailable` when the game's source is not available or
its root is offline. An option with no
host-local path carries an explicit reason when no inventory node can
run it (`no advertised executor in inventory` or `no compatible
executor in inventory`); each node candidate carries its own reason.
`GET /api/v1/library/titles` serves `MeshLibraryTitles` of that
projection. The route does not select a backend or assert composition
Ready. `content_sources[].node_ids` stays empty until #396; the
projection does not read node-document `content_ids`. A remote
`native_emu` candidate stays unverified until #298 and the runner
admission and provisioning prerequisites, and the projection does not
send a bearer for that read. A stored digest `FromSHA256` rejects
skips the title with `<slot> digest is not a stored sha256`. A built
id that is not canonical `sha256:` text skips it with `<slot> content
id is malformed`. The title is not dropped without a reason. When the
local catalog cannot be read the route returns 500 and does not answer
an empty title list. The remote emulator option is added only when
inventory advertises `native_emu`.

`meshcontent.Ensure` is the host ensure step. It takes one projected
entry and the executor the session is already bound to. Each required
content-id comes back Present, Checking (mid-pull), or Missing. A
required id that is missing and has no source is
`ErrContentMissingNoSource`. A Checking slot is not pulled again. A
pull that finishes Missing is `ErrContentPullFailed`.
`Result.Execute` stays false while any required slot is Checking or
the executor's ABI id and major are not eligible, and Launch returns
before the existing execute path. Expansion content-ids stay separate
and are linked by `Executor.LinkExpansion` only when every required
sibling is Present and the package ABI is eligible. A sibling that is
Checking or Missing is not linked. The same expansion name and
content-id may be linked again; that call is a no-op. Ensure does not
roll back a link that already succeeded. The host does not pre-link
them and does not program the FPGA. Ensure does not choose a node,
does not pull onto a node the session did not bind, and does not
release or change a lease. It also does not pull unless the caller
reports an owned binding that is not in use (`ErrLeaseNotFree`).

`LaunchOn` admits the request first, with the seam on or off. A
canceled context or a malformed game id returns before the core-entry
lookup, so that request does not pull and does not read the catalog
for the launch. When a mesh session is installed, it then takes one
launch snapshot: target name, TargetID, address, enabled state, the
captured client, the bound
node id, and the catalog row Ensure will check (package, ABI, media,
firmware, ROM, and expansion slots). Ensure runs against that snapshot.
When that launch asked for placement, `meshplace.Place` runs after the
snapshot and before Ensure. A selection of the bound executor is
recorded on the session. A selection of another FPGA kit claims that
kit's lease and rebinds the session, then Ensure and bind use that
kit. A conflict returns before the rebind. A `native_emu` selection
of any other node returns `ErrUnboundNode` before Ensure and before
bind, and the bound node stays where it was. A launch that did not
ask does not call Place, and a launch that names a target does not ask.
A known target that is already disabled is refused before Ensure. An
implicit target with an empty TargetID is the bound node only when its
name is that node. Otherwise Ensure returns `ErrUnboundNode` and does
not pull. Host-only mesh play stays on the installed session node.
When mesh ensure is on, an explicit FPGA `LaunchOn` whose kit is not the
executor already installed dials that kit and Ensures there. Before
Ensure, the snapshot stores that kit's client. Open creates a client
only for the selected target, so an explicit sibling that does not yet
have one is opened with the target client factory and captured on the
snapshot. The lease claim uses that client. A factory that cannot open
it returns before Ensure and does not pull. Readiness
keeps the selected-target session. That snapshot's executor is the
dial, so a later change of the selected session does not by itself
fail revalidation; the named kit's address, TargetID, and enabled flag
still do. A dial that does not return that kit's node leaves the
installed executor in the snapshot, and Ensure returns `ErrUnboundNode`
without pulling on it. A launchable FPGA entry that the foreign-kit
check would deny is rejected
before Ensure. For an FPGA entry, LeaseFree is the session-owned kit
grant and its generation. InUse is the Phase 1 busy connection: another
session holds that kit. A fresh session does not yet hold a grant.
Content pull is the lease-acquiring mutation: when that kit is free,
Launch claims the session grant before Ensure, and Ensure then sees
LeaseFree. A grant claimed in that call is released when Ensure does
not start execution. A grant the session already held stays held. A
held or denied claim is `KIT_LEASE_DENIED`. A held grant whose observed
generation differs fails closed before any pull, and that observation
is the selected connection only when the launch executes on that kit.
Host-only play stays on the host executor.

Ready for rooms and `GET /api/v1/games` uses that same grant when a mesh
session is installed. An unleased kit is free only when the bound
node's client can claim the pull. A lost or closed grant is not free.
A client for a different kit is not used. A foreign holder is not
Ready. Ensure's own LeaseFree is unchanged: a free kit is not owned
until the content pull claims it. Slot and source reads for that view
are one batch per request (`GET /v1/mesh/content/slots`), with a one
second snapshot on the host client. Each read has a deadline. A
transport failure is `CONTENT_UNREACHABLE`, not missing content.

Immediately before execute, and while lifecycle admission is held,
`revalidateLaunchSnapshot` compares that snapshot with live state. Any
difference — target disabled or removed, address or TargetID changed,
selection changed on an implicit launch, bound node changed, catalog
composition changed, or a different live client at the same address
and TargetID — returns `ErrLaunchSnapshot` and does not install a
client. A match binds the captured client. Launch does not overwrite
a different live client. The core path and the host-only path both use
this one check. With the seam off, a launch that did not rebind leaves
the target live: bind resolves the selected target under the target
lock at bind time. A placement rebind keeps the claimed kit pinned
through that bind.

A Checking slot waits up to the service checking timeout, which
defaults to 30 seconds and is clamped at two minutes
(`SetMeshCheckingTimeout`). The caller's cancel returns immediately.
The timeout returns `ErrCheckingTimeout`. Session launch maps
missing-with-no-source and a slot that ends Missing to
`CONTENT_MISSING` (422), a failed pull to `CONTENT_PULL_FAILED` (422),
a failed link to `CONTENT_LINK_FAILED` (422), a slot read that does not
complete to `CONTENT_UNREACHABLE` (503), an ABI the executor cannot run
to `ABI_INELIGIBLE` (409), a checking timeout to
`CONTENT_CHECKING_TIMEOUT` (504), and an unowned or denied lease to
`KIT_LEASE_DENIED` (403). `CONTENT_CHECKING` (409) remains the mapping
for Ensure's in-progress block. Launch always waits with a positive
timeout, so that 409 is not produced by
`POST /api/v1/session/launch`; the timeout wins. Ensure with a zero
checking timeout can still return the block.

The executor on the kit is `kitcontent.Store` in the target agent.
It holds one content-id per object, pulls bytes from a content source
on that kit, and links expansion slot-bytes there without folding them
into primary media or a programmed image. Pull honors the caller's
context and deletes a partial file on cancel or failure, so that file
is not Present. The agent serves that store at `/v1/mesh/content/*`
for the node's id. Opening the store does not create directories under
the media root; the first pull or link does. A partial file left by a
crashed pull is removed when the store opens. A pull refuses to start
when free space is under 32 MiB or the object store is already at 2 GiB.
Pull and link require the current kit lease. A link failure is
`CONTENT_LINK_FAILED`. A kit lease rejection of pull or link is
`KIT_LEASE_DENIED` on the host, not a failed copy. A canceled pull is
logged on the kit. Node, slot, slots, and source reads remain available
to other clients. The host authorizes pull as a lease-acquiring
mutation and link as a mutation that needs the grant already held,
including when the remote base URL has a path prefix. When a session
installs a remote executor, the authorizer is the client for the bound
node. Those routes drive the kit store.
They are not an Ensure-result document. The host installs the executor
only when `SetMeshExecuteSession` is called, so Phase 0 and Phase 1
launch stay on the existing path. The store does not program the FPGA.
Production `fogcast-api` and the `fogcast` CLI call `EnableMeshContent`
after open. `[mesh] ensure` defaults off, so that call leaves the seam
off until the operator sets `ensure = true`. The default stays off
until #177 (legacy fallback when the source does not advertise) and
#172 (kit home host) land. The agent installs a
launcher-backed content source unless `mesh_content = false`, which
restores a nil source and an empty ABI list. An empty ABI list also
makes that kit ineligible for production placement. With the switch on, the
node document's ABIs and package ids are read from installed package
manifests when that document is served. A directory named with the
64-hex package id, or with a Stage publication
`<package-id>-<token>`, contributes that package id. A package staged
after the agent starts is included on the next read, and a removed
stage is left out. The host re-reads that document when it asks the
bound executor for package ids or eligible ABIs, and keeps the previous
lists when the read fails. An empty ABI list
is not eligibility. A nil content source advertises nothing. When the
source is the launcher credential, the kit sends its own target id on
`GET /api/v1/mesh/content/source` and `GET /api/v1/mesh/content/object`.
The host admits those two GETs for any enabled configured kit that
presents a valid launcher bearer. They do not require that kit to be
paired to the presented bearer or to be the foreground selected target.
Other launcher operations require the requesting kit to be the bearer's
paired kit; session ownership, not selection, then gates stop, status,
and input (see the kit-only host description above).
A pull still caps each write at `min(quota-used, free-reserve)`, re-reads free
space while copying, and counts `partial/` bytes toward the 2 GiB
quota. A failed dial leaves the host seam off, so Phase 0 and Phase 1
launch continue. When the selected target's name, address, agent token,
node id, or enabled flag no longer matches the dial that installed the
executor, that session is dropped and the new endpoint is dialed.
Until that dial succeeds, readiness stays on the Phase 0 and Phase 1
path instead of the previous kit. Host-only titles are not projected
into the seam.

The projection is not a host route. JSON tags stay on the host catalog
shape. Ensure results have no JSON tags. `GET /api/v1/mesh/nodes` does
not call `ReadyHere` or `Ensure`. `POST /api/v1/session/launch` calls
Ensure only when `SetMeshExecuteSession` installed a session; otherwise
Phase 0 and Phase 1 launch are unchanged.

When that session is installed, `GET /api/v1/games` calls `ReadyHere`
through `discovery.ReadyForBoundExecutor`. Rooms Ready and Play follow
that result. A title is Ready here only when this shell has an Execute
binding, every required slot is Present on that executor, the lease is
free for this session, and the mesh-protocol major is compatible.
Bytes that exist only on a distant node are not Ready. The games row
then carries `ready_here`
false, `ready_block` (the ReadyHere block), and `next_action`. A true
`ready_here` still applies the catalog gates (`source_offline`,
missing firmware, ROM, or expansion, and the rest), so Play is not
offered for a host-only row whose root is offline.

When a placement ask is installed, or production placement builds
one, that same games read runs
`meshplace.Place` for each projected title and writes `placement`
(`selected`, `unresolved`, or `fail_closed`). The read uses the
candidates on the ask. Building the production ask may read a kit's
content node document; the read does not record the decision, does
not claim a lease, and does not launch. `selected` leaves this Ready result in
place, so Play stays the existing launch and does not ask which
machine. When that selection names a kit this host already sees in
use, Ready is cleared to `lease_held` so Confirm does not launch and
does not take the lease. `unresolved` and `fail_closed` clear Ready
when the row would otherwise be Ready here. The block is `placement_unresolved` or
`placement_fail_closed`, and `next_action` is `unavailable`. Rooms
Confirm does not launch and does not pick a node. Those two blocks
are codes, not a new sofa sentence. Edition Needs a choice, version
skew, and in use keep their own blocks when those already explain the
row. An Execute advertisement does not grant Ready. A capture preview
is not a DisplaySink on this read. No ask leaves Phase 2 Ready
unchanged.

| `next_action` | `ready_block` |
| --- | --- |
| `wait` | `ensure_in_progress` |
| `supply_content` | `content_missing` |
| `fetch_here` | `distant` |
| `wait_for_lease` | `lease_held` |
| `resolve_version` | `version_skew` |
| `bind_executor` | `no_capable_executor` |
| `browse` | `browse_only` |
| `unavailable` | `invalid`, `placement_unresolved`, `placement_fail_closed`, and any other block |

A slot mid-pull is Checking (`ready_block` `ensure_in_progress`,
`next_action` `wait`). Rooms show that as Unavailable or Checking and
do not Play.
`launchable` stays the platform and package gate from
`enrichLaunchable`, so a firmware-ready Coleco or ZX81 package is not
reclassified as browse-only.

When the session is not installed, `ready_here`, `ready_block`, and
`next_action` are omitted. `placement` is omitted too, unless
production placement is on and the host projects that title: then the
row carries `placement` alone.
Phase 0 and Phase 1 Ready stays composition against the bound executor:
Coleco and ZX81 packages and host-only titles stay Ready and launch as
they do now. A neighbor Execute advertisement still does not grant Ready.

The host authenticates health at its configured or last validated endpoint. A
legacy address-only target can bind a discovery-capable agent's existing ID
through the same private write path. A bound target may resolve matching
`_fogcast._tcp.local.` DNS-SD announcements after a read-only connection failure.
Health must confirm the expected ID and API version before endpoint adoption;
HTTP redirects are rejected. One DNS-SD instance may publish multiple addresses,
but multiple distinct matching instances/endpoints are ambiguous and are not
chosen automatically. Discovered DHCP addresses stay in memory.

The service's existing lifecycle admission serializes validation with launch,
Stop and development operations. A one-second host monitor provides retry
opportunities; failed lookups back off for 1, 2, 4, 8 and then 15 seconds.
Individual health probes and multicast browse windows are bounded and cancellable.
Browse is stopped with cancel when its deadline expires, so the DNS-SD packet readers exit.
Settings changes cancel an in-progress lookup, and shutdown cancels and joins the
monitor before releasing leases. Development recovery first programs idle on
the current boot. It starts a board reboot only when that idle program fails,
then uses the same read-only validation while retaining its existing lifecycle
admission. A soft reboot after FPGA or HPS activity can leave the kit
unreachable; if idle recovery still reports `reboot_required`, use a hard
power cycle.

Endpoint adoption reads public lease ownership and runtime status before reporting
ready. `maintenance` and `recovery-required` are accepted lease states. Adoption
does not grant ownership for them, and it invalidates a stale local grant.
The shared host lease object moves game requests, lease renewal, input
attach and input CONNECT to the validated endpoint. On an unchanged boot, a
locally held grant is retained only when its live generation still matches the
agent. An address change closes that target's old local input stream without
replaying held input; the user can attach input again through the existing
session action. On a changed boot or lost grant the host forgets that target's
old grant and session and closes its local input handle without remote Stop,
release, or input cleanup. Sibling target input attachments stay intact. A new user
launch may claim a free lease; discovery never claims or takes over a kit, and
never replays a launch, upload, Stop, reboot, or input operation.

The existing public health response includes `target.connection`; status responses
include `connection`, including unavailable responses. Its states are
`disconnected`, `connecting`, `ready`, `active`, `busy`, `version_mismatch`
and `recovery-required`,
separate from runtime/game state. Busy responses include the public owner label.
Named session reads, launches, Stops and lease checks resolve the configured
target by name. Each configured target keeps its own client, connection
observation and lookup backoff. An unavailable target reports its own configured
address and target ID; it never reuses another target's observation.
Settings edits clear the affected target's cached connection and lookup backoff
when its name, identity, enabled state, address or credential changes. An
explicit named Stop runs the same identity-checked discovery and ownership
admission for a sibling target, ignoring only the retry backoff; it does not
retry the Stop mutation.
Busy means another session holds the kit lease, or the kit is reserved for
software maintenance. Tenfoot rooms show a Ready
FPGA title aimed at that kit as Unavailable, with the copy "In use" /
"Someone else is playing on this machine. You can play when they're done."
Confirm explains and does not launch. Library and detail Confirm
apply that gate only after catalog blocks: a title missing Coleco BIOS still
opens the household firmware picker, and any other catalog block keeps its
own copy. Firmware import posts host library endpoints and does not claim
the kit. Rooms leave those destinations unchanged because a foreign lease
applies only to a Ready FPGA title. A host-only title stays Ready
and Play reaches the host executor. `POST /api/v1/session/launch` returns the
existing lease denial, without claiming, when that launch would use the busy
kit. A host-emulator title and a launch aimed at a different target still
proceed. That named target is the client used for the load, including while a
host-only session is still the active execution. The shell that holds the grant,
including after Soft-stop, stays ready and keeps Phase 0 Play. Generation
takeover remains `POST /v1/kit/takeover`.
The browser and tenfoot target views show this state. Manual addresses remain
usable where multicast is unavailable. This path has host/fake-peer regression
coverage; physical reboot and DHCP acceptance belongs to the selected FES image.

## Native kit launcher

The kit UI is `fogcast-tenfoot -gfx menu-display -home rooms`. The base-image
supervisor starts it after the runtime and agent. `ui/launcherconfig` reads the
provisioned `launcher.json` (`api`, `token`, `target_id`) and ignores keys left
by the removed grid launcher. `cmd/fogcast-kit` is a stub that prints `tenfoot`
for `--print-kit-ui`; it exists only for bases whose supervisor probes it before
starting the launcher, and kit software bundles carry it until every kit runs a
base that starts tenfoot directly.

A paired, authenticated host listener serves a restricted set of existing
library, artwork, and session operations. The host keeps target and input lease
ownership for host launches and Stop. When the kit has its own playable shelf,
tenfoot serves the kit-local catalog on loopback and launches through the
agent's local-control socket instead. A gamepad or USB keyboard on the kit
writes raw input frames to `/run/fogcast/local-input.sock` through
`ui/localfeed` while a runtime core is bound, and does not post those events to
the host. A pad on another machine is delivered on the host-to-kit input stream
for the session that owns the play. A missing stream is an error to that
caller. The kit pad does not fall back onto the host route.

Native input admits devices through `ui/controller` and maps them with
`controller.Mapper`. The mapper identifies a pad by its evdev bus, vendor,
product, version and name CRC, and reads it through the matching embedded SDL
GameControllerDB Linux entry. If the DB does not list the pad, it uses the
Linux gamepad-API codes plus a generic `BTN_TRIGGER`-range layout. Left stick
and D-pad share `left-x`/`left-y`, and a held D-pad hat or D-pad axis owns
that axis until it returns to centre. Logical codes are then remapped with the
profile `tenfoot.json` saves under the pad's GUID (`ui/inputmap`).

See [kit launcher](kit-launcher.md) and [host connection contract](launcher-host.md)
for setup, controls, exact routes, timeouts and ownership. The existing browser
listener remains loopback-only.

## Persistent kit software releases

The FES base-image `fes-kit-software` supervisor owns the agent, native runtime
and selected kit launcher as one userspace group. Immutable bundles declare the
board, architecture, base-system contract, interfaces and executable/asset
digests. Their interfaces include `menu.<interface>` versions the runtime admits.
The base image records the interfaces its sealed menu declares as `requires` in
`/etc/fes/software-base.json`. Install and activation refuse a bundle that does
not accept them or fits another base contract. At boot and in recovery the
supervisor holds such a selection back and runs the image's factory software in
its place; status reports `held_back`. Installation and selection live on verified FESDATA3 independently of
core packages and the base appliance image. The existing service scripts are
replaced by one supervisor entrypoint; physical transitions remain runtime-owned.

The lease manager's idle queue gives a caller-persisted software intent priority
after the current execution owner naturally releases. Queueing preserves that
owner's renewal and operations. Only a settled free kit can atomically become a
maintenance reservation. A private caller credential binds reservation recovery;
public operation fields cannot recover a diagnostic owner's token. Cancelling a
queued intent removes priority only after the durable cancellation is stored.
The supervisor closes an execution fence first; if that durable write fails, it
restores the queue before opening the fence, and leaves the fence closed when
the restore fails. Active maintenance retains its existing resume/end checks.
Trial reconciliation installs the persisted queue while the boot maintenance
reservation is still held, and only then ends that reservation. A cancelled
trial-restore receipt ends that reservation instead of retrying the same
operation ID. The stable
supervisor persists queue and activation state across reboot; the lease manager
does not persist its pending queue.

The supervisor waits up to 30 seconds for `/media/fat/fogcast/agent.toml` before
starting the agent, and bounds the launcher selection probe. That startup also
publishes `/media/fat/fogcast/state` as `XDG_CONFIG_HOME` and `XDG_STATE_HOME`
for the service group. A process that
exits before the readiness window is accepted is restarted within that startup
deadline. After acceptance, a launcher exit is restarted on its own while the
runtime and agent stay up. An accepted agent or runtime exit stops the whole
group, confirms every process group is gone, and starts it again, backing off
from one second to thirty seconds when the replacement dies quickly. The
replacement agent closes new execution claims before it listens. The supervisor
confirms that fence before probing the launcher and opens it only after the
group is accepted. A core that has reached `running_development` is accepted
once the fence is confirmed. A finished session whose fence cannot be closed
stays running with claims closed, and the next tick retries the fence. A load
still in `starting`, and an idle sample whose fence was not confirmed, stay on
the readiness budget. The supervisor verifies process exit before replacement, pins every service to
one bundle, checks startup and finishes the exact maintenance generation.
Accepted bundles remain selected. Failed startup permits one durably recorded
prior-bundle recovery attempt; failed recovery remains blocked. Appliance trials
run factory software, then reconcile persistent storage and queued work after
confirmation. Confirmation records the durable release, then rewrites that
boot's ticket to `trial:false` without changing its boot id or image, so
bundle operations are not refused until another reboot. When the rewrite
fails, the durable record still confirms the release and admission opens.
The storage gate accepts the leftover `trial:true` ticket for that boot and
image. The agent logs the failure and retries the rewrite once on a later
idle status read. Further status reads leave the ticket in place. The next
agent start tries the rewrite again. A base-image update blocks activation
and software installation.

`WithKitSoftware` proxies the five bearer-authenticated `/v1/kit/software`
routes to the fixed root-only supervisor socket. Upload is bounded by the bundle
contract and joins appliance-operation draining. Queue/cancel remain visible
while activation is deferred. Software proxy routes bypass the replaceable
agent's software drain so supervisor calls back into admission cannot deadlock;
the stable supervisor serializes installation and activation independently.
Authenticated health reports `software` separately from sealed image artifacts.
`targetclient` mutations have no implicit replay; `cmd/fes-software` records the
operation ID and expected generation for later status reconciliation. That
client dials with a 10 second connect timeout. Health, status, and list retry
transient network failures, including truncated response bodies, up to three
times; each probe is bounded by 30 seconds. TLS certificate verification
failures return immediately with a certificate error. Install, activate, and
cancel are not retried. `--timeout` remains the whole-operation deadline. After
a request body is written, the client waits
at most five minutes for response headers.
See [kit software releases](../../../docs/kit-software-releases.md) for commands,
recovery boundaries and evidence limits.

## Durable target core packages

`internal/kitpackages` retains canonical archives and sealed package members on
the mounted ext4 FESDATA3 volume at `/run/fogcast/fesdata3/core-library`.
Production opening verifies that volume against the block device
`appliancedata.Prepare` selects: `/dev/disk/by-label/FESDATA3` when the label
is present, including a labelled USB partition, otherwise labelled
`/dev/mmcblk0p3`. It also requires writable ext4 and exact mount identity
before mkdir. Before any of those paths open a block device, and before a
catalog scan or tape/firmware listing states a mount, `appliance/blockprobe`
reads only sysfs `size`, `device/type`, `device/state` and
`device/access_state`. Optical nodes (`sr*` or SCSI type 5) are skipped. Any
other device whose size is missing, unreadable or zero, or whose SCSI state
is not ready, is skipped as no-media and is never classified ok. A USB stick
with a readable size above zero and the SD card, including FESDATA3, stay
eligible. An open that is still required is bounded by a short
timeout. Subsequent operations recheck the mount and pinned store
directories. An absent FESDATA3 volume, a preparation error, a skipped
appliance update gate, or a failed open after successful preparation leaves
storage unavailable. That failure blocks kit-local inventory and withholds
factory selections. The kit-local socket `GET /v1/local/cores` returns HTTP 200
with an empty list and does not read factory package bytes.
Bearer `GET /v1/core-packages` reports the same unavailable store as HTTP 503
`CORE_PACKAGE_STORE_UNAVAILABLE`. Shared flock, file and directory fsync, immutable
identity and selection CAS coordinate independent agents. Reopening completes
verified interrupted unselected publications; selected corruption remains blocked.

Bearer `GET/POST /v1/core-packages` and `GET /v1/core-selections`,
`PUT /v1/core-selections/{core_id}` expose inventory, bounded archive installation
and checked future selection. Installation does not select or inspect hardware.
Selection compares `expected_package_id`, checks descriptor/core identity and
queries the native runtime's read-only compatibility authority before commit.
These storage operations acquire no execution lease; appliance update admission
excludes mutations during trial or image transitions. The target client does not
replay lost mutations. No delete, restart, activation or implicit rollback exists.

Kit-local inventory overlays explicit durable selections by core ID on factory
selections. Listing reads each factory manifest, streams the payload digest,
and streams the ROM-map digest, including the package id. Format 3 and format 4
maps are structurally validated while they stream, including binding to the
payload digest, and a map full inspection would reject is omitted. Only the
manifest and the running hashes are kept. Checking one map holds a fixed CRAM
destination bitmap for that package and does not retain the encoded map or its
decoded blocks. A finished factory catalog is cached until its stat fingerprint changes:
selection and member size, mtime, inode, and directory entries, plus the
size, mtime, and inode of `unavailable-cores.json` beside the selections
directory (or the line `unavailable absent`). Opening a
selection file does not change that key, so a durable selection read does not
start another factory scan. An optional core recorded in that manifest is
listed with block `Hardware unavailable` and a synthetic package id, and a
launch of that id is refused. It is not installed: Home does not open on it,
the shelf count excludes it, and Details does not show that package id. An installed package for the same core id stays
the listed row. A malformed manifest is ignored. After the durable store opens, the agent warms
that factory cache and the durable verification cache on a background goroutine.
The warm returns before the control socket and the bearer listener accept
requests, so it does not delay agent readiness. Durable verification runs, then
the factory scan, so only one payload is in flight. `GET /v1/local/cores` waits
at most 200ms for an in-progress warm. If the warm is still running it returns
HTTP 503 `{"error":"warming"}` and does not start a second scan or read package
bytes for that request. The kit-local client surfaces that as `warming` without
waiting out its 3s timeout. Tenfoot keeps the checking state and retries until
a list succeeds; the installed-cores room does the same. `POST
/v1/local/cores/{package_id}/launch` resolves the package through that same
list. A launch that arrives while the warm is still running returns the same
HTTP 503 `{"error":"warming"}`, claims no lease, and does not program a core.
The kit-local client returns `warming` as soon as that response arrives, as it
does for a list, so the caller can retry. It does not treat the response as an
unavailable agent and does not reconcile it as a lost launch. A successful list is
the finished catalog, not a partial one. The list handler waits on the
request context, so a client disconnect ends that wait and leaves the warm
running. One catalog scan runs
at a time. A fingerprint change while that scan is in progress waits for it to
finish, then scans once more when the tree still differs. Durable listing
reuses the byte verification recorded at install, reopen, warm-up, or the last
fingerprint miss while the sealed archive and member fingerprint (size,
mtime, ctime, device, inode, mode, and directory entries) still matches, and
does not read those payloads again. On Linux, ctime and device come from the
file stat. A same-size rewrite that restores mtime updates ctime, so the next
list verifies the bytes and withholds a package that fails authentication.
Select, path lookup, and launch reread the selected package with full
inspection before programming. Reopening the store verifies every installed
package before it serves. Corruption that leaves every stat field unchanged
waits for one of those byte reads. Manifest requirements block unsupported local media,
firmware, multi-ROM and native video-part paths.
`Launchable` describes local inputs, not hardware acceptance; native inspection
checks the exact canonical bytes before programming. Plain local loads use the
existing archive staging path, preserving native trusted roots. Durable mesh
inventory advertises a package only when that byte verification is current
for the same fingerprint, and omits the durable library when verification
fails. It uses those verified store objects instead of scanning only ABI
metadata.
Local launches also enter the appliance update drain before claiming execution.
The existing local execution mode remains volatile for settings/progress.

See [durable kit library](../../../docs/kit-core-library.md) for the operator
workflow and explicit storage/recovery boundaries.

## Installed core packages and library entries

Coleco firmware and optional ZX81/Coleco CPU expansion-bus modules use the normal library launch
path. A title may require a household firmware object; rooms/catalog **Ready**
follows that fill, and `session/launch` binds firmware before cartridge media
and reset release. Tenfoot Confirm imports an 8192-byte BIOS through the
existing media/firmware APIs. Expansion selection binds an independently linked
pack to the exact shell package. The Apple II disk is the first removable
media; see [removable disks](#removable-disks-fescomputer-media-units) and
[launch composition](launch-composition.md).

Library list, detail and variant responses report expansion selection and
readiness independently of firmware requirements, including firmware-free ZX81
shells. Coleco's bus belongs to `fes.application` 1.0 and admits optional bus
1.0 or 2.0; ZX81's belongs to `fes.simple-computer` 1.0 and admits bus 1.0.
Package composition carries the declared bus major to the linker. Each package
may declare exactly one optional bus,
and the selected module must bind that bus, map, package, BUILD_ID and payload.
The target agent validates the runtime's composed status against that ABI and
optional bus pair before confirming a launch or reconciling a running session.
Expansion admission distinguishes missing/invalid packs from catalog
failures: missing titles retain not-found responses, concurrent choices retain
conflict responses, and unexpected storage failures remain internal errors.
Malformed archives or incompatible compositions are rejected as admission
errors.

### FES Home system shelf

The embedded `example.fes-cores` Home room combines the kit-local package
list from `kit.cores` with prepared host-library systems from
`hardware.read`. Each host card publishes its library `game_id` and launches
through the library path; a ready host card replaces a blocked local row for
the same core, while a launchable local row stays selected. Host readiness
follows library launch admission, including selected video parts, required and
selected removable media, package composition, and the format-2 ZX81 machine
ROM linker. The format-2 ZX81 local row is blocked because local `LoadCore`
loads only the installed archive and does not splice the machine ROM. If
kit-local listing fails, Home still reads the host shelf and shows any
available host cards; with no host cards, it shows the kit-listing error.

### Home-computer packages and slot cards

`fes.computer` 1.0 (the Apple II pathfinder, `fes.apple2`) is a recognized
play ABI. Its package declares required fixed video, `fes.keyboard.hid`,
`fes.gamepad.ports`, stereo audio and `fes.media.apple2-floppy`, and may be
format 3 with one `firmware` ROM linked at download. The optional
`fes.expansion.apple2-bus` 1.0 is a multi-socket bus (`fes.apple2-bus.slots/1`,
physical slots 2, 4, 5 and 7), not a capability bit.

Atari ST (`fes.atari-st`) uses the same home-computer launch and media paths
with required `fes.media.atari-st-floppy` 1.0 and optional selected raw disk.
The base disk is exactly 720 KiB; the required geometry extension admits
80..82-track, 1..2-head, 9..10-sector layouts.
Optional `fes.expansion.atari-st-bus` 1.0 uses shared map
`fes.atari-st-bus.socket/1`, with one card in socket 1 through the same
slot-composition transport and independently checked shared linker. `.msa`,
`.stx` and compressed images require conversion to the admitted `.st` bytes
before upload. These new paths have host coverage; hardware acceptance is
pending.

`catalog/core_slot_expansions.go` stores one card per `(game_id, slot)` (schema
12); `fogcast/core_slot_expansions.go` validates import against the installed
shell (`corepackage.ValidateSlotCards`: shell binding, physical socket and one
trial link), exposes compare-and-swap selection and reports per-card readiness
in library `slot_expansions`. Launch reads every selected card before target
contact; a missing or incompatible card is an admission error, so no Stop or
programming follows.

Launch links on the host first. Format 3 uses `corepackage.PrepareROMInput`
to assemble the selected ROM and any selected composition inputs from their
raw assets. Selected video-part assets enter `ROMInput.Parts`; one preparation
pass links the video parts and ROM together through `expansion.ComposePartsROM`
and records the parts composition and programmed digest in the `rom-link.json`
receipt. Slot cards use `expansion.ComposeSlotsROM` and record the v2
composition and programmed digest beside ascending `slot-N.tar` members.
`romLaunchSource` does not build a standalone parts archive for a successful
launch. If preparation fails with selected slot cards, launch reports the slot
expansion as unavailable. If selected video assets are present, it checks
`ComposePartsArchive` only to classify the failure: a failed check reports the
video part as unavailable, while a successful check leaves the preparation
failure classified as a ROM admission error. A ROM-less shell sends
`corepackage.SlotCompositionBundle` (`slot-composition.json`, `package.tar`,
`slot-N.tar`, `linked.rbf`) through the existing compose route. The agent's
`corepackage.StageROMInput` /
`StageComposition` relink independently and refuse different evidence, then
publish the shell, one `slot-N-<publication>` directory per card (exactly
`manifest.json` and `cart.rbf`) and `composition-<publication>/linked.rbf`.
Restart adoption relinks those retained members. `internal/misterruntime/slots.go`
sends the runtime v2 form of `load_rom_composed_core` / `load_composed_core`
(also `load_initialized_composed_core`, which no producer uses yet):

```json
{"protocol":2,"operation":"load_rom_composed_core","package_path":"…","package_id":"…",
 "expansions":[{"slot":2,"path":"…/slot-2-…"},{"slot":7,"path":"…/slot-7-…"}],
 "payload_path":"…/composition-…/linked.rbf",
 "composition":{"composition_id":"…","package_id":"…","expansions":[{"slot":2,"expansion_id":"…"},{"slot":7,"expansion_id":"…"}],
  "shell_sha256":"…","payload_sha256":"…","payload_size":N},
 "programmed_path":"…/rom-link-…/programmed.rbf","rom_link":{…}}
```

The runtime reports the same v2 object under `active_package.composition`;
`Protocol2ActivePackage` decodes it into `SlotComposition` and the agent
publishes it as `core_package.slot_composition`. The v2 tuple is valid only
for an exact `fes.computer` 1.0 shell with the optional bus, every slot a
physical socket, a recomputed `fes-composition-v2` identity and volatile
persistence. With no card selected the load is the ordinary
`load_rom_library_core`. Single-socket ZX81 and Coleco v1 tuples are unchanged.
ZX81 single-socket composition admits optional `fes.expansion.zx81-bus`
1.0 or 2.0 on `fes.simple-computer` 1.0 and `fes.computer` 1.0, bound to
`fes.zx81-bus.socket/1` or `/2` respectively. The
v2 bus carries CPU clock and active-low reset within the same physical
socket. Import, staging, adoption and runtime status checks retain exact
shell and cart version binding; tuple identity domains are unchanged.

`internal/misterruntime/computer.go` is the protocol-2 client for the other
`fes.computer` operations. Each request is bound to the exact active package
generation and each reply must report that same `fes.computer` generation:

```json
{"protocol":2,"operation":"set_keyboard_hid","package_id":"…","expected_generation":N,"rows":[r0,r1,r2,r3,r4,r5,r6,r7,r8]}
{"protocol":2,"operation":"insert_media","path":"…/media.bin","expected_package_id":"…","expected_generation":N,"unit":0,"size":143360}
{"protocol":2,"operation":"eject_media","expected_package_id":"…","expected_generation":N,"unit":0}
```

Rows are nine 16-bit values; row 0 bits 0..3 and row 8 bits 8..15 must be
zero. `set_controller` is unchanged and carries `keypad:0` for these packages.
Status may report `capabilities.media_units`
(`[{"unit":0,"interface":{"id":"fes.media.apple2-floppy","major":1,"minor":0},"min_bytes":143360,"max_bytes":143360,"chunk_bytes":512,"state":"empty|loading|ready"}]`);
the decoder accepts it only for an active `fes.computer` generation whose
interface is active, ascending and unique by unit, and exact for the known
floppy interface. The agent publishes it as `core_package.media_units`.

`fes.application` 1.0 packages compose fixed 720p60 video with optional presence
of normalized gamepad and raw blob/stream media interfaces. Each implemented
operational interface is declared required; omitting input creates an autonomous
demo. The runtime remains the compatibility authority through its negotiated ABI
registry. Host session input attachment follows observed gamepad/keyboard
capabilities, so an application without input does not attach a controller and
an application gamepad does not pass through legacy Coleco/SMS keyboard mapping.
The host recognizes the application media transport by ABI and interface version,
never core ID. Library launches requiring application blob media reject a missing
selection before package activation; entry creation and development package load
remain possible before choosing/uploading media. Development loads with media
remain held until commit. Existing simple-game/computer behavior is unchanged.
These software contracts do not establish exact-image hardware acceptance or
general keyboard/mouse support. Shared controller ports are described below.

### Keyboard HID for home computers

An active `fes.computer` 1.0 generation with `fes.keyboard.hid` 1.0 takes USB HID
key state, not a machine-shaped matrix. Clients send physical keys as keyboard
events whose code is `0x1000 + usage` (`internal/hidkeys`; usages 0x04..0x7f and
the modifiers 0xe0..0xe7): the browser maps `KeyboardEvent.code`, tenfoot uses
SDL scancodes, and the kit maps evdev `KEY_*`. There is no per-core character
map; the core owns characters, shifted symbols, repeat and reset chords. The host
attaches input for these sessions even without controller ports and passes the
events through `POST /api/v1/session/input/event` and the host stream unchanged.
The browser's **Capture keyboard** session action forwards every key, including
Escape and Backspace, until **Release keyboard**; Stop stays the Stop button.
The browser posts these events one at a time in event order, so a release
cannot overtake its press. A release the host does not confirm stays pending
and is retried before the next event and on a short bounded timer.

mister-agent observes `CoreObservation.KeyboardHID` (package and generation) and
routes keyboard frames to `internal/input/keyboard_hid.go`. Host-stream and
kit-local holds are kept per source and posted as their union; posts are ordered
and do not hold the controller-port lock. Each change posts all nine rows with
`set_keyboard_hid` for the observed generation (the runtime writes only changed
rows). A failed post leaves the state unconfirmed, so a repeated press or release
posts it again rather than reporting success. A probe that does not confirm
the core keeps that HID binding and its held keys. The kit-local release that
meets that probe, and a later release while that generation stays bound, still
posts the neutral rows. A press waits until a probe confirms the generation is
active. If the release post failed, the next confirmed observation of that
same generation posts it before the new frame. That replay stays unconfirmed
until `set_keyboard_hid` succeeds. A later kit-local frame posts it again under
the input lifecycle lock, including a gamepad frame while controller ports stay
bound and skip observation. Confirmed idle, or a confirmed different
generation, drops the retained binding and does not replay those keys.
Source release posts the remaining holds; lease expiry, core replacement
and Stop neutralize best-effort, and the runtime itself neutralizes on Hold and
Stop. Controller frames of the same session still use `set_controller` ports 0/1
with `keypad:0`.

### Shared controller ports

Tenfoot forwards physical keyboards on player 0 without reserving a gamepad
port. Only physical gamepads occupy the two stable controller slots: the first
two pads opened take ports 1 and 2 (pads present at startup in `/dev/input`
order, later ones as they are plugged in, each taking the lowest free port) and
keep them until unplugged. The kits' `081f:e401` fixture pad is the
exception: it yields its port to any other pad that arrives, releasing what
it held there, and takes the port that pad would have had. A later pad that finds both taken never reaches a machine: it
drives the menu where a pad with a port would, and its Guide button still stops
a kit-local core. A `-input` node the classifier cannot place also takes a
port. Keyboard
connection or removal does not shift either controller. Keyboard disconnect
releases its held keys independently.

On a keypad session, a portless pad's Select reaches the focused launcher while
its controls own input, including the cartridge picker. During play the
portless pad cannot send Select to the machine or use it as a menu command.

If an HDMI control transition leaves display ownership uncertain, a portless
pad's B uses the same guarded return-to-play operation as a ported pad. Other
menu commands stay blocked until display ownership is known.

An observed `fes.gamepad.ports` 1.0 interface (under `fes.application` 1.0 or
`fes.computer` 1.0) attaches the ordinary host session
input stream and reports `core_package.gamepad: true`. It has two digital ports;
optional `fes.keypad.ports` 1.0 adds a twelve-key mask on each port. Attachment
uses exact observed interface versions, not a core-ID allowlist. A migrated
Coleco package omits `fes.keyboard`, so its pad does not pass through the old
controller-to-ZX81-matrix translation. Packages that still declare
`fes.keyboard` retain that translation.

`remoteinput.Event.Player` and the existing binary frame's `Player` carry port
0 or 1. Omitting the JSON field preserves port 0. The public
`POST /api/v1/session/input/event` and paired launcher's existing NDJSON stream
carry the same event. For example, this presses player 2 Right; changing
`Action` to 0 releases it:

```json
{"event":{"Player":1,"Device":1,"Kind":1,"Action":1,"Code":103,"Value":0}}
```

Gamepad codes 100–107 remain Up, Down, Left, Right, A, B, Start, Select.
Keypad codes 120–129 mean digits 0–9; 130 means `*` and 131 means `#`.
These are semantic gamepad-button events, independent of a keyboard layout.
A kit pad's saved profile can bind its spare buttons to `keypad-0` through
`keypad-9`, `keypad-star`, and `keypad-hash`. Keypad events require the observed
keypad interface. For packages advertising `fes.keypad.ports`, gamepad Start
also asserts keypad `1` (the common one-player game-select key), and Select also
asserts keypad `*` (the common replay key); both ordinary gamepad bits remain
set. Explicit keypad events combine with these aliases in the same keypad mask.
Host snapshots and reconnect replay retain each player's buttons and axes
separately. Axis directions use an 8000 deadzone and combine with held digital
directions, so centering a stick cannot release a held D-pad.

On the local socket each player index is its own claim. When a pad is
unplugged, tenfoot sends the releases for that pad's held buttons and then a
departure event, which releases that player's claim without closing the shared
feed or releasing the surviving pad. The departure marker is a gamepad button
release with code and value zero; it is consumed only on the local route and
rejected by the host stream. At most two pads contribute.
The kit keeps Select+Start stop chords separate per controller: tenfoot tracks
held and sent buttons per pad, and a chord needs both buttons on one pad. If
the pad that started the one-second hold lets go while the other pad still
holds its pair, the hold counts from when that other pad completed it. When the
active core has only the legacy single-pad contract, the kit retains the prior
merged port-0 behavior, including input from a surviving second physical pad:
the agent's pad merge keeps each kit pad's holds separately and unions them, so
one pad's release leaves the other's holds in place. A departure changes
nothing there, because a keyboard shaped onto player 0 may still hold
controls. Browser
and SDL physical-device assignment are unchanged; clients can submit explicit
players through the shared API without room or renderer changes.

The native target controller selects `controllerPortsSink` from a fresh runtime
capability observation under its existing input lifecycle lock. For ports cores
it bypasses uinput and sends the local protocol-2 `set_controller` full snapshot:
`package_id`, `expected_generation`, `port`, `buttons`, and `keypad`. Digital
bits are Up, Down, Left, Right, A, B, Select, Start; keypad bits are 0–9, `*`, `#`.
The runtime validates the complete request and owns the physical GP writes.
The kit-local feed is a unix socket at `/run/fogcast/local-input.sock` (mode 0600),
not another network endpoint, and there is no additional virtual-device discovery rule.
For observed `fes.coleco`, `fes.sms` and `fes.sg1000` with active `fes.keyboard`
1.0, the agent maps kit-local pad directions, left stick and A (plus Coleco B)
onto that keyboard matrix. Each physical pad retains its own buttons and axes;
their desired matrix keys combine by OR, including opposing stick directions.
A matrix post that fails stays unconfirmed. The next local pad frame sends
that pad's full desired matrix, so a release after an ambiguous press failure
still reaches the runtime.
Synthetic pad keys, the physical local keyboard and the remote keyboard retain
separate contributions, so one release cannot clear another device's hold.
Local pad frames do not also enter the virtual gamepad. Other cores retain the
single virtual gamepad and keyboard sink.
Installed-core control is a separate root-only HTTP socket at
`/run/fogcast/local-control.sock` (mode 0600, no bearer). It lists installed
packages and, when the kit lease is free, launches one that needs no cartridge
or firmware under owner `kit-hostless` purpose `kit-local-core`. The rooms
engine can list and launch through that socket when a `localcores` client is
injected (`kit.cores`, destination kind `core`). A listed core names `keyboard`
when its package requires `fes.keyboard.hid`, or plain `fes.keyboard` 1.0 with
`fes.video.session-display` 1.0 and no cartridge ROM (the ZX81 bring-up
computer). Master System, SG-1000 and earlier ColecoVision reuse that matrix
as a joystick, so it is not a keyboard requirement. `fogcast-tenfoot -gfx menu-display`
connects that socket and, while the core runs, writes pad frames through the
shared `ui/localfeed` socket. A host session, and any other tenfoot gfx backend,
leaves `kit` unset. Confirm shows the room launch overlay while loading, then
pauses menu presents when the core owns HDMI. "In use. Someone else is playing on this machine. You can
play when they're done." is the busy lease. A blocked tile keeps its block line
("Needs a cartridge", "Needs firmware") and does not call the runtime. Anything
else the socket cannot launch says "This core isn't available right now."
Every successful local launch returns its opaque kit-lease `generation`, captured
from that launch's claim. Local status includes the same generation throughout
launching, running and stopping, and clears it at idle. `POST /v1/local/stop`
requires JSON `{"expected_generation":"<64 lowercase hex characters>"}`.
Missing, malformed and stale generations return `409 in_use` without calling
the runtime. There is no unconditional or empty-body Stop path. The service
compares generation under its state lock and dispatches through that claim's
lease token; old renewal and Stop completions cannot clear a replacement run.
Clients retain the launch-returned generation instead of fetching whichever
claim happens to be current when Stop is pressed. A lost launch response is
unavailable, even when status shows the same package: package identity does
not prove session ownership. An explicit observation/adoption of a current run
is separate from completing an earlier launch.
Keyboard F12 or a pad's Guide button (BTN_MODE) calls the same generation-checked stop with one press; neither is forwarded to the core. Select+Start held for one second also calls it. A lone Select or Start is forwarded.
When the second of that pair goes down, tenfoot releases whichever of the pair
it already sent and suppresses both until one is released. Releasing either
before the second elapses cancels the stop and does not replay the swallowed
press, so the core sees at most a brief tap of the button that went down first.
The one-second mark is the next pad event or the existing frame tick, not a new
loop. The event that reaches that mark is not forwarded. Stop returns the runtime
to fes.menu; tenfoot resumes presents and redraws on that new menu generation.
On startup, with a local-control client and no local run yet, that tick reads
GET /v1/local/status until one read succeeds. A running answer with a valid
generation is re-adopted as this launcher's run: menu presents stay paused.
Late adopt drains outstanding menu work and skips the stray menu frame,
because that core already owns HDMI. An older menu failure does not block
that pause. The session status is the In use copy, Select+Start calls
generation-checked stop, and idle status returns to the menu. The idle menu
plane cannot scan out over a running core, so this panel is launcher state
and not a second HDMI picture while that core owns the display. A launching
answer with a valid generation stays in the launching phase, presents stay
paused, and Select+Start posts Stop only after a later status says that same
generation is running. The agent refuses Stop while the phase is still
launching, and that refusal would drop the claim. A stopping answer with a
valid generation is watched, with the pad held and presents paused, until
generation-free idle, which returns to the menu, or until that generation is
running again, which becomes the stoppable session. A failed read is retried.
Idle, and a running or launching answer with no generation, leave the menu
alone. Starting a launch in this process ends that probe, so a rejected
launch does not take stop ownership of a run already on the agent.
This is the launcher noticing a run the agent already reports.
It is not a claim that this process started the run, and it is not supervisor
adoption of a base image. A restarted supervisor stops leftover children and
starts a fresh group; a supervisor lock file blocks a second start. It does
not take over running processes.
While the phase is running, that same tick polls GET /v1/local/status at most
about once a second, off the app lock and single-flight. If the agent reports
idle or not running, tenfoot resumes the same way. A running status whose
generation or package differs from the launch claim is not adopted and is not
stopped. Tenfoot keeps menu presents paused and delivers pad input on the local
feed, without Select+Start stop, until status is idle with an empty generation.
That hold has no timeout. Stop that returns in_use
resumes too; only unavailable is retried, because the core may still be up.
Packages declaring firmware stay in that class: the local socket does not
link firmware ROMs. A cartridge launch posts `library_root` and `relative_path`.
The agent pins the library root and opens each relative component from that
directory handle, refusing a symlink under the root, so a parent directory
swapped for a symlink cannot name a file outside the library. A cartridge for
an installed core is linked by
LoadCartridge, which rebuilds the canonical archive from the extracted
directory, verifies the exact archived identity with the runtime and programs
the core through the existing staging path. A held, busy, blocked, or recovery lease returns 409 and does not
call the runtime. The claim starts renewal immediately. The first renew waits
a quarter of the time still remaining, so a slow launch renews before the
grant expires, and renewal continues until stop, a failed renew, release, or
process shutdown. A failed renew, and cancellation of the lease context on
takeover or expiry, clears the service token, phase, and package immediately,
so status no longer reports running. Launch and stop keep the runtime operation on the lease
context; disconnecting the local client does not abort an in-flight program.
Launch takes the input replacement barrier before `load_core`, so a kit-local
pad frame cannot rebind a generation that barrier just cleared. Stop uses the
owned stop path. Kit-local loads do not enter the coordinator, so takeover,
expiry, shutdown, and release after a failed load stop the runtime before the
lease can be freed. Cleanup that does not reach idle leaves the lease blocked.
A successful kit-local Stop waits until that revocation has left the revoking
state and the lease is free. Until then local status stays stopping; idle means
the next launch can claim. Release returns while cleanup is still running, and
a launch during revocation is in use. Cleanup that fails makes Stop return
unavailable. Disconnecting the local client does not abandon that wait. The
kit client gives the stop POST the runtime stop budget plus the lease cleanup
budget. A lost Stop response is reconciled from local status: generation-free idle
resumes, the same generation still stopping or running remains unavailable, and
a different generation is refused. Reconciliation never posts another Stop or
adopts a replacement merely because it runs the same package. Tenfoot's stop budget outlives that POST.
Bearer `GET /v1/status` still reads that coordinator record. While the record
is idle and a kit-local core is launching, running, or stopping, the response
reports state `launching`, `local`, or `stopping` and sets `observed_core` to
the installed core id. The coordinator record stays idle, so the read does not
open a host session. `GET /v1/local/status` on the local-control socket remains
the phase and package detail.
Host session clients preserve `local` as kit occupancy without assigning a host
game id, remote input, or host-session Stop behavior.

A pad or USB keyboard on the kit writes raw input frames to that socket; keyboard
frames carry the key's USB HID usage (`internal/hidkeys`, from evdev `KEY_*`). mister-agent
shapes them with `playhid.StreamEvent` from the runtime's controller-port and keyboard
capabilities, then applies the same keypad Start/Select aliases as the host stream.
HID sessions keep the usages; `fes.keyboard` and native cores first map a usage to the
ZX81 matrix or arrow code the kit sent before HID, so those cores see the same keys.
The socket is not an HTTP route and does not consult the kit lease, so a pad on the
kit drives the running core whichever host launched it. Frames are delivered only
while a runtime core is bound. A kit-local HID release is the exception: when a
probe fails, a release for the generation that probe kept is still posted. If
that post does not land, a later local frame retries it under the same lock,
including when controller ports are bound. The release stays unconfirmed until
the post succeeds. Delivery takes the same input lifecycle lock as host
attach and core replacement. While replacement holds that lock, from ReleaseAll
until the finish callback, an arriving frame is dropped before observation, binding,
or writing. The status read that binds a local frame, and the set_keyboard or
set_controller post that follows it, are each limited to 250ms while the lock
is held, so a stalled runtime reply drops the frame and releases the lock.
Idle observations and active observations without a controller-port binding
are cached for 250ms, then refreshed. An unavailable observation retires local
pad holds with a bounded neutral post. A failed post remains pending and keeps
the observation cache invalid; the next frame refreshes status and retries before
accepting input. Failed socket-disconnect cleanup preserves that pending neutral
too, including physical keyboard releases. Confirmed idle, a different core or a
changed runtime generation
discards the old contribution without posting its release into the new core.
Local frame drops, including idle,
replacement and delivery refusals, are counted by `LocalInputDrops()`.
The agent logs the cumulative drop count at most once per second when it grows.
The local listener serves one writer synchronously; that writer must close
before its successor can deliver frames.
Host attach and the host stream keep their own deadlines.
The host stream does not hold the controller-port mutex across
set_controller, so a slow remote post cannot block kit-local delivery.
The host stream and the local socket keep separate
button and axis state. On one player they combine by OR for buttons and keypad bits
and by the larger stick deflection for axes, so a release or a centred stick from
one source leaves the other source's hold in place. Local pads occupy P1 and P2
first; a remote pad uses the next free port, and Coleco's two-port limit rejects
a player that does not fit. Claiming local P1 immediately moves an already connected
remote P1 to P2; remote players without a free port still retire released buttons
and centered axes from retained state. Releasing the local claim therefore cannot
replay controls the remote player has already released.
Closing the local socket releases the local source
only. Tenfoot writes physical pad and USB keyboard events to this socket
through `ui/localfeed` while a kit-local core it launched is running. That feed does not use the host
session's input.ready, launcher.json reachability, or which host holds the
lease. Select+Start held for one second posts an explicit `POST /api/v1/session/stop`
for this host's active or failed session: `retain_lease` false with the captured
play. Package plays send that generation. Package-free plays send the session
id, launch flight, game, and target, and are not posted unbound. A queued chord
keeps the play from chord time. Idle cleanup releases that play's grant and
renewal of that grant stops; grants retained on other targets stay until shell
exit or Release kit. A release that fails after idle retries the idle-grant
release rather than the captured play. Rooms B/Back stays the retain
request. For a kit-local run it goes to local
control `POST /v1/local/stop` instead, with the exact launch generation. A
foreign-owned core accepts local play controls, but this launcher cannot stop
it. Stop and kit menu actions stay on the kit. If the
socket is not listening, play input reports
the failure, waits one second before dialing again, and does not post the pad
to the host. Remote pads go from the host to the kit on that session's input
stream (`POST /api/v1/launcher/input` for a pad on another machine, and
`POST /api/v1/session/input/event`). The host reuses an attached, ready
stream only while the core, package, generation, keyboard, gamepad-port,
keypad, and relative-mouse capabilities still match. Status reconciliation
replaces the stream when any of those change. An explicit attach of a
stream that no longer matches is refused and leaves the existing stream
in place. If the stream is not live, the host
returns 503 `INPUT_UNAVAILABLE` (`no live input stream`) and does not accept
the event. Kit-local pads stay on the unix socket and do not silently fall
back to the host.

Disconnect of the host stream releases the remote source. Lease expiry, core
replacement and Stop neutralize every source on both dirty ports. A failed
write remains dirty because delivery may have happened. Cleanup attempts both
ports and propagates failures. A reconnect cannot publish its replay until the
old stream finishes releasing input. Core replacement uses the existing input
barrier; every cleanup retains its original package and generation. A failed
zero request may retire old local state only after a fresh runtime observation
proves clean idle or a different active generation. Same-generation failures,
unavailable observations and reboot-required states remain errors; cleanup never
zeros a replacement core. New generations begin with empty host and target state.

The host package store validates and atomically publishes immutable archives by
package ID. Serialized publication checks request cancellation after acquiring
its lock and before linking an archive. Catalog schema v8 associates each
stable game entry with an explicit package ID and optional media role/digest.
Multiple titles can use the same core. Importing a package or media object does
not select or activate it.
The `fpga` browse platform is not a cartridge/runtime system. Library scans
exclude its logical root. See [package library API and operations](core-package-library.md).

`fogcast/core_packages.go` owns installation, inspection and checked selection.
`internal/hostapi/core_packages.go` exposes those operations. Read-only target
`POST /v1/development/core/inspect` stages and invokes native `inspect_core`
without acquiring a physical lease or replacing input. The target runtime
remains the compatibility authority. Target transitions can report busy;
failed inspection cleanup cannot report compatibility success.

Package-backed `session/launch` uses the confirmed-package transition with
explicit library context, before ordinary game launch detaches prior input/media.
The service resolves selected immutable bytes under lifecycle admission, then
records the library identity only for the exact confirmed package generation.
Changing the selection affects future launches. Runtime status stays truthful;
the host adds its explicit library association and does not infer one after a
restart. Existing ordinary cartridge launch and Stop paths remain in place.
Replacing a recognized-ABI native package session with a cartridge or host-only
title stops the package-load target and clears package ownership first. True
Diagnostic sessions still require an explicit Stop.

Library titles whose installed package has a recognized play ABI
(`fes.simple-computer` 1.0, `fes.simple-game` 1.0, or `fes.application` 1.0)
resolve to `execution: fpga_native`. `fpga_development` remains the no-ABI
fallback and the explicit LoadDevelopmentRBF / development-core path. Target
status may still report `development: true` for the package-load transport;
the host session and catalog labels follow the ABI policy, not that flag.
After a host restart, an in-progress recognized-ABI package session is
reconstructed as `fpga_native` from that CorePackage ABI. A raw development
RBF or unknown ABI still reconstructs as `fpga_development`.
A failed development observation keeps that execution and the kit's package
and recovery evidence. The host drops the reported game and system from the
retained play and from the status it returns. `GET /api/v1/status` for a
named target and for a paired kit copies that returned status, so the
response omits the game the kit was recovering from.

Media objects are immutable SHA-256-addressed bytes in the existing catalog
database. Host storage accepts 1 byte through 32 MiB independently of target media capacity.
Imports stream to a private temporary snapshot before taking a database writer
transaction; SHA identity, declared length and cancellation are checked before
atomic metadata/chunk publication. Schema 8 stores new objects in 64 KiB chunks,
retaining schema 7 inline objects. Reads verify contiguous chunk order, size and
digest; launch receives a verified private snapshot, never a live database
cursor. Failed imports and closed snapshots remove temporary files. Backups
still cover a single catalog database, not a second persistent asset directory.
Interrupted import reads preserve cancellation and deadline errors through the
service boundary rather than classifying them as invalid media.

`protocol/core_media.go` projects supported transport semantics from exact
package ABI/interface declarations. When a shared unit has machine-specific
source images, its capability also reports that format: `fes.zx80` uses raw
`.o` files for next-start setup, while ZX81 and Timex packages use `.p` files.
The offline
`core-media-capabilities` API/CLI separates `import_max_bytes` storage policy
from each role's `min_bytes`/`max_bytes`, format and transport. It explicitly
reports `source:declared-contract` and `compatibility:unknown`: this is not
a live target observation. Unknown versions expose no supported roles, and
optional interfaces still require active runtime support at launch.

Legacy target delivery accepts role `blob`, 1..16384 bytes, for declared
`fes.simple-computer` 1.0 or `fes.application` 1.0 and `fes.media.blob` 1.0
capabilities. Application controller ports and keypads compose with either
media transport; they do not change the selected transport or its size limit.
A package with
both required `fes.media.blob` 1.0 and required `fes.media.blob-stream` 1.0
uses the explicit stream transport. Its offline declared safe range is
1..32768 bytes (32 KiB), not the host's 32 MiB import capacity. No core-ID
allowlist selects this behavior. Unknown stream versions do not widen legacy
delivery. The raw `core-media PATH` CLI and its host development endpoint
retain their original 16 KiB limit.

Stream delivery uses authenticated target `POST /v1/development/media-stream`
with fixed `Content-Length`, `application/octet-stream`, package/generation
headers and the existing kit lease. It shares lifecycle and update exclusion
with legacy `/v1/development/media`; it does not add a host management route
to the paired launcher listener. The adapter stages through a bounded buffer,
honors request/lease cancellation during staging, then rechecks identity and
observed limits before the single protocol-2 `load_media_stream` request:
`path`, `expected_package_id`, `expected_generation`, and `size`.
Legacy `load_media` is unchanged. No network caller supplies the staged path.

Runtime `capabilities.media_stream` and target `core_package.media_stream`
carry the exact interface version and observed `min_bytes`, `max_bytes`, and
`chunk_bytes`, associated with the active package and generation. Admission
uses the coordinator's retained copy of that observation from normal package
activation. Status snapshots deep-copy it so callers cannot mutate retained
media admission through a returned capability pointer. Admission
requires min=1, max=32768..33554432, chunk=512 and both active interfaces;
missing or invalid observation fails closed. FogCast still caps selection and
delivery at the declared 32768-byte guarantee even if an endpoint reports more.
The runtime owns the actual endpoint query and physical transaction. Generated
constants live in `protocol/internal/generated/fes_simple_computer.go`, retaining
the shared emitter's `generated` package. The runtime serializer fixture at
`internal/misterruntime/testdata/protocol-v2-media-stream-responses.jsonl`
tests the C++ JSON/Go decoder boundary, not physical hardware behavior.

`fogcast/core_media.go` rejects assets outside the selected capability's size
range before opening a verified snapshot or activating hardware. Library
storage/import and target delivery deliberately have different limits.
It validates selection and snapshots bytes before package
activation. One lifecycle admission spans package activation, media delivery,
and any Stop/recovery cleanup. Delivery binds the confirmed package ID,
generation, target and target ID; failure is not a usable launch. Kit, browser,
and CLI use the same session launch path, with no core-ID media registry.
Launch target binding occurs only after lifecycle admission. After activation,
media failure receives a separate bounded cleanup deadline, including when the
launch caller canceled or timed out.
Snapshot close failure after delivery enters that same Stop/recovery branch;
it cannot turn a running launch into an error without attempting cleanup.
For stream delivery the host uses `max(UploadTimeout, 150s)`, still bounded by
the caller's deadline/cancellation. The target retains its operation owner for
a bounded 135s call covering the runtime worker's 120s budget after staging.
Legacy delivery retains its configured host timeout and existing 15s adapter
budget. Ambiguous mutation replies are never replayed. These tests and limits
make no SMS execution, mapper, or hardware-acceptance claim.

Schema 7 seeds the historical Coleco diagnostic and binds existing Coleco
entries once, preserving game IDs and history. The licensed source remains in
`catalog/seeds` for migration only. New entries select media explicitly;
cleared selections are not restored on restart. Package/media selection affects
the next launch, not the active session. The raw `core-media` development
operation remains available. This transport is not a retail compatibility
claim or support for arbitrary new media roles.

A confirmed package activation commits its host-side ownership only after the
previous host executor stops successfully. If that cleanup fails, the service
retains the host owner and a package recovery marker, reports the observed
FPGA package with the recovery error, and blocks input attachment. Stop or a
subsequent package launch retries target recovery and host cleanup before
clearing either pending owner.

Runtime reconciliation may report `State=idle` with a retained runtime error,
such as a failed native core load. Coordinator Stop fast-paths only clean idle;
it retries runtime Stop for idle-with-error and clears the error only after a
confirmed clean idle response. A failed Stop keeps the recovery error visible.

An idle menu therefore does not by itself establish launch readiness. A new
explicit library launch can clear a retained idle legacy-launch error through
one existing leased Stop request before its single activation. Source/media
validation, target identity, API and ownership admission still apply. Cleanup
must confirm clean idle; active sessions, save failures, attributed package
failures and reboot-required states are not silently cleared. Cleanup failure
blocks activation and remains visible. This does not replay the earlier failed
launch or automatically reboot. Other recovery states still require explicit
Stop or operator attention. Kit launch diagnostics record the submitted game ID and
bounded result code, separately from the currently displayed selection, without
logging credentials or raw response bodies.

Stop errors additionally expose an allowlisted `stop_stage`, independently of
the target protocol `phase`. Admission health, ownership, status and lookup
backoff are distinguished from `target_stop` and later recovery. `target_stop`
means the client call began, not proof that the remote mutation executed. Stage
reporting preserves cancellation and protocol error identity. Explicit Stop
ignores the background lookup backoff timer, but still performs fresh bounded
health/version, identity, ownership and status admission. Polling retains its
backoff; Stop does not bypass those checks or replay a mutation. This fixes the
host-side backoff refusal, not every possible Stop failure, and is not itself a
hardware acceptance claim.


## Developer video parts

The separate developer parts path admits a format-2 `fes.coleco`
`fes.application` 1.0 package, or a format-4 shell with exact 8 KiB firmware
and 16 KiB cartridge ROMs, with optional `fes.expansion.coleco-bus` 2.0 and
exactly one optional video marker: `fes.fabric.video.raster-rgb888` 1.0 selects
`fes.coleco-video.parts/1`, while `fes.fabric.video.native-pixels` 1.0 selects
`fes.coleco-native-video.parts/1`. Native version 1 fixes the source at
256×192 TMS9918 palette indices; it requires a linked part to capture frames
and generate output. The fabric markers have no GP capability bit. The shell
keeps its fixed 720p video and declared audio/input interfaces, GP BUILD_ID and
package identity. Both closed layouts require one video part; one Coleco bus-2
expansion may also occupy its separate socket. Each asset binds the exact
sealed shell package, payload hash and BUILD_ID. Admission rejects simultaneous
source markers, raster/native part mixing and mismatched receipt layouts.

From `sources/FogCast`, prepare the operator's selected parts offline:

```sh
go run ./cmd/fes-parts -package /absolute/path/shell.fcore \
  -video /absolute/path/direct.fexp -out /absolute/path/parts.tar
# Add -expansion /absolute/path/card.fexp to select a CPU expansion too.
```

The command creates a private file and prints its composition identity. It
selects the layout from the sealed package marker and never contacts or claims
a kit. The file contains canonical `parts.json`,
`package.tar`, ascending `part-expansion.tar` when present, `part-video.tar`,
and `linked.rbf`. The target recomputes all linked bytes and the typed
composition identity; uploaded linked bytes are comparison evidence.

POST the bounded file as `application/octet-stream` to
`/v1/development/core/parts/inspect` for read-only native admission, then
`/v1/development/core/parts` for activation with the existing kit lease. Both
require the ordinary agent bearer credential. Neither accepts query paths or
`X-FogCast-Package-ID` library context. Inspection reports the exact base
descriptor and composition with `persistence_mode: volatile`, and leaves the
active machine unchanged. Activation uses the existing coordinator and physical
replacement lifecycle through local protocol-2 `load_parts_core`; inspection
uses `inspect_parts_core`. Active status exposes `core_package.parts_composition`
with layout, sorted role/part identities and programmed payload digest/size.

Private part companions remain owned with their base publication. Restart
adoption independently recomposes their bytes before recognizing an active
parts tuple. Generation reconciliation compares the complete parts tuple, so a
lost response cannot confirm a different video selection. The production CPU
expansion route does not accept this transport.

Both native and raster video use the factory catalog and household profile
selection described below. A native shell has no built-in direct output:
library Play must link a valid selected video part before target mutation.
The [native Kit 2 diagnostic](../../../docs/validation/2026-10-03-native-video-parts-kit2.md)
records exact developer Direct/Scanlines compositions, fresh video/audio
captures and Stop/relaunch. It does not qualify native factory/profile selection
or an appliance image.

Library video selection uses the separate `/v1/library/core/parts` route with
one exact `X-FogCast-Package-ID`, a bounded octet-stream body, bearer
credential, existing kit lease and update exclusion. `LoadLibraryPartsCore`
retains library context through the coordinator and adapter. The agent admits
the selected parts independently, inspects the base core-data namespace even
for a composed load, then sends local protocol-2 `load_parts_library_core`
with the fixed core-data root. Native admission repeats the namespace check
before programming. A durable namespace cannot be replaced by a candidate
without its persistence contract. This closed Coleco parts lane remains
volatile; its exact parts tuple, persistence mode and generation are checked
on response reconciliation and retained on restart adoption. Developer routes
continue to reject library headers and remain explicitly volatile.
Host preference and availability selection is described in
[Library video preferences](#library-video-preferences).

Both layouts keep fixed 720p output. Native parts capture and buffer source
frames; raster parts process the shell's timed pixels. Profile selection adds
no runtime output-mode switching, CRT, overlay, audio processing or
hot-reconfiguration capability. Host tests establish software coverage; hardware acceptance remains
separate for exact shell, part, runtime and image artifacts.

## Library video preferences

The household `[library].video_profile` setting accepts `direct` or `scanlines`
and defaults to `direct`. The existing library settings API and browser Settings
save it through the atomic `library-settings.json` overlay. Changing it affects
the next library launch; the running machine retains its selected parts and
composition identity. **Manage FPGA library** imports a video-part archive for
an explicitly chosen profile and shows each entry's resolved output and
available choices. Installing a published core also installs its declared
video companions through the same immutable inventory; those parts need no
separate manual import.

`catalog/core_video_parts.go` stores video archives in the existing immutable
chunk store. Schema 17 adds a separate inventory with one part per exact
`(shell_package_id, profile)`. Reimporting the same part/profile is idempotent;
a different part at that mapping conflicts rather than replacing it. The
profile is household metadata outside the immutable part manifest. Imports
accept archives up to 32 MiB in total, require the matching closed native or
raster video socket 1.0 and map,
validate archive identities, and fully compose against the installed sealed
shell before storage. Reads recheck the archive, part identity and shell
binding. Video imports do not enter CPU expansion inventory.

| Host operation | Endpoint |
| --- | --- |
| List imported video parts | `GET /api/v1/library/video-parts` |
| Import a bounded binary archive for a profile | `POST /api/v1/library/video-parts/{profile}` |
| Inspect the next launch for an entry | `GET /api/v1/library/core-entries/{game_id}/video` |

`fogcast video-part-install direct|scanlines PATH` is the CLI for that import.
It snapshots a regular `.fexp` within the same byte bound before opening HTTP.
The local check accepts `fes.fabric.video.raster-rgb888` and
`fes.fabric.video.native-pixels`; any other socket returns `BAD_REQUEST` and
is not uploaded. The accepted bytes are posted as `application/octet-stream`
to `POST /api/v1/library/video-parts/{profile}`.
`fogcast.ImportCoreVideoPart` reads the body again, requires one of those
sockets, loads the installed sealed shell named by the archive, and composes
the part against that shell before `catalog.ImportCoreVideoPart` stores it.
Storage still requires socket 1.0 and the matching closed native or raster
map. A lost response is not replayed. Success requires the returned part
identity, shell package and profile to match the snapshot.

`fogcast/core_video.go` considers only parts for the entry's exact installed
package and declared video socket. The closed Coleco shell contract admits
format 2 or format 4 with exact 8 KiB firmware and 16 KiB cartridge ROMs, and
rejects mixed markers and unsupported versions even without installed parts.
If the preferred Scanlines profile is absent, selection uses an installed Direct
part for that same shell. A native shell without that fallback rejects launch;
it cannot launch its vacant base. A valid selected Scanlines part works without
Direct installed, while selecting Direct requires its own matching part.
Raster shells retain built-in Direct as their final missing-part fallback;
a core without a video socket uses its built-in output. An installed part selected by
either preference or fallback must pass integrity and composition checks with
the entry's selected CPU expansion; failure blocks launch before a target
mutation. The read-only resolution reports preferred and effective profiles,
part identity, built-in output, fallback reason and availability for each
choice. When a native selection has no matching part, it reports
`builtin: false`, omits `part_id`, and marks the effective choice unavailable
with its reason. The library panel displays that unavailable output without
claiming a built-in fallback. Absence of an installed part means unavailable, without asserting that
the core has insufficient LUTs or routing capacity. Successful composition
checks the part's actual changes against the exact frozen shell and its reserved
region.

Library launch uses the existing lifecycle admission and fully links the video
part and optional Coleco bus-2 expansion before dispatch through the library
parts route described above. The host recognizes the confirmed application ABI
as `fpga_native` and binds the complete parts tuple, programmed digest and
generation to the library session. The shell package, BUILD_ID, fixed video and
audio/input contracts remain those of the sealed base. This selection path
accepts the same two closed Coleco layouts as the developer path, including a
format-4 layout with exact 8 KiB firmware and 16 KiB cartridge ROMs.

## Described-core persistent data

`fogcast/core_data.go` resolves a selected immutable library package under the
existing lifecycle admission, binds each data result to that package and the
current target, and exposes settings/progress APIs documented in
[the package library guide](core-package-library.md#persistent-settings-and-progress).
Host code consumes the runtime's `persistence_layout` metadata without a second
ABI registry. Selection rejects persistent-to-missing/different-layout changes,
including when no record has yet been written, and checks durable target data.

`targetclient/core_data_client.go` sends bounded archives and expected package IDs to
`internal/httpapi/core_data.go`; writes and library loads use the existing kit
lease. Read-only data inspection does not claim hardware. The target coordinator
serializes these calls against lifecycle operations. `internal/misterruntime/core_data.go`
privately stages/admit-checks exact bytes, supplies the fixed trusted
`/media/fat/fogcast/core-data` root, and calls protocol-2 `inspect_core_data`,
`update_core_settings`, or `load_library_core`. No network caller supplies a
filesystem path. Failed cleanup remains owned, and an ambiguous settings write
is never replayed. A successful settings-only write or definite admission/revision rejection
releases only the grant acquired for that operation; an existing session grant
stays held. Described-package Stop uses protocol 2 so retained unsafe
persistence recovery cannot trigger the legacy development autoreboot path.
Leftover staging from a failed inspect or settings cleanup does not divert a
later native-game Stop onto that protocol-2 path. Explicit library loads and
described-package Stop retain the same diagnostic dispatch events and native
event-dump import as the other runtime operations.

The runtime owns bounded record validation, revision CAS, settings/progress
semantics, atomic publication, and physical restore/capture. It refreshes a
same-core incoming record after outgoing flush, before activating the next
generation. Development `load_core` remains volatile. Active package status
reports persistence mode separately from its historical development execution
label and host library association. A safely resumed save failure preserves
its exact generation and can restore host input; unsafe persistence recovery
retains package/generation attribution in failed status and blocks input.
Existing post-activation HostOnly cleanup recovery remains unchanged.


## Initial Atari ST library disk

A writable format-3 ST library launch may omit the disk and boot to GEM with
an empty drive A. Required floppy interfaces declare hardware support, not a
requirement to insert media. The library can also format a blank FAT12 `.st`
(`POST /api/v1/library/atari-st-disks`) and bind it as that title's drive A.
Binding requires a format-3 `fes.atari-st` firmware package with `fes.computer`
1.0 and required base and writable floppy interfaces. Store-only creation does
not inspect or require a package.
The host stores the formatted bytes in the content-addressed media store; later
GEMDOS writes stay on the existing durable drive-A record. When a disk is selected, the launch includes its
exact admitted raw-ST source and explicit game/base-media binding in the closed ROM envelope. The
host snapshots and hashes those immutable bytes before target mutation. The
agent rejects initial disks on development routes, stages them privately and
passes the fixed durable-media root to the runtime with the linked ROM load.
The runtime validates the source and saved record before replacement, refreshes
the record after outgoing save, uploads the chosen base or saved bytes while
execution remains held, binds the ready drive and then releases the CPU. A disk
failure cannot report a successful running launch. Every format-3 ROM-linked ST
library activation uses `max(UploadTimeout, 300s)`, including plain firmware,
cartridge-only, video-only and combined parts, with either an empty drive or an
initial disk. The validated launch source selects this budget; earlier caller
deadlines and cancellation still apply. Other cores retain their configured
upload budget. Existing live insertion/ejection keeps the running machine intact.
Launch confirmation checks both ROM/parts identities and the ready persistent
disk's game/base binding; an initial disk is a startup input, not an immutable
identity imposed on later live media changes.

## ROM-bearing package inspection

`corepackage` accepts closed format-2, format-3, and format-4 packages. Formats
3 and 4 carry exactly `manifest.toml`, `core.rbf`, and `rom-map.json`. The
format-3 `rom` manifest table names one ROM requirement (id, firmware/cartridge
role and exact binary source size) and binds the map's length and SHA256.
Format-3 identity uses its own domain and hashes all three exact members.
Format 4 binds its map and two ordered ROM requirements in the `rom_map` and
`roms` manifest fields; format-4 identity also hashes all three exact members
under its own domain. The maximum archive/import size is 65 MiB; format-2
payload limits and identities remain unchanged.

Import, private staging, inspection and restart adoption validate and preserve
the map. The Go reader checks its closed JSON shape, unique destinations and
source ranges, device/encoding, and binding to the manifest's payload digest
and source size through `expansion.ParseROMMap`. It does not trust a map
supplied separately by a media upload. Expansion composition retains the whole
sealed shell package, including the map, and its package identity.

ROM-map inspection streams closed objects and fixed 40-destination words rather
than retaining JSON copies of the whole map and every block. It bounds maps to
32 MiB, blocks and words per block to 256, and unconsumed decoder input to 4 KiB
so oversized JSON values cannot grow the decoder buffer. Private staging reads
the declared upload into one bounded allocation, clones its small manifest and
borrows payload/map slices until publishing independent sealed files; other
archive readers retain independent member ownership.
Staging cancellation reaches map decoding and is checked between words as well
as during decoder reads, before any publication or hardware transition.

Format-3 library entries select one exact-size binary through the named ROM
selection API. The host sends a source-only `rom-link.json` envelope containing
the sealed package, ROM bytes and optional expansion asset. It does not run
Python or build the programmed RBF. The target negotiates `rom_linking: 1`,
validates the package/map/source bindings, composes any expansion, then applies
the ROM map using the Go linker before entering the input replacement barrier.

The runtime accepts `load_rom_core`, `load_rom_library_core`, and
`load_rom_composed_core` with the retained programmed artifact and a `rom_link`
identity binding the named ROM, map digest, source digest/size, and programmed
digest/size. It validates and rechecks the artifact before hardware mutation.
Bare format-3 and old initialized format-3 loads are rejected. Active status,
lost-reply reconciliation and restart adoption retain this tuple; adoption
relinks retained source inputs and compares the programmed bytes and identity.
Two ROM selections on the same core package are distinct active instances.

Production ZX81 exports use format 3. Existing format-2 packages retain their
launch behavior, including the older ZX81 Python prototype during transition.
The image selector preserves all three sealed format-3 members. Host-side tests
do not establish hardware acceptance or kit performance; those remain evidence
for the exact tested package and software.

### Local core publication

The systems catalog reports a package as installed only when its declared video
companion mappings also match. A previously imported base shell with missing
companions remains available for the ordinary Install action, which imports the
pair without programming the target. Conflicting mappings still require repair;
install does not replace them. Complete cached installs remain installed when
the original publication archive becomes unavailable.

FES publishes the ROM-less `fes.ramtest` utility in the standard catalog and
selects its OSS 100 MHz package for the factory image. The image selector
admits it through the same sealed package path; the kit-local installed-core
inventory marks it launchable without firmware or media, like Pong.

The optional `[core_catalog] path`, with a stable per-library `library_source_id`, selects a FES-published version-1 index.
`corecatalog` reads a bounded closed schema and verifies the canonical index
digest, contained relative archive paths, file size and SHA-256. FogCast stages
canonical package bytes and checks package/core identities before importing to
its existing core-package store. An entry may include `video_parts` references
with profile, part ID, contained archive path, archive size and SHA-256; the
entry's exact package ID binds every companion to its shell. The catalog digest
covers these references. `InstallAvailableCore` snapshots the canonical shell
and all declared companions, and admits each through the shared
`corepackage.AdmitFactoryVideoPart` real linker before any package or video
mapping is published. It then imports the shell and profile mappings through
the existing stores. Identical installs are idempotent; a different existing
profile mapping conflicts and remains intact. Package-only catalog entries
keep their existing behavior. This does not add video parts to mesh inventory
or change the kit-local cartridge launch path.
Available systems and manifest-derived setup
requirements are exposed under `/api/v1/core-catalog`; existing manual APIs remain
available when publication is disabled. Guided title setup writes the existing
catalog and ROM selections locally; target compatibility remains a launch gate.
It never acquires a kit grant, changes placement or binds a live session.
Publication namespaces are stable across generations. Serving-library identities
are configured separately, so identical local game ids from two hosts remain
distinct. Both identities are independent of executor identities. No second mesh catalog, coordinator or lease is introduced.

A successful catalog refresh retains online catalog state when the selected
package changes or is not yet installed. Changed references and their setup
choices are cleared; setup is fetched only for an installed selection. Setup
request failures do not masquerade as a failed catalog fetch.
Mesh projection preserves explicitly selected primary blob/disk content. Linked
ROM content supplies the primary slot only when no such media is selected.
Incomplete ROM selections retain their package-backed browse row; normal launch
validation still enforces required ROM inputs.

The tenfoot `core_library.go` overlay consumes the same `hostclient` catalog/setup
methods as the browser, reached through Settings → Systems. It keeps publication
and serving-library references intact without changing session placement. Existing
file enumeration, OSK, overlay geometry and renderer are reused; selected file
bytes stream to core-media after regular-file and descriptor size checks. BIOS
selection remains a separate explicit host API action. Asynchronous operations
lock overlay mutation and use bounded contexts; failed refresh retains cached
rows with setup disabled. App/renderer snapshots copy row data and pointer hit
routing gives the systems overlay priority over room/catalog widgets.

The general system workbench reuses that overlay for installed-package setup,
sealed ROM selection and prelaunch media; catalogue setup also exposes declared
cassette roles. Required linked cartridge ROMs satisfy their package's startup
media requirement. The media picker checks declared format/size limits, and
launch retains the ordinary conversion and package admission checks. Setup
retains existing package, ROM and media choices on retry; it does not overwrite
a conflicting saved entry.

The Sinclair ZX81 room retains its local package/ROM setup, specialist expansion
import and prelaunch cassette selection. The picker saves a `.p`
selection as the role that package declares: `cassette` for `fes.media.zx81-tape`,
and `blob` for a legacy `fes.media.blob` package. Installed setup uses existing
package/library/ROM endpoints independently of publication. Media selection
retains the captured package and previous media ID for compare-and-swap; expansion
import does not fit the asset. Catalogue schema 16 adds optional `in_progress`
presentation, without changing immutable admission or shared wire definitions.
The hardware projection carries each setup's core ID through the tenfoot action;
bundled starter cassettes appear only for `fes.zx81`. ZX80 next-start setup
imports raw `.o` cassettes; ZX81 and Timex setups import `.p` programs. The
live-media selector continues to admit `.p` only. Machine-bound
tape-shelf reads apply the same starter filter, including the live picker.
The workbench may suggest a same-target ZX81 package with required
session-display, HPS DDR and ABI-specific media interfaces when the selected
package lacks them. Optional declarations count only when the exact package is
confirmed active on the target; prelaunch candidates are not active, so their
optional interfaces do not enable the selector. It checks each candidate's ABI,
firmware and saved-media admission in package-ID order, continuing after an
incompatible candidate. For format-3 packages, candidates must share the
`machine-rom` firmware ID, role and source size; their sealed ROM-map digests may
differ. A package-inventory error omits that optional suggestion;
the selected package is inspected separately so an unrelated bad archive does not
hide the saved setup.
For the supported format-3 ZX81 package's 8 KiB `machine-rom` requirement, the
host bundles the standard image from `misteross/cores/fes-zx81/rtl/zx8x.hex`
with SHA-256 `14ad84f4243efcd41587ff46ab932d11087043e8d455a1ed2a227b9657828dfa`.
When a matching library entry has no firmware selection, the first firmware
read imports this verified image into the existing core-media store and binds
it with the existing compare-and-swap selection API. An explicit selection is
preserved. This lets the normal Workshop start path satisfy firmware admission
without opening the import prompt.
When a title switches between format-3 ZX81 packages that declare the same
`machine-rom` firmware ID and exact source size, the host rebinds the existing
media ID to the selected package with the media-ID compare-and-swap API. This
preserves both the provisioned image and a user-selected image; a selection
whose ROM ID or size differs remains stale and blocks launch until explicitly
replaced. If stored bytes fail integrity checks during rebind, readiness stays
false while the old media ID remains available as the firmware picker's
compare-and-swap value, so the selection can be replaced.
The attributed `internal/zx81tapes` assets enter the existing media store only
after explicit selection. A menu-display kit enables live routes for a legacy
`fes.simple-computer` package only when that package advertises
`fes.video.session-display` 1.0 and `fes.memory.hps-ddr` 1.0. Home/Select first
requests `POST /api/v1/session/display` with `visible: true`, the captured
session ID and existing target/package/core-generation headers. `visible`
composites the plane over the machine picture, and `focus` says whether the
launcher or the machine receives input. An omitted focus is the one
visibility implies: launcher when shown, machine when closed. Requests name
it only otherwise, so a host and a kit that ship separately still agree on
opening and closing the room. A null or empty focus is refused, not
defaulted. A shown plane with `focus: "machine"` keeps machine input, as a
surround around the picture needs, and a peer that predates focus refuses
it. A closed plane requires machine focus. Moving focus on a shown plane
keeps its display generation. The same display route accepts a running
`fes.application` machine, such as the Coleco native shell, when its active
FPGA identity reports those two interfaces. Application machines have no
removable-media unit, so this path needs no media-unit identity or selection;
the computer path retains its existing media-unit checks. The agent's input
fence holds delivery during each transition, but only a launcher-focus request
retires held keys and neutralizes the mouse. Controller-port releases received
while the launcher has focus clear their retained source snapshot without
being sent to the core, so released buttons cannot replay when focus returns.
The runtime also neutralizes the single `fes.gamepad` interface and gates its
worker's nonzero snapshots during launcher focus. The fence ends focused when
the runtime confirms launcher focus. After a refusal it follows the focus that
the session's reply reports, and after an unconfirmed failure it stays focused.
The paired listener admits only
that kit's captured session, hardware/starter reads, exact starter imports,
media imports of at most the largest image the media selector inserts
(839,680 bytes, a declared-geometry Atari ST floppy) and live
replace/clear/display operations. The unit still checks its exact format and
size on insert. These do not change the library's next-start media selection.
The Change media selector header shows `media_name` from the last confirmed
live-media insert, retained by the target's session coordinator for that package
generation. Eject, library disk insertion, and development-media loading clear
it because they can replace unit 0 without providing a live-media filename; disk
save preserves it. Preflight failures and explicit no-change rejections retain
the confirmed name; an uncertain reply after dispatch clears the cached name
and prior confirmed-eject marker rather than reporting old media state as
current. Status clears the cached name and binding after
it observes an inactive or changed session, preventing a restarted runtime from
reviving a filename when it reuses a package generation. Existing launch
selections and adopted sessions have no recorded original filename and display
that the name is unavailable. A confirmed eject also carries `media_ejected` in
the session projection for legacy `fes.simple-computer` media, so tenfoot can
show no media without treating an unknown launch filename as an empty unit.
Empty and loading units show their observed state. Numeric GP rejections from a
dispatched insert do not retain the prior filename: the runtime cannot
distinguish a `MediaBegin` rejection from a later data or commit rejection.
Completed busy rejections from media-unit insert or eject retain the prior
filename; a confirmed eject clears it.
The name is presentation only; admission continues to use the captured session, target,
package and generation.

It also admits the core-entry list, exact single-entry reads, and exact
package, ROM, expansion and media selection routes used by setup. Core-entry
routes require one validated game ID and the expected path suffix. Installed
core-package inventory and exact descriptor/media-capability reads are
available; package import and compatibility mutation remain outside the paired
listener.
Public and paired display/live-media mutations compare the captured target name
and ID with the coordinator's current target before service dispatch. Rebinding
a configured name to another kit rejects the old binding even if that kit has
the same package and core generation.

Paired session reads use the current foreground fields for that kit immediately
after launch, or its retained target record while another kit is foreground.
For described cores, enrichment requires the observed package and core generation
to match the record. This keeps the captured game identity consistent between
session reads and display replies.

The runtime opens a separate display generation. The asynchronous presenter pins
package, core and display generations instead of following a replacement;
idle presenters cannot acquire an active session plane. ZX81 selects launcher
pixels only after a full completed frame on its shared 720p raster. CPU, RAM,
audio and the expansion socket continue. Back/Return closes and drains the plane
before input resumes, unless the TV surround keeps it shown.

With the **TV surround** setting on (`surround` in `tenfoot.json`, the
default), tenfoot opens a capable session's plane with machine focus as soon as
the session is active. The parked frame becomes `drawSurround`:

- an opaque cabinet and bezel;
- the title, system and room hint;
- a clear window over the runtime's `menu_display.picture`, which
  `gfx.MenuDisplay.SessionPicture` reports, with shaded edges.

Until the picture is known, the frame is fully clear. While the hardware
room holds launcher focus on that plane, tenfoot sets `room.overlay`. The room
then clears to a premultiplied dark scrim and switches to light text on dark
surfaces, so the machine stays visible behind it. `gfx.Color` is straight
alpha, and `Software.Clear` and replacing fills store the premultiplied form
that the plane blends. Home/Select and Back move
focus on that shown plane without pausing or rebinding the presenter. Input
handoff (releasing held keys and closing the local feed) happens only when
focus moves. A refused surround closes the plane, and the setting stays off
for that session. UI navigation is suppressed at both launcher and target;
held keys and controller buttons require release and a fresh press, including B
when Stop fails. Button releases still clear suppression while Stop is pending
or retryable. Failed close retains
focus for an explicit retry. Stop retains the display binding and pending display
reply until idle or a replacement session is confirmed. If Stop fails, Back,
controller B and shell exit can return the captured plane; a late open does not
reopen the room or implicitly close the plane. Display faults cannot invoke MENU
reload or Stop.

F12 opens **Change media** during an active session when the package offers
removable media and the hardware controls are available. If the kit's
session-display plane is required and launcher focus is not active, tenfoot
focuses the plane first, then opens the selector after that transition succeeds.
The core keeps running, and the evdev F12 route consumes the key instead of
forwarding it as HID. On a host display, tenfoot opens the existing hardware
room on the host window and uses the host live-media route without a kit
session-display transition. When a kit-local core owns input, F12 requests
local Stop instead. F12 on the idle menu does nothing.

Media delivery can briefly reject a concurrent launcher frame as busy. A healthy
probe clears that presentation error and its retry delay; the room removes the
temporary HDMI notice while retaining the cassette operation's status.
A running `fes.computer` generation that reports `fes.media.zx81-tape` 1.0 uses
that same `POST /api/v1/session/display` when it also reports
`fes.video.session-display` 1.0 and `fes.memory.hps-ddr` 1.0. The plane
composites launcher frames over ZX81 video; closing it restores those pixels
without reprogramming the core. A tape unit without the plane does not open
the kit routes. Older packages keep prelaunch controls and the separate
host renderer keeps its live routes. The shared wire layout is in
[mister-packages session display](../../mister-packages/docs/session-display.md).

The session service records every kit's game, development load, package
rejection, and recovery state in `plays[target]`. That per-kit record is the
authority for kit status and lifecycle admission. Root `activeExecution` owns
only the local `host_only` RetroArch game; kit loads and failures do not stop or
replace it in non-cast mode, and kit-local cores execute directly on the kit.
Scoped Stop and status use the corresponding kit record. Library firmware and
media bindings, including post-Start delivery, use the selected kit's play
context so an independent local host game cannot redirect them to another target.

On Stop of a running library ZX81 package that requires
`fes.media.zx81-tape-write` 1.0, FogCast captures a completed BASIC SAVE for the
captured game and package generation, imports its `.p` bytes into core media,
and inserts the idempotent game/media catalog row before acknowledging the
snapshot. Capture keeps the FPGA snapshot frozen and dirty until that
generation-bound acknowledgement succeeds. Capture, import, catalog, or
acknowledgement failure returns `save_failed` and leaves the play active so Stop
can be retried; the runtime independently refuses Stop while an unacknowledged
SAVE remains. The selected core ID is retained with the launched play, so a
known non-ZX81 library core skips this bookkeeping without another catalog read.
For a bound ZX81 play, status, package, generation, or required tape-write
capability mismatches return `save_failed` with structured expected and observed
values in the host log. An empty capture means no SAVE is pending. When the
active package or game binding is unidentified, catalog and status lookup
failures are logged; Stop proceeds unless an active writable package yields
captured bytes that cannot be attached to a game. The runtime still refuses to
retire an unacknowledged SAVE. The running cassette picker adds that game's
saved programs alongside the attributed starters. A
saved row already names durable core media, so selecting it arms the existing
media ID without importing the bytes again. This is software coverage; exact
artifact hardware acceptance remains separate.

The configured host capture sender is one physical pipeline with one RTP
destination and sender token. Only its managed sender plus target cast path
places host-only playback on a kit display; MJPEG preview and ffplay playback
remain local. Cast mode has one display owner and replaces the other play in
both directions. Non-cast mode leaves host-only and kit FPGA plays independent
in both directions. Host-only playback claims the sender pipeline for its
target until its media handle stops. A host-only start on another target fails
with `MEDIA_BUSY_OTHER_KIT` (HTTP 409), and the target cast started for that
launch is rolled back. Configuration does not bind the RTP destination to a
named target address, so the host cannot validate destination-to-kit
correspondence; operators must configure the destination for the intended kit.

### Atari ST raw floppy geometry

The original `fes.media.atari-st-floppy` and writable extension keep their
737,280-byte raw ST admission. A package must require
`fes.media.atari-st-floppy-geometry` 1.0 to admit the twelve layouts formed by
80..82 tracks, 1..2 heads and 9..10 sectors of 512 bytes. Length uniquely
identifies geometry; other sizes and compressed MSA/STX streams are refused.
Non-720 KiB immutable sources require little-endian boot BPB fields matching
their sector size, total sector count, sectors per track and heads. Legacy
720 KiB sources still permit arbitrary boot sectors. The target independently
stages and checks immutable sources before runtime insertion. Durable mutable
snapshots retain their admitted length even if guest writes alter the BPB.

The existing initial-ROM envelope carries the exact disk length through its
receipt and immutable publication. No disk remains a valid GEM launch. Live
unit 0 retains the base floppy interface and advertises 368,640..839,680 bytes
only when the required geometry extension is also active; admission checks
the discrete supported lengths rather than accepting every value in that
range. Disk replacement, save and restore use the existing library binding
and package-generation checks. A blank disk from the library uses one of these
twelve layouts, defaulting to 80 tracks, 2 heads and 9 sectors.


### Independently installed rooms

`fogcast-tenfoot room-install [--rooms DIRECTORY] PACK_DIRECTORY_OR_ARCHIVE`
snapshots a room's `room.toml`, Lua and artwork into the local launcher's room
directory. It accepts either a source directory or a portable ZIP. The default
resolves `tenfoot.json` `rooms_dir` before `FOGCAST_ROOMS`, then uses the
persistent config location shared with the launcher. Use `--rooms` when the
running launcher uses a custom room directory. A loose pack inside the rooms
directory can be installed directly; a directory source that contains the
installation directory is rejected. Installation uses POSIX file locking on
Linux/macOS. This is local installation, not a host-to-kit transfer API.

`fogcast-tenfoot room-export --output FILE PACK_DIRECTORY` validates a source
directory and atomically publishes a portable ZIP. The ZIP stores the pack
files at its root. Import extracts and validates the archive privately before
the ordinary installer changes the selected snapshot; no source checkout is
needed on the launcher's machine.

Installation checks the manifest, main script, icon presence and syntax of all
Lua files before atomically selecting the snapshot. Packs are limited to 64 MiB,
2048 entries and 16 MiB per file (Lua sources retain the 2 MiB engine limit);
symlinks and nonregular files are rejected, hidden entries are ignored. Managed
snapshots live below `.installed`, separate from loose development packs and
`<room-id>.state.json`. Each running instance reads its original snapshot, so an
update cannot mix old Lua with new artwork. The previous selection is retained.

Reopen **Rooms** and select the room again to load the selected version without
restarting the launcher or rebuilding Go, the OS or any core. The launcher trials
load and the first update/draw before replacing the working instance. If either
fails, it preserves the working instance and restores the previous selection;
a failed first installation is deselected, allowing the embedded room of that ID
to remain available. A concurrent newer install is not undone by an older
failure. Home and Rooms navigation refresh the index from embedded defaults and
the current rooms directory. Home trials a changed installed selection before
replacing an open instance. Room replacement is refused while a game is running
or launching, including when the selected pack is loose.
Nested rooms retain their parents and persistent state stays outside the pack.

Errors in later callbacks retain the normal room error handling. Explicit
`fogcast-tenfoot room-rollback [--rooms DIRECTORY] ROOM_ID` selects the previous
installed snapshot; when there is no earlier installed snapshot, it deselects the
pack and restores the loose/embedded default (or removes that room from the
list). Reopen Rooms and select it to apply. Snapshot files are
retained; no installed versions or user state are garbage-collected. These
commands require a launcher containing the installer once; subsequent Lua/art
updates use that same executable. Embedded rooms remain the offline defaults.
To replace an embedded example, use its launcher ID (for example
`example.fes-cores`) in the external pack's manifest.

A development loop on the client is:

```sh
fogcast-tenfoot room-install --rooms /path/to/rooms /path/to/my-room
# Reopen Rooms and select the room; then edit the source Lua/artwork.
fogcast-tenfoot room-install --rooms /path/to/rooms /path/to/my-room
# To recover an update that fails only during a later interaction:
fogcast-tenfoot room-rollback --rooms /path/to/rooms my.room
```

## BIOS and media drives

**Settings → BIOS & media drives** in tenfoot selects either **This device**
(the kit or computer running tenfoot) or **FogCast host** (the API server).
Both locations can discover USB filesystems, remember a drive by filesystem
UUID, and add an SMB share as `//server/share` (also `smb://server/share`).
SMB accepts an optional username, domain and password; an empty username uses
Guest. A nonstandard SMB port can be included as `//server:port/share`.
Connections and separate BIOS/media start folders are saved on the machine
that owns the drive. They survive launcher and host restarts. The kit keeps
local SMB passwords in memory because its `drives.json` is on FAT; after
tenfoot restarts, enter those passwords again. Host-side SMB passwords remain
in the host's private configuration directory. Disconnected saved drives
remain listed; opening them again reconnects or reports an error.

The browser's Settings has the host drive manager. **From host drive** in
FPGA library setup and media import browses those same host drives. A browser
cannot inspect a kit's locally attached USB devices; use the kit's tenfoot
settings for those. Tenfoot's BIOS, Systems ROM/media, expansion, next-start
cassette and live tape/disk selectors all include saved local and host drives.
USB discovery and mounting use Linux block-device tools and are available on
Linux kits and hosts. Other Tenfoot platforms can use SMB shares but do not
discover or mount local USB volumes. Import continues through the existing
media store and explicit BIOS/media selection APIs. Selecting a drive or
folder never changes a running core. Drive inventory refreshes can arrive while
a root picker is open; they preserve the highlighted row so a pending Select
cannot be redirected to another action.

`storage` owns connection settings, USB discovery, rooted enumeration and
bounded regular-file reads. SMB2/3 is read directly with the Go SMB client;
it needs neither a kernel mount nor root privileges. Each operation owns its
connection, and cancellation closes the socket. USB discovery filters sysfs
through `appliance/blockprobe` before querying only usable USB devices with
`lsblk`; internal SD/system disks and optical or not-ready devices are excluded.
An existing USB mount is reused only when its current block-device identity
matches the discovered volume. A stale kit mount is unmounted before the
reconnected volume is mounted read-only with `nosuid,nodev,noexec`; desktop
users use `udisksctl` without interactive authorization. FAT, exFAT and ext4
use the kit kernel; NTFS uses `ntfs-3g`. Desktop Linux hosts need `lsblk` and
an existing mount or desktop device permission.

The host composes `storage.Manager` with `host-drives.json` beside its canonical
configuration; tenfoot keeps its local `drives.json` beside `tenfoot.json` in
its existing persistent configuration directory. Host drives are household
resources shared by browser Settings and authenticated paired launchers; they
are not private to one kit. Every paired operation still requires the
configured bearer and matching target identity. The host atomically replaces a
0600 file that includes credentials; inventory and save responses expose only
`password_set`. Blank password plus `password_set:true` preserves a stored
password only while the SMB destination and account stay unchanged; clearing
it is explicit. Tenfoot writes local drive metadata without passwords and holds
local SMB passwords only for its process lifetime. When it reads an older
`drives.json` containing a password, it removes that field atomically and keeps
the value only in memory until tenfoot exits. This protects the secret from
plain FAT file access but requires re-entry after a restart. The local and host
stores remain independent of library settings and target selection.

The paired launcher admits only the explicit `/api/v1/storage/drives` CRUD,
drive-relative `entries`/`file` operations, and the existing explicit household
firmware selection. A paired launcher may manage and browse these household
host drives by design; it cannot address another kit's private local storage.
The Coleco firmware selection is likewise a single household slot, updated
only through the explicit BIOS selection flow, not a per-target setting. API
reads cannot address arbitrary host paths: USB reads use `os.Root`, reject
escaping paths and nonregular files, and ordinary reads are limited to 32 MiB.
Expansion archive reads use their declared archive bound. SMB reads stay
relative to the selected share. Directory and connection work runs outside the
tenfoot app lock. Existing game/session/package admission and captured
live-media identity checks remain in force after import.
To transfer a pack as one file, export it on the source machine and install the
archive on the launcher's machine:

```sh
fogcast-tenfoot room-export --output my-room.room.zip /path/to/my-room
fogcast-tenfoot room-install --rooms /path/to/rooms my-room.room.zip
```
