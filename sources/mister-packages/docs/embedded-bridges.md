# Experimental owned firmware bridge channel

`fes.fabric.embedded.bridges` 1.5 supplies generated C/Go constants for the
Linux-free Nano firmware experiment. It adds no programming profile, package
ABI, register mapping or production compatibility entry. The FPGA endpoint
contract and its generated inputs are unchanged.

The private SiP call `0xc2000301` uses an opt-in `x3=0x46455342` cookie.
The number is unused in the inspected pins and outside their v2/v3 ranges.
This is a local experimental extension, not an assigned vendor API. It does
not probe the older mutable `HPS_SET_BRIDGES` service as a query.
`x1` is the operation, `x2` the expected boot-scoped owner and `x4` the expected
generation. Snapshot uses zero owner/generation. Close additionally carries
request bits 8 (require fabric handshake) and 9 (retain continuously held data).
Control and Release open exactly the predefined control and remaining masks.
There is no initialization or boot-proof operation callable from non-secure code.

Replies have status in `x0`, tag `0x46424f31` above the flags in `x1`, owner
in `x2` and generation in `x3`. Status 5 means the recognized service lacks
trusted boot/platform initialization; it carries the tag with zero flags,
owner and generation. Other nonzero statuses carry no successful state.

| Flags | Meaning |
| --- | --- |
| 3:0 | Reset mask |
| 5:4 | Fabric warning capability: 1 absent, 2 present; zero rejects |
| 6 | Trusted never-enabled boot evidence |
| 7 | All relevant masters contained |
| 8 | Producers quiescent for a completed close |
| 9 | Fresh fabric acknowledgement for an active close |
| 13:10 | Newly drained hardware mask |
| 14 | Data stayed contained across this owner's control-only access |

Only Close returns bits 8 through 14. Held-data close returns no fresh fabric
or hardware-drain claims. Snapshot/Control/Release reject extra response bits.
Reserved request bits, response bits, invalid operations and missing/mismatched
owner/generation reject. The marker is not firmware authentication. Qualified
boot identity, platform ownership and physical validation remain required.

Operations 4/5 read the two halves of an optional source-context SHA-256.
They use the same cookie and expected owner/generation. Each reply carries
status in `x0`, `0x46484331 << 32 | part` in `x1` (part 0/1), and 16 digest
bytes in `x2/x3`, each packed most-significant byte first. Failure returns
zero digest words. Status 1 means configuration or a device lease is active;
2 means a bad operation/scope; 4 means closed containment was lost; 5 means
the trusted provider or compiled digest is absent. Readers bracket both
halves with identical successful Snapshots; partial or changed observations
reject. These reads never change generation or bridge state.

The source context binds the sealed HPS-first FPGA package before native SPL
construction, avoiding a self-referential SPL hash. Its digest is compiled
into the matched secure firmware and native image. It is not live FPGA
identity, measured boot, CPU mapping qualification or programming admission.
The contained `bootcontext`, `fpgacontext` and `fpgastatus` diagnostics select it. The default
provider returns NOT_READY, and the cold dispatcher still denies generic bridge
mutations and configuration. The fabric warning capability describes the
optional warning handshake, not the presence of a configured FPGA core.

Operation 6 is a separate one-shot secure identity probe, available only in a
matched source-bound cold chain (`fpgacontext` or `fpgaload`). Its fixed CPU aperture, ABI and build ID come
from the sealed project's compiler handoff and are compiled into secure
firmware. The request contains no address or FPGA write. It requires the
current owner/generation, never-enabled evidence, contained masters and
closed bridges. Before opening it performs one read-only SDM `CONFIG_STATUS`
query, requires the complete six-word reply, state zero, NSTATUS high, both
CONF_DONE and INIT_DONE, and no SEU fault. It rechecks closed ownership after
the query. Not-ready fabric returns status 5 without opening control, accessing
the endpoint or changing bridge generation; the one attempt is consumed.
Malformed/failed queries retire authority. This never calls the legacy
`FPGA_CONFIG_ISDONE` frontend or sends a configuration command.
Under the owned lock it opens only control (reset mask `0xd`,
the next generation), reads two complete identity/idle snapshots, compares them with
the compiled target, and closes control (mask `0xf`, generation 3). Data
bridges remain reset and secondary CPU release remains denied throughout.
The pinned platform requires EL3 translation and data cache disabled; an
unsupported CPU context rejects without changing that context.

On success `x0=0`, `x1=0x46454931 << 32 | ABI`, and `x2/x3` contain the full
128-bit observed build ID, each packed most-significant word first. The
reader checks a final owned Snapshot at generation 3. An in-flight failure returns no
identity words, attempts to assert only control reset, and retires this boot's
probe and ownership. It preserves both the first error and cleanup result and
never retries the read. Invalid requests do not consume a valid probe.
This operation supplies no active data drain, HPS I/O measurement, SDM
programming permission or production core admission. Ordinary cold builds
have no probe provider; operations 1/2/3 remain unavailable from non-secure
code in every cold diagnostic.

