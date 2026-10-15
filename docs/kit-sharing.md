# Sharing a development kit

## Development host protocol

Keep the operator-selected shared host and its autostart setting unchanged.
Coding, builds, simulations and package preparation do not require stopping it.
For automated diagnostics use a private host with its own binary, configuration,
port, catalog and evidence directory through the [package acceptance
lane](package-acceptance.md). Do not replace the shared host or mount its live
library to test a branch. Stop only the private host you started.

Several idle hosts may inspect the same kit. The target lease admits one
execution owner; a competing launch must report busy without stopping another
session. Do not run two controllers for the same owned session. Keep the normal
menu available between probes and during long builds. If a diagnostic requires
changing the launcher, record its configuration and restore that temporary
change after testing; do not disable the shared host as routine cleanup.

The target lease is sufficient coordination for ordinary kit use: loading a
core, supplying media/input, collecting diagnostics and stopping the session.
No separate Slack reservation or acknowledgement is required for those operations.

Coordinate disruptive maintenance such as replacing/restarting shared services,
rebooting or changing the installed image. Coordinate the managing service, not
just its PID: restart policies can undo a process kill. Slack is not a push
notification channel for this harness; poll a thread when a reply is needed.
Coordination does not replace the existing lease or authorize another device.

Managed kit software is installed as an [immutable bundle](kit-software-releases.md)
and activated at the next released idle boundary. Use private hosts for branch
diagnostics. Managed images reject temporary binary/configuration swaps and
stock restoration; selecting an earlier bundle is an explicit operation against
the current generation. The stable supervisor owns the complete service group.

On an unmanaged maintenance-aware image, replacing binaries or configuration
uses the [kit runtime manifest](kit-manifest.md) commands. Each swap/restore acquires a
target maintenance reservation before changing files or stopping services,
retains it across agent restart, and ends it only after idle cleanup succeeds.
Workstation hold markers record a diagnostic task; they cannot reserve hardware
against another host. Release an ordinary kit.py session before starting a
swap/restore; the tool claims its own grant and promotes it into maintenance.

An agent that does not provide `fes-kit-maintenance-v1` cannot admit this workflow.
Install the supporting agent once through the existing authorized appliance-image
procedure. The tools reject missing maintenance routes and refuse replacement
agents that cannot enforce the reservation; there is no SSH-only fallback.

Host, agent and runtime revisions may differ when their advertised contracts are
supported. Record each exact artifact independently. A new core using a supported
ABI uses package import/selection/launch rather than a kit-file swap or OS update.
An accepted appliance update remains installed; unmanaged temporary overlay diagnostics
retain their explicit restore procedure. Managed software releases stay installed.

## Claim a development session

From `sources/misteross`, using your private FogCast configuration:

```sh
python3 scripts/kit.py --config /absolute/path/config.toml status
python3 scripts/kit.py --config /absolute/path/config.toml session \
  --owner agent-name --purpose 'OSS bring-up'
```

The session accepts these commands on stdin:

```text
load /absolute/path/to/core.rbf
stop
status
load /absolute/path/to/next-core.rbf
release
```

`stop` returns hardware to idle and retains ownership. `release`, EOF, or normal
process exit attempts cleanup and release. Repeat uploads in the same session;
compiling does not claim the kit. The CLI streams files through the existing
FogCast development-RBF endpoint; it does not change artifact/build recipes.

The normal FogCast host shares one lease between launches and remote input.
Explicit user Stop (empty-body `POST /api/v1/session/stop`) releases it after
cleanup. Sofa Soft-stop (tenfoot now-playing B, with `retain_lease: true`)
keeps the grant after that cleanup. Stop used when replacing a game retains
it. A client without ownership cannot claim merely to Stop another
session. Losing renewal disables that client's mutations rather than taking
the kit back automatically; restart the client after resolving ownership.

## Expiry and operator override

A lease lasts **90 seconds** and clients renew every **20 seconds**. If an agent
crashes, disappears or stops renewing, the target revokes the lease. Its input
streams close, admitted operations finish, and input/hardware cleanup runs
before another owner can claim. The target uses monotonic time; relative expiry
in responses handles kits without a working real-time clock.

For an agent that remains alive and keeps renewing, or after fixing failed
cleanup, inspect status and explicitly take over using its current generation:

```sh
python3 scripts/kit.py --config /absolute/path/config.toml status
python3 scripts/kit.py --config /absolute/path/config.toml takeover \
  --owner operator-name --purpose 'recover kit for development' \
  --expected-generation GENERATION_FROM_STATUS \
  --reason 'previous agent is no longer responding'
```

