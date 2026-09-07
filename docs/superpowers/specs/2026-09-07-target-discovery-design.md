# Target discovery and reconnection

Status: approved by the user on 2026-09-07; implementation completed; integration verification accompanies publication.

## Outcome and scope

A configured FogCast host finds its FES kit after an address change or reboot,
without editing an IP address or restarting the host. The existing browser and
tenfoot target views report disconnected, connecting, ready, active, busy, or
recovery-required state. Discovery never launches a game or takes over a lease.

This first milestone covers the existing local-network target and existing
credentials. Controller navigation improvements, new game systems, updates,
remote-network discovery, and automatic game resumption are separate work.

## Existing integration

Base: FES origin/main e1aca5f, selecting FogCast 5fc0b4a. The existing host target
configuration supplies name, address, enabled state and agent credential. Health
already reports a boot ID; this changes at reboot and cannot identify the kit.
The service serializes lifecycle operations and owns target clients and kit
leases. The target is the lease authority; its existing expiration and explicit
takeover rules remain authoritative.

The recent card boot demonstrated the gap: DHCP assigned a new address, requiring
a private configuration edit and restart of the persistent host API. The one-shot
CLI closes its service and releases its lease; acceptance uses the persistent API.

## Approach

Use mDNS/DNS-SD on the local link to advertise `_fogcast._tcp`, with an opaque
persistent target ID and discovery protocol version. Advertisements carry no
credentials, library information or lease secrets. The agent owns advertisement
startup and shutdown; multicast failure must not prevent its HTTP API starting.
Use a maintained Go DNS-SD implementation after checking its license, supported
platforms, cancellation and interface-change behavior during implementation.

A fixed hostname alone does not bind an endpoint to a specific configured kit.
Subnet scanning adds unrelated probes and scales poorly. Preserve explicit
address configuration as the fallback for networks that block multicast.

## Identity and provisioning

Add an optional target ID to the host target configuration and agent config, and
return that ID in authenticated health. IDs are random identifiers, never derived
from bearer tokens, hostnames, or boot IDs. They identify a provisioned target,
not a cryptographic hardware identity.

Persist an ID once per configured target using the existing private configuration
write path. FES automatic media provisioning copies that ID into agent.toml along
with the existing credential. Repeated assembly from unchanged inputs retains
the ID and identical configuration bytes. Media assembly itself must not silently
rewrite the host configuration or generate a new ID on each build.

The host offers an explicit preparation action through its target settings that
creates and saves an ID before media assembly. Existing address-only setups remain
usable without it. For an already running discovery-capable agent with a persistent
ID, a successful authenticated connection at its explicitly configured address can
bind that ID using the same private write path. Agents with neither configured nor
persisted ID create one once on writable persistent storage; failure leaves HTTP
available and discovery disabled with an actionable diagnostic.

Two live endpoints advertising one ID are ambiguous: do not choose one
automatically. Copying a provisioned card to another kit requires preparing a
distinct target ID. This milestone does not claim adversarial endpoint identity
verification beyond the project's existing local HTTP/token model.

## Resolution and lifecycle

The host tries its configured or last validated address first. After a read-only
connection failure it resolves only candidates matching the bound target ID,
then validates authenticated health, ID and protocol compatibility before
adopting an endpoint. Never send credentials to every discovered service. Do not
automatically enroll an unknown kit or fall back to another configured target.

Resolved addresses are runtime state; DHCP changes do not rewrite configuration.
Keep resolution cancellable, deduplicate concurrent lookups, and bound retry
backoff to 1, 2, 4, 8, then 15 seconds while a target is enabled. A successful
connection resets backoff. Disable, target change and service shutdown cancel
pending work. Changes to network interfaces cause discovery to refresh.

Integrate endpoint adoption with existing lifecycle serialization. All target
clients, lease renewal and input connections must agree on the adopted endpoint;
do not create an independent session coordinator. Discovery and status polling
use only read-only target operations and cannot acquire ownership.

On a changed boot ID, invalidate the host's previous lease/input/session handles
without sending stale cleanup requests to the new boot. Reconcile current status
and ownership before enabling a fresh user launch. On an unchanged boot, retain
ownership only if the existing lease can be validated; otherwise show the current
owner or recovery state. Never replay launch, upload, Stop, or input requests
whose outcome became ambiguous during a disconnect. Existing runtime status
reconciliation continues to determine physical state.

## User-facing behavior

Expose connection state separately from game state through the existing host
target API and use it in the browser and tenfoot target views. A disconnect must
not look like a successful Stop. Show busy ownership and cleanup failures using
existing public lease metadata. Ready means the target is reachable, reconciled,
and available for a fresh launch; active and busy are not collapsed into offline.

Retain manual address entry and provide a useful message when automatic lookup
cannot find the configured kit. Keep credentials out of responses and logs.

## Validation and acceptance

Tests exercise the real host service with controllable discovery and HTTP peers:
address changes, reboot IDs, multicast unavailability, wrong identity, duplicate
IDs, authentication rejection, cancellation, bounded retries, and concurrent
status/launch. Verify discovery never claims a lease, never replays mutations,
and cannot stop or take over another owner's session. Cover lease renewal and
input endpoint changes, not just HTTP health resolution.

Test configuration round trips and legacy loading, deterministic provisioning,
and absence of credentials in advertisements and public responses. Run affected
Go race tests and parent provisioning tests; cross-build the target agent.

For the designated kit, claim the existing lease for any deployment/reboot.
Use a diagnostic image first, preserving the current verified artifact. Record
boot ID and exact artifact identities. Prove host recovery after kit reboot and
an actual controlled address change without a host restart; launch visible Pong
through the persistent API, then Stop and confirm a free lease. Verify another
owner is reported as busy without takeover. An address-change simulation alone
does not establish the DHCP hardware acceptance claim.

Reuse existing FPGA outputs and compiler/base caches. Perform required complete
image verification when packaging/init or locked dependencies change; do not
recompile unchanged FPGA designs. Update FogCast's canonical architecture and
FES provisioning/usage guides with the resulting behavior.

## Delivery

Implement in separate FogCast and FES worktrees. Keep component pins unchanged
until tested component commits are available and publication is authorized.
Hand back focused test results, hardware evidence and outstanding limitations.
No commits, pushes or PRs are authorized by this design document itself.
