# DE25 repeated-load boot contract and paired auditor

Format 15 selects generation-bound source release and warm SDM/SMMU rearm
explicitly, through `--nano-sdm-source-rearm` together with
`--nano-sdm-input-transfer` in both firmware/boot builders. The default and
format-14 first-MENU selections keep source release disabled. This builds on
the [firmware prototype](../2026-11-14-de25-source-rearm/README.md); it does not
establish physical repeated-load acceptance.

The cold-chain producer binds six CPU source files from the actual matched
entry's selected native source map: release-before-copy, the private arena,
stored configuration, both storage-return paths and the generated bridge
protocol. Missing or changed compiled source is rejected. Paired MENU storage
return derives from its compiled companion, as it does in the existing cold
header; it does not require the unrelated raw window-diagnostic flag. The
contract requires retained selected Tenfoot programs, computer rearming and
computer storage return. Both boot builders carry the selection through their
pre/post source checks and shared chain derivation. The final ELF must contain
the strong release/rearm implementations, not weak default suppliers.

## Per-load observations

The firmware emits a source-bound warm-preparation record with owner, epoch,
previous/new generations, source size/SHA-384, the actual queue baseline and
producer, retained abort and fresh source clock. It observes existing private
state and adds no register write or ownership grant.

The actual paired Tenfoot controller now has a separately selected source
auditor. It verifies the first cold source, then requires an owned source
release before each next configuration and the matching warm-preparation
record inside START. Each source must independently pass full transfer,
retirement, timing, queue-consumption and continuation checks before the
existing configuration/identity and window/held-computer lifecycle parsers
consume the rest of the wire. MENU/core generation advances remain six/five;
queue cursors and SDM job IDs carry their actual wrapping history. Warm clocks
cannot reuse the preceding transfer or extend its failed attempt.

The existing cold observers retain their fixed first-load expectations.
Repeated-load expectations are privately derived from the preceding verified
source. Reader proofs bind the current source generation, while format 14
still admits only the first MENU. Computer opening separately requires the
retired Non-secure abort and unchanged Secure bypass state. An unknown,
missing, reordered or changed proof retires the paired client; it sends no
further exchange. A UART record does not grant DMA, source reuse or hardware
acceptance.

## Evidence and remaining work

Synthetic source traces cover thirteen changing-length/digest sources and
wrapping queue/job histories. The real paired controller completes its full
three-core launch/return/final-Park exercise: seven configurations, ending at
source generation 37. It rejects changed releases, cursors, write counts,
namespace state and missing continuations. The model injects a verified boot
observation and synthetic source/SDM wire around existing lifecycle fixtures;
it does not execute a real boot image, package/ROM load or physical core.

The preceding firmware commit `3a4ceaf0c5e6891c38b2bd52f0cbf9560ff12c70`
now separately passes all eleven selected affected phases: 2,699 parent tests
(seven skips), native runtime/producer integration, firmware/identity/drain RTL
and 2,181 FPGA software tests (88 skips). That result does not validate this
successor. Focused successor results and exact selected source hashes are
recorded in [software.json](software.json).

The next step is rebuilding the SDK, CPU entry, complete BL31 and SPL/U-Boot
under this explicit contract, checking the linked image/stack and fresh native
file admission, then freezing a new bounded TRWU8CBL procedure. No card update,
JTAG operation or hardware acceptance follows from these software tests.
The previous format-14 hardware procedure remains consumed. PR #1791 stays
`lane:deano`; the full DE10-equivalent loading goal remains open.