Status 5 alone does not distinguish an absent probe provider from unconfigured
fabric. Native code rechecks its original closed source ownership before
reporting `FES_NATIVE_FPGA_UNREADY CLOSED`. The hardware readiness diagnostic
additionally requires the saved complete SDM response. It accepts no FPGA
identity or programming authority. A CPU abort or hung MMIO/SMC can prevent
error-return cleanup; the host timeout supplies no proof of control reclose.

Operation 7 queries configuration readiness without an endpoint access or
control opening. It requires the same current owner/generation, initial
never-enabled scope and contained masters as the identity probe. The secure
provider issues only fixed `CONFIG_STATUS`, checks the complete response and
rechecks unchanged closed ownership. Successful replies carry `x0=0`,
`x1=0x46435331 << 32 | ready` (ready exactly 0/1), `x2=owner`, `x3=generation`.
Not-ready fabric is a successful observation with ready zero. Provider absence,
busy, invalid scope and errors return no successful state. The query is
one-shot per boot and does not consume the identity attempt; a later identity
operation always queries readiness again. A failed query retires authority
and may assert control reset for containment, without opening any bridge.

The `fpgastatus` native diagnostic uses operation 7 and verifies an identical
closed Snapshot afterwards. It never invokes operation 6. Its completion
reports SDM readiness only, regardless of whether fabric is ready; neither
branch supplies live endpoint identity, HPS I/O measurement or admission.

Operation 8 prepares one source-bound cold configuration under the existing
owner lock. It takes the expected owner and generation through the same
private cookie channel. A qualified secure provider requires the current never-enabled generation,
never-enabled history, closed bridges, contained masters and no device lease.
It checks the fixed staged whole image against a compiled digest before
returning `x0=0`, `x1=0x46435031 << 32 | core_bytes`, `x2=owner`, `x3=generation`.
No address, bytes, digest or memory permission comes from the request.
Default providers return NOT_READY. Other cold profiles still reject the four
configuration operations.

The prepared provider gates the existing START, WRITE, COMPLETED_WRITE and
ISDONE operations, under the same lock and with every bridge kept closed.
Only its fixed memory region and exact sequential chunks are accepted. Its
trusted digest callback checks the whole image before START and after final
status, and each issued chunk before WRITE and across asynchronous completion.
Accepted BUSY writes are never submitted again. Completions must name exactly
the outstanding SDM-relative address. Unexpected replies, lost scope or changed
bytes retire authority and retain the pending memory until reboot. Bridge
Snapshots remain available; context, identity, SD and configuration-status
operations cannot interleave while the transfer is prepared or running.
Preparation is consumed even on a failed check; configuration completes only
once per cold boot. It supplies neither active drain nor runtime activation.


Operation 9 irreversibly retires a failed caller's still-running cold
configuration before one bounded same-client `RECONFIG_STATUS` diagnostic.
It requires the original owner and prepared generation, a prepared and started transfer
with pending bytes, closed bridges, contained masters, no device lease and no
in-flight firmware call. Rejected scope issues no mailbox command. Retirement
sets both configuration and bridge authority failed while keeping all byte
counters and DMA descriptors unchanged. It does not cancel or restart the SDM.
Every later configuration, identity or bridge mutation remains rejected.

The diagnostic sends one fixed command with the HPS client and reserved job 15.
Its separate reader requires idle local parser/descriptors and an empty command
FIFO, has six response words, a 64-word FIFO budget and at most 10,000 polls
with 100-microsecond waits. It accepts only the matching six-word reply. Late
zero-word asynchronous replies can be consumed after retirement without
acknowledging or releasing their buffers. Foreign packets, malformed lengths,
partial replies, busy state and timeouts remain observations of failure.
There is no resend, mailbox restart, configuration completion or bridge opening.

A retirement acknowledgement carries `x0=0`,
`x1=0x46435231 << 32 | uint32(diagnostic_status)`, `x2=owner`, and
`x3=generation`. The low status is a signed nonpositive transport result; zero
means the diagnostic reply was collected, regardless of its reported FPGA
state. The six raw SDM words are serial diagnostic records. A missing provider
returns status 5 with zero owner/generation; rejected requests return no
retirement acknowledgement. The caller must keep all source and DMA storage
until reboot even after a successful acknowledgement or a ready status.
This supplies no configuration, drain, activation or runtime-admission proof.

The version-1.6 cold storage handoff preserves the boot owner while secure SD
resume/prepare/stop transitions advance the closed generation. Configuration
initializes at generation 1, then captures the current contained generation
once during preparation (generation 4 after one SD cycle). Every subsequent
configuration call and retirement requires that exact captured generation.
Active device leases, stale requests, opened bridges and changed owner/context
reject. The identity query/probe likewise uses the current never-enabled
closed generation; its control-only open/reclose advances it by two. This
extension changes no wire constants and grants no active-fabric storage return,
runtime admission, data release or memory reclaim authority.
