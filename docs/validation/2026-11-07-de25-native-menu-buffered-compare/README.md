# Buffered exact menu comparison, 2026-11-07

The [direct comparison trial](../2026-11-07-de25-native-menu-exact-trial/README.md)
passed every functional gate but regressed to about 1.7 seconds per frame,
versus the saved 1.3-second hashing control. That attempt/source is retained
and the direct candidate is rejected for performance.

`arenaComparison` now copies fresh bounded 32 KiB chunks from the uncached
frame arena into private CPU scratch, then compares those cached bytes exactly
against the private expected image. Every successful check rereads the complete
8 MiB, including both slots and padding; no earlier result or sampled bytes can
substitute. Length mismatch or corruption rejects. Existing session operation
locks serialize the scratch, which is never given to DMA. No owner/generation
check, store barrier, deadline, poll bound, failure retention or independent
master-stop/closure is removed. Source/package/map/initial-publication hashes
remain unchanged. This buffers the reads used by exact equality; it does not
yet establish a hardware speedup.

The focused race-enabled menu suite passes the existing positive/failure cases,
50 lifecycle corruption cases and new helper checks for empty/mismatched lengths,
unaligned starts, scratch boundaries, tails and fresh changes to either image.
Fresh native integration also passes actual prepared C/control RTL, injected
platform ownership and real Tenfoot launch/return/failure paths. All affected
commands pass, including 1,638 parent tests with three existing skips. The
local TF-A reference retains the same pinned source identity check. Unchanged
CPU discovery/crypto model inputs are verified and their evidence reused.

The fresh SDK, entry, BL31, SPL and HPS-first composition pass the matched
input/actual-ELF/final-SPL-guard audit. Firmware BSS remains `0x5afc0`; the
reservation, guard rejection branch and private cookie are preserved. FPGA
package/build/core bytes and CPU feature selection remain unchanged. The FIT
stays below 8 MiB. Exact sources/hashes/check receipts are in
[result.json](result.json). A new bounded top-only attempt must pass the entire
three-frame/local-drain/independent-return/cleanup sequence before its timings
are compared. Keep the 1.3-second control if this candidate is not faster.

The continuing top-kit claim/spare remain under the factory-restoration waiver.
Original card and bottom kit stay untouched. Physical HDMI, live App/core/menu
display ownership, ordinary admission and broader device/platform qualification
remain open.
