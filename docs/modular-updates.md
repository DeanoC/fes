# Modular host, kit and core updates

FES is moving toward independently installed cores and kit software above a
stable appliance OS. This guide distinguishes implemented foundations from
remaining work. The existing [appliance update](appliance-releases.md) remains
the installed-system update path; the bootstrap and its recovery policy are
unchanged.

## Implemented foundation

- Host diagnostics accept independently specified full host, agent and runtime
  revisions. Exact identities are still recorded and checked. API/ABI admission
  decides compatibility, and diagnostic host labels still require opt-in.
- Kit swap/restore transactions use target-enforced maintenance reservations,
  which fence ordinary execution across agent restart on the same boot.
- Temporary diagnostic ownership and installed artifact provenance remain
  distinct from execution ownership. A workstation marker cannot reserve a kit
  against another host.
- Shared operator hosts remain available; branch diagnostics use private hosts,
  configurations, catalogs and ports. Cleanup affects the diagnostic owner's
  resources rather than resetting the shared environment.

These paths have software tests. They do not establish physical kit acceptance.
The first adoption requires an authorized image update containing the supporting
agent. There is no fallback to file swaps outside target admission.

## Planned update boundaries

| Layer | Planned update operation | Activation boundary |
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

Reuse the current package reader and immutable identity model. Installation must
not activate a package or change a running selection. Resolve installed packages
across the factory read-only inventory and a writable durable inventory; the
launcher, mesh inventory and runtime admission must observe the same selection.

The host package store requires POSIX permissions and hardlinks. FAT storage
cannot directly provide those guarantees. Choose an explicit ext4-backed store
and admission policy before reusing that implementation on the kit; do not infer
that every card already has FESDATA3. ROMs and parts remain separately identified
objects, and upgrades must respect existing persistence layouts.

## Persistent userspace releases

Build and publish a closed software artifact with board, architecture, base-system
requirements, provided contracts and executable digests. Store it outside the
base image and select it atomically under maintenance. Successful accepted
updates remain selected. Temporary diagnostics remain explicit and restore only
their own overlay changes. Health should report base-image and active-software
identities separately rather than represent a substituted binary as image stock.

The lifecycle must reject stale completion and selection requests. Recovery is
scoped to the failed release or selection; it must not restore an unrelated
host, core library or newer deployment. FPGA transitions remain runtime-owned.

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