Takeover uses the existing configured bearer credential and enters the same
interactive session. It never interrupts FPGA programming midway. A changed
generation rejects a stale takeover request; inspect again instead of blindly
forcing it. Delayed Stop, release and input requests from the previous owner
cannot affect the replacement lease.

Cleanup failure leaves status `blocked` with a recovery reason. Repair the
underlying problem, then use explicit takeover to retry cleanup. Reboot remains
an operator maintenance option for the designated disposable kit. Agent restart
invalidates old credentials and reconciles/cleans hardware before accepting a
new claim. An abandoned takeover request does not reserve a successfully
cleaned, free kit forever.

## API and boundaries

All routes use the existing bearer authentication:

| Operation | Target route |
| --- | --- |
| Public ownership status | `GET /v1/kit/lease` |
| Claim with owner, purpose and secret request ID | `POST /v1/kit/claim` |
| Renew / release current credential | `POST /v1/kit/renew`, `POST /v1/kit/release` |
| Explicit generation/reason takeover | `POST /v1/kit/takeover` |
| Maintenance status | `GET /v1/kit/maintenance` |
| Promote an owned grant into maintenance | `POST /v1/kit/maintenance/start` |
| Resume/end the exact reservation | `POST /v1/kit/maintenance/resume`, `POST /v1/kit/maintenance/end` |

Mutation requests carry `X-FogCast-Kit-Lease`. Status reports owner, purpose,
generation, remaining milliseconds and reason; credentials are not included.
Claim/takeover retries use the same random request ID to reconcile a lost
response. Cleanup may still be running when release returns `revoking`. Maintenance
reservations have separate operation/generation identities and survive agent
restarts on the same boot. Ordinary claim, takeover and mutation are refused
until matching maintenance completion has confirmed idle. Failed maintenance
stays reserved for explicit repair; it does not expire into another owner's play.

Lease admission covers game launches, development uploads/reboots, Stop, input
attach/detach/streams, session display open/close, cast start/stop, and mesh content pull and link. Mesh
content pull acquires the session grant: a fresh session's first pull claims a
free kit, and a kit held by another session fails closed. Mesh content link
requires that grant. A grant claimed for that ensure is released when Ensure
does not start execution. A grant the session already held, including after
Soft-stop, stays held. Node, slot, and source reads stay available to other
clients, as do status and content-cache operations. A batch slot read is
`GET /v1/mesh/content/slots`. Revocation interrupts stalled HTTP uploads
and input streams, while the existing coordinator retains already-dispatched
physical operations until completion.

Direct SSH/Main FIFO programming, JTAG and unrestricted runtime-socket access
bypass agent enforcement. `make program` is explicitly a maintenance bypass.
Use `kit.py` for ordinary native bring-up and coordinate direct maintenance
with the current owner. A workstation file lock cannot enforce sharing across
hosts; no second lock service or misteross ownership database is introduced.

## Validation

The state-machine, HTTP, input, agent, host and public session tests include
concurrent claims, expiry with an admitted operation, stale cleanup/takeover,
failed cleanup, retained input neutralization, stalled uploads, real TCP input
stream closure, shutdown ordering, and unsynchronised target clocks. The Python
client tests exercise actual local HTTP transfers, renewal loss and takeover.

On the designated kit, two development loads retained one owner across Stop.
Competing claims, unowned/foreign Stop, input and upload requests were rejected.
Operator takeover cleaned an active development core; delayed old-owner Stop
and release were rejected. An abandoned Pong lease expired after 90 seconds,
returned hardware to idle and allowed a new claim without rebooting. The host
then passed automatic renewal and Pong/Stop/SNES/Stop/Pong/Stop, releasing each
lease. The normal host service also passed Pong launch/Stop after installation.
A real CLI session loaded a development RBF, stopped it and released ownership.

Exact diagnostic binaries, source archives, test logs and hardware results are
retained in `out/kit-lease-diagnostic/`. Hardware classification is diagnostic;
it does not select or publish parent component revisions.

The installed target diagnostic image SHA-256 is
`80f6d37dfcae4c05dc615367200352defe1df3900e1e4b20e8415e0df38a316d`.
The final host binary SHA-256 is
`7004094ae2bb74a7f4c70259b684f78fcd5f42203140d391dd71b114e4e6ae39`.
The initial and reviewed host binaries in the evidence directory were superseded
by `fogcast-api-final`; `host-final.json` identifies the tested source snapshot.
The previous installed executable is preserved as
`out/kit-lease-diagnostic/previous-installed-fogcast-api`.
