# Modular host, kit and core updates

FES is moving toward independently installed cores and kit software above a
stable appliance OS. This guide distinguishes implemented foundations from
remaining work. The existing [appliance update](appliance-releases.md) remains
the installed-system update path; the bootstrap and its recovery policy are
unchanged.

The [forward plan](plans/forward-plan.md) tracks the remaining OS, software,
core and room installer separation. The default image now includes only
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

## Remaining installer separation

1. **Separate parent regressions from product lanes.** Select and balance
   parent Python and host Go tests from package/import dependencies and measured
   timings while retaining a full scheduled run, targeting ordinary room/host
   PR feedback within five minutes (measured p50/p95 before claiming the target).
2. **Make OS building consume installed artifacts.** `make image` consumes an
   explicit prebuilt bootstrap artifact set ([assembly inputs](image-artifacts.md))
   while `make build`/`make dev` remain explicit source integration. Offline
   first boot and an empty playable-core inventory still need kit evidence.
3. **Separate UI engine and room delivery.** The room installer provides
   bounded validation, immutable snapshots, atomic selection, live reopen and
   rollback, and first-party packs declare public `example.*` IDs so rooms can
   update without compiling Go, restarting the agent or rebuilding MENU.
   Embedded snapshots remain offline recovery defaults; remote room catalog
   distribution and end-to-end kit install/reopen evidence remain open (see
   [room installation](../sources/FogCast/docs/rooms.md#installing-and-opening-rooms)).
4. **Finish installer dependency handling.** Install a chosen core with its
   matching video/expansion packages, surface download/install/ready states and
   missing user-supplied firmware, and ensure cancel/retry preserves the
   previous working selection and saved data.
5. **Separate development images from release certification throughout.**
   Trace bootstrap/export/receipt consumers that still impose release
   reproducibility assumptions, switch routine development to one-pass builds
   with explicit release certification, and remove the old path in the same
   cutover.

End-to-end acceptance for this separation is a clean boot with no playable
cores, installing and playing one core, updating a room without rebuilding
software, updating software without building cores, and recovering a failed
installation.

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
