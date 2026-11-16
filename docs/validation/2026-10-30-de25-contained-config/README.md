# Contained configuration protocol and display drain model

FES-owned exploratory work on `codex/de25-embedded-os`, based on
`cfa314b571ee85974a1781fb60893868ca4622d2`. The result is the commit containing
this record. Evidence is host software/fault tests, native AArch64 QEMU checks
and Nano cross-compilation. No physical hardware, shared kit, GPU or Powerboat
process was used. No bootable Nano image or FPGA bitstream was produced.

## What changed

The existing fabric controller now refuses a generic success reply for display
drain. It issues the owned reader's display generation and requires both an
explicit quiescent acknowledgement and that exact generation before containment
or programming. Revoking a plane preserves the identity of its old reader.
Missing, partial or stale evidence retains the issued command and image
references, enters reboot-required, and prevents implicit idle recovery.
`CompleteModel` supplies matching acknowledgements only for software endpoints.
The boot-owned physical framebuffer still has no release/drain operation.

The SDM configuration model tracks the dedicated START, WRITE, COMPLETED_WRITE
and ISDONE protocol below that same controller. It retains private source and
issued buffers, permits at most four outstanding writes, counts accepted-BUSY
writes exactly once, and checks complete response batches before retiring any
addresses. Requests use CPU DDR addresses while completions use the reviewed
Agilex 5 SDM-relative namespace. Missing/foreign/duplicate addresses, wrong
namespace, mutated buffers, transport errors and expired/regressing clocks
retire the model while keeping its references. Late completions cannot revive it.
There is no transfer replay, bridge-release operation or physical binding.

The only allowed endpoint is explicitly hypothetical `HeldBridgeModel`.
Reference TF-A v2.12 and unknown firmware reject before START. The real Nano
profile continues to admit only the read-only mailbox probe; it cannot select
this hypothetical programming endpoint. A completed transfer is neither a live
identity observation nor permission to release bridges. The single fabric
controller still performs identity verification and release ordering.

The new software endpoint connects these pieces for full idle/game/idle cycles,
waits for its simulated accepted display reads to drain, keeps bridge containment
through identity verification, and retains a failed transfer object until the
model ends. Wrong identity permits only the controller's existing bounded idle
recovery. Unknown transfer ownership prevents release and further programming.
This adds no second lifecycle, production recovery coordinator or owner database.

## Source review and remaining physical work

The [SDM reference record](../../../platform/embedded/tamago/sdm/README.md)
documents exact TF-A v2.12 source identity and the additional Agilex 5 header.
WRITE subtracts DRAM_BASE before storing its buffer address; completions return
that stored address. The model now accounts for this translation explicitly.
The remapper registers are SDM backend registers: the previous general concern
about XGMAC was not evidence that configuration changes its DMA mapping.
Interactions with other masters need actual firmware/board review.

Source review also found START passing a null pointer to a helper which
dereferences that argument. That is a concern in this inspected reference, not
an executed failure or a claim about the delivered board's firmware. Combined
with the already observed automatic bridge-enable path, this reference is not
a usable contained programming profile.

The next integration step is to implement and validate the contained firmware
path and an FPGA identity/disable/drain endpoint, with a concrete address-space
and resource ownership contract. These Go generation fields are not a shared
hardware register layout. No matching RTL, secure firmware build, real memory
drain, configuration SMC execution or Nano boot is claimed. Physical activation
remains unavailable; production Linux runtime and shared ABI/schema consumers
are unchanged.

## Checks

- Entire embedded Go module with race detection and the retained-ROM/parts
  model fixtures: passed on the final sources.
- Configuration tests include 100 deterministic variable-chunk/completion
  schedules, accepted-BUSY accounting, partial completion batches, wrong
  namespace, immutable source ownership, asynchronous buffer mutation,
  stale/cross-transfer tokens, transport failures and deadlines.
- Native QEMU `fabric`: thirteen lifecycle checks plus six configuration checks
  passed. The latter cover rejected reference firmware, exact transferred bytes,
  contained launch/return, stale drain, unknown completion ownership and wrong
  identity before release. Controlled clocks are not hardware timing evidence.
- Native QEMU `play`: all eleven actual tenfoot launch/return/failure cases
  passed with the stricter drain rule. Its FPGA endpoint remains a software model.
- Nano `fabric` and read-only `fpga` programs cross-built. Symbol inspection
  confirms the configuration diagnostic has no board SMC entry, while the real
  probe has no configuration-model execution methods. This is structural
  evidence, not a physical boot or firmware-call test.
- `make test-changed` against the base, `make deadcode` and whitespace checks
  passed. Committed-source consistency is checked separately after committing.

The adjacent receipt binds the final sources, reports, fixture inputs, tool/ELF
identities and check logs. Full local reports remain beneath this worktree's
`out/native-contained-*` paths. Earlier namespace exploration runs are excluded
from the final-source evidence.
