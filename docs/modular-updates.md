# Modular host, kit and core updates

FES is moving toward independently installed cores and kit software above a
stable appliance OS. This guide distinguishes implemented foundations from
remaining work. The existing [appliance update](appliance-releases.md) remains
the installed-system update path; the bootstrap and its recovery policy are
unchanged.

The [development-speed redesign](development-speed.md) sets the pre-release
direction and records remaining work. The default image now includes only
MENU display firmware and the locked idle RBF; playable cores are installed
independently. Room packs already load from an external directory, but their
installer and independent first-party distribution remain to be implemented.

## Implemented foundation

- Host diagnostics accept independently specified full host, agent and runtime
  revisions. Exact identities are still recorded and checked. API/ABI admission
  decides compatibility, and diagnostic host labels still require opt-in.
- Target-enforced maintenance reservations fence ordinary execution across an
  agent restart on the same boot. Managed kit-software changes use immutable
  userspace bundles.
- Shared operator hosts remain available; branch diagnostics use private hosts,
  configurations, catalogs and ports. Cleanup affects the diagnostic owner's
  resources rather than resetting the shared environment.

These paths have software tests. They do not establish physical kit acceptance.
The first adoption requires an authorized image update containing the supporting
agent. The host-side file-overlay claim/swap/restore workflow has been removed.

## Update boundaries

| Layer | Update operation | Activation boundary |
| --- | --- | --- |
| Base OS, kernel, drivers and foundational init | Verified appliance image | Protected reboot, infrequent |
| Kit agent/runtime and launcher | Immutable userspace release outside the OS image | Owned idle maintenance and affected service restart |
| Core, ROM, expansion and video packages | Immutable durable installation plus explicit version selection | Next owned launch, without service restart |
| Host software | Independently installed host artifact | That host's idle session boundary |

Initially the agent and runtime may ship as one userspace release because the
runtime response decoder remains strict. This does not require equal host
revisions or replacing the OS. A future wire-extension policy must preserve
strict identity and mutation checks, explicitly negotiate capabilities, and
reject unsupported contracts rather than guess older behavior.

## Durable core installation

[The durable core library](kit-core-library.md) stores verified immutable packages
on FESDATA3 and uses explicit generation-safe version selections. Installation
and selection do not restart services or replace a running core. The runtime
checks the selected artifact's ABI/profile at the next owned launch.

## Persistent userspace releases

[Kit software releases](kit-software-releases.md) provide a deterministic bundle
of agent, runtime and launchers above the base OS. The base supervisor owns
startup, durable queued activation and one bounded previous-bundle recovery.
Compatibility uses explicit base-system and interface contracts; revisions remain
independent provenance. The target waits for the execution lease to become free
before acquiring maintenance. It never takes over to make an update happen.

Kits take no file overlays. An accepted release remains selected;
ordinary diagnostic cleanup cannot restore an older release. Status identifies
the base image, active software and pending operation independently. Host-only
regressions do not establish physical acceptance of the bootstrap cutover.

## Legacy cutover and acceptance

Inventory active compatibility aliases, old core behaviors and contradictory
instructions against current producers. Convert the required artifacts and
configuration once, then remove the superseded implementations and instructions
together. Supported interface versions remain explicit contracts, not implicit
fallbacks. Retained immutable artifacts are recovery inputs, not alternate
production backends.

Before promoting the complete lifecycle, test two independent host revisions,
competing execution requests, service replacement during maintenance, stale
completion requests, and installed-core persistence across reboot without OS
replacement. Physical activation, HDMI/input/media checks and exact-artifact
acceptance remain separate from host regression results.
