# Nano firmware bridge service prototype

Base: `0014b35d89c41d010a06b44c59d2934c897f2c19`. This continues the
[CPU platform contract](../2026-10-31-de25-bridge-contract/README.md) with a
freestanding C service and source-bound vendor-driver tests. No board, UART,
JTAG or physical card was accessed. The selected physical boot chain and its
stage-3 rejection remain unchanged.

## Implemented provider

The service requires trusted, serialized, bounded-return operations for owner
scope, reset readback, master containment, producer stop and raw bridge
enable/disable. Boot code supplies the owner, external-change epoch and
qualified optional-handshake capability. A never-enabled assertion is accepted
only at initialization with every bridge and master contained. It is not an
SMC argument, package field or reset-bit inference. There is no default proof.

Each operation checks the current owner scope, prior generation and stable
reset/master state. It verifies ownership again after producer stop before
calling the driver, then verifies the completed mutation before publishing its
next generation. Scope loss, reset drift or uncertain operations retire the
service. A separate configuration guard verifies an owned closed state without
sending another bridge shutdown request.

The prototype uses the exact pinned Terasic driver function bodies and register
definitions. Its opt-in transformation gates only the optional fabric warning
signals and polls, for both enable and disable. It preserves the required
hardware fence/drain sequence, time budgets and first-error propagation.

Control-only recovery now carries an explicit retained-containment result:
data paths stayed reset under this owner, so only the control bridge closes.
The service returns zero new fabric/data ACK claims for that operation. The
Go hooks accept this only from their own verified control-only state. Active
data paths must obtain new hardware-drain evidence and the fabric ACK when
supported. Unknown initial partial resets remain rejected.

## Checks and evidence

| Check | Result and boundary |
| --- | --- |
| Standalone C service | 32 cases with undefined-behavior checks; all callbacks simulated |
| Actual vendor-driver/register model | 18 cases, both optional-handshake capabilities, cold/active/recovery/failures; register/poll replies injected |
| Freestanding AArch64 build | Service object compiled and ELF machine checked; not linked into BL31 or booted |
| Native integration | 13 C-service/TF-A/RTL lifecycle cases with race detection, including launch/return, missing ACKs, stop failure, reset mismatch, ownership loss, control/data-open failure, identity recovery and lost transfers |
| Tenfoot integration | Actual UI actor launch/return and identity recovery; hardware/fabric/ownership failures block a second programming attempt |

The active shutdown model executes six polls with the fabric responder and
four without it. All four mandatory hardware polls remain. The closed
configuration guard performs no reset writes. Control-only recovery reports
continuous data containment, rather than labeling prior closure as a new ACK.
Failed enable can have partially changed register state; the service rejects
the result and requires reboot, without asserting rollback.

The initial combined build found a C/C++ linkage declaration error; it was
corrected and the complete run passed. An initial cross build lacked the
retained toolchain's library environment; the recorded retry used that
environment and passed. Failed logs are preserved with the final evidence.
The new service uses canonical synthetic packages in the actor tests. The
artifact-dependent sealed Nano tests are not selected for that service; this
does not requalify a sealed package or a physical programming profile.

[evidence.json](evidence.json) records exact sources, results and limits.
The ordinary Terasic contained service/reset source bytes were compared with
the base revision and are unchanged. The preparer source itself has changed,
so previous producer-bound boot receipts require a fresh build before reuse.

## Remaining integration

This service is unbound. Only the private host test transport connects it to
prepared TF-A START and the native `BridgeServices` hooks. No real SiP ABI or
dispatcher, board address mapping, ownership-monitor callback, producer-stop
implementation or trusted boot-history proof was installed. It does not prove
the reference SOF's internal handshake wiring or physical FIFO/CDC drain.
There is no new physical programming profile or compatibility registry.

The next step is the controlled BL31/boot integration: establish the cold-boot
proof, route every bridge/configuration mutation through this ownership state,
bind the actual platform operations and revalidate a freshly matched boot
chain. A board test needs those gates before this prototype can be selected.

## Replay

See the [firmware commands](../../../platform/embedded/firmware/README.md#controlled-nano-bridge-service).
The service's always-available callback tests run with:

```sh
python3 -m unittest discover -s tests -p test_nano_bridge_service.py -v
```

The opt-in integration requires the exact vendor checkout and pinned header
hashes; it writes only a fresh private `out/` directory and starts host test
processes. Its register and ownership callbacks do not access hardware.
