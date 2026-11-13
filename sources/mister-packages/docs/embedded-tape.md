# Experimental stepped initial tape handoff

[`fes.fabric.embedded.tape`](../packages/fabric/fes_fabric_embedded_tape.yaml)
defines the private channel between native publication and a secure initial-tape
service. It generates C and Go constants. Call `0xc2000304` is an unused private
experiment number, not a vendor allocation. The owned Nano SiP export includes
the service, with an unready default. The explicit `FES_NANO_INITIAL_TAPE` build
flag connects the strong supplier only in a complete shared-USB/SD/held-ROM
computer composition. Explicit SDK `--initial-tape` selection binds that flag
through native, entry and cold receipts and selects the board CPU wrapper.
The linked request and generated board constant must agree. The firmware
selection additionally requires the complete existing OCRAM exclusion; default
builds leave the service unselected.

The trusted platform supplies the fixed 8 MiB arena at `0x94800000`, with the
descriptor in its final page at `0x94fff000`. The payload begins at the arena
base and contains 1–16375 bytes. A completed configuration lease is required
before publication; a tape lease cannot be overwritten until successful
completion. A subsequent tape requires a new completed configuration and a
newer generation under the same boot owner. Uncertain outcomes retain memory
and retire the arena. These software rules supply no physical reservation.

The 128-byte little-endian descriptor is:

| Offset | Bytes | Meaning |
| --- | --- | --- |
| 0 | 4 | Magic `0x32505446` |
| 4 | 4 | Version 2 |
| 8 | 4 | Header size 128 |
| 12 | 4 | Payload byte count |
| 16 | 8 | Boot owner |
| 24 | 8 | Active bridge generation |
| 32 | 4 | Acknowledged initial Hold GPO |
| 36 | 4 | Payload IEEE CRC-32 |
| 40 | 16 | Build ID in manifest byte order |
| 56 | 48 | Payload SHA-384 |
| 104 | 24 | Zero, reserved |

The version-2 request registers are `[fid, operation, owner, cookie,
generation, 0, 0]`, with cookie `0x46455354`. They carry no address, timeout
or selected identity. `Load=0` begins exactly one transfer. Each pending reply
supplies the next ordinal; resume uses `Step | (ordinal << 8)`, with `Step=1`.
`Abort=2` uses the same next ordinal and retires without supplier I/O. Wrong,
repeated or foreign ordinals do not touch the supplier or consume another step.
An issued call with an uncertain result is never retried.

Each call runs under the existing SiP lifecycle lock. Preparation verifies the
fixed descriptor, private accepted identity and source digest. Each subsequent
call rereads that same descriptor and verifies the whole source before any GP
operation. Another digest and fresh ownership check frame each successful
return. No source pointer or supplier frame survives a call; the trusted service
retains its cursor and CRC. Every CSR keeps its fresh before/after CPU, USB,
reset and ownership checks. The emulated CPU stays held throughout preparation,
all chunks and final verification.

A pending reply is `[Pending=6, (tag<<32)|verified_bytes, generation,
next_ordinal]`. It is progress, not a completion receipt or permission to Run.
One call prepares, one call transfers each <=512-byte chunk, and one commits
and checks final identity/readiness/unchanged fetch state. Final success is
`[OK=0, (tag<<32)|bytes, generation, (crc<<32)|finalGPO]`, with tag `0x46545032`.
Failures publish only the tag, and acknowledged cancellation is status7.
The new magic/version/tag reject a mixed old caller/firmware chain.

Each firmware call has a one-second timer bound, including its source and
ownership checks. Total elapsed time, including time between calls, is bounded
by `ceil(bytes/512)+2` seconds: at most34s for16375bytes. Qualified timer samples
remain monotonic across service/engine and call boundaries; a request cannot
supply or extend them. Every GP acknowledgement also retains its64-poll bound.
These are cooperative checks around callbacks with bounded return, not a way
to interrupt a hardware access or SMC that never returns.

The native caller yields between pending replies and can cancel with one
ordinal-bound abort. Its media budget is `ceil(bytes/512)+4` platform seconds,
allowing publication and final observation overhead (at most36s on Nano).
The runtime independently preserves its one-second control-progress allowance
around Hold/held samples/Run/fetch progress, excluding only the separately
bounded upload. Tape-enabled fetch diagnostics carry explicit byte count,
media/control durations and their derived total limit; serial validators bind
that count to the selected retained media. Non-tape control diagnostics keep
their existing one-second contract. Normal per-word Go media operations also
retain an independent one-second acknowledgement deadline inside their size
budget. No receipt is published until every step and final check succeeds.

Portable tests join the actual Go arena/descriptor codec to the actual C
service and engine with an injected endpoint, ownership and digest supplier.
Separate captured-RTL tests cover the engine and native launch/return protocol.
Joined strong-provider tests execute the actual secure dispatcher, cold/CPU/USB
callbacks, pinned SHA and engine with modeled registers through core/menu
load/return, failure and no-replay paths. Neither establishes physical timing,
a matched boot/runtime selection or board acceptance.

The board wrapper reuses the completed program arena and issues only the fixed
service call. Full live mapping observations frame that memory-consuming SMC;
the publication lease also checks copied bytes and mapping before release.
A selected uncertain outcome never falls back to ordinary per-word delivery.
The firmware emits one cached failure-counter record after containment, without
reading hardware or printing media payloads during that report.
