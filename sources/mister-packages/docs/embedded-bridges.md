# Experimental owned firmware bridge channel

`fes.fabric.embedded.bridges` 1.1 supplies generated C/Go constants for the
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
Only the dedicated contained `bootcontext` diagnostic selects it. The default
provider returns NOT_READY, and the cold dispatcher still denies all bridge
mutations and configuration. The fabric warning capability describes the
optional warning handshake, not the presence of a configured FPGA core.
