# Checked SDM containment preparation

This change integrates a default-off containment candidate with the existing
source-bound DE25 kernel producer. It does not qualify recovery or close #1298.
Ordinary ZX81 remains the development milestone; physical personas are still
waived/default-off under #1568. The running board was not accessed or changed.

`--sdm-containment` selects CONFIG_FES_DE25_SDM_CONTAINMENT independently of
static expansions. It requires the permanent-owner profile and built-in SMMUv3
and service. The producer and image assembler verify the selected flag against
the actual embedded Image configuration and full supplier profile. All eight
supplier files are authenticated before/after patching; a new header must not
already exist. Unchanged clock source remains authenticated separately.

The manager error path retains uncertainty and all buffers, closes service
admission under a controller lock and requests a checked SDM SID10 abort entry.
The first result is permanent for that kernel epoch, including timeout. No
retry, reopen or buffer release is introduced. Known errors attempt the block
before the existing worker join; errors discovered only during join attempt it
afterward. Original programming errors remain authoritative. Another existing
service client causes refusal before hardware access. The mailbox mutex is
not taken by the new path.

The existing built-in, protected manager allocates its four programming buffers
at probe and retains them between requests. Supported supplier and ancestor
unbind paths are suppressed. The candidate depends on that permanent owner;
refusing free is not a general solution for arbitrary caller destruction.
A stalled synchronous secure call may prevent Linux from running containment.
IOMMU group/queue waits, buffered traffic and cross-reset retirement remain
unqualified. No successful model result authorizes physical failure injection,
memory reuse or reboot after an uncertain transfer.

Validation at base c95aed6291143d92b76bc912eb96bae2de56a30d plus this change:

- Nine producer tests and nine image tests pass, including new-header collision,
  dangling symlink, unsolicited/modular/duplicate containment config and a mode
  receipt inconsistent with the embedded Image.
- Actual-source write and lifecycle proofs pass with successful and failed
  containment replies, containment disabled, and platform-disabled positive
  controls. Original permanent-error/BUSY hangs and unsafe-retry controls are
  retained. The claim proof's nine cases pass.
- Actual-source controller and SMMU models pass on native Linux and AArch64 QEMU:
  25 concurrent same-channel request pairs, in-flight/final status caching,
  refused ownership release/admission, foreign ownership and 23 SMMU validation,
  command-status and STE-reopen cases. Kernel types/locking, entry-update steps,
  IOMMU iteration and command completions are injected.
- Two complete AArch64 kernels build from a private copy of the authenticated
  9cec source/config/SDK, with containment disabled/enabled. Both preserve that
  baseline's compiled static-expansion setting; this is not a change to runtime
  persona admission. Embedded configuration, linked notes, DTB and required or
  absent containment symbols are verified. No native boot or clean public
  snapshot build is claimed by this incremental diagnostic.

See `offboard.json` for exact source identities and kernel hashes. Powerboat
retains commands, results, input/output files and compiler identity under the
recorded evidence path. The first missing-module preflight and patch-context
whitespace refusal remain retained. The patch was regenerated with zero context;
all eight supplier results and executed model bytes are unchanged and rechecked.

Next: a clean committed-source producer build and matching image preparation,
then an approved exact-stack boot/ordinary-programming check. Independent
physical fence/reset qualification must precede intentional failed-transfer
injection. The separate ordinary failed-state Stop/reboot policy proposal is
still pending; this change does not implement it. Prior c95 ordinary source-image
and host HIL remain tied to their original artifacts and do not accept these
new kernels. Current-head review/CI and authorized landing/main baseline remain
separate gates.

The HIL planner classifies this change as FULL_IMAGE. Passing the CPU models
and incremental kernel builds does not satisfy that physical image/boot check.
