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

Parent validation ran 64 planner cases and 1,690 parent cases (45 skipped).
The precommit run retained one source-provenance error: the fixture selected
HEAD while its factory recipe came from the modified working tree. After
commit a0c127a260714d528c23b5eb87b880e6388c7156, that exact test passed against the
matching Git snapshot. The remaining planned consistency check and `make check`
pass (41 consumers/45 fixtures). No entire-parent rerun is claimed; new-head CI
remains pending. The precommit failure log remains preserved.

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

## Shared-service ownership correction

The clean public producer at 1684bf394 built Image `c9894b4c` with the recorded
source profile, mode and kernel notes verified. Before any board trial, a
further source audit found that the stock configuration enables SENSORS_SOC64
built-in. The enabled Terasic temperature/voltage DT node can bind soc64-hwmon,
which acquires another service channel and asynchronous client at probe. RSU
and FCS drivers also use the controller. A sole-device IOMMU group is therefore
not proof of sole service ownership; no native active-client count is claimed.
The manager's live count guard correctly refuses another client, so the earlier
producer did not guarantee that this experimental hook could run on its output.

The opt-in producer now excludes all pinned non-FPGA service clients and FCS
consumers, both built-in and modular. Kconfig and configuration/image verification
require that exclusion. Temperature/voltage monitoring, RSU and FCS services are
unavailable in this experimental configuration; ordinary builds keep the stock
choices. Unexpected live ownership still refuses before hardware access. This
adds no shared-client parking, DMA release, retry, reset or firmware change.

Twenty focused producer/image cases pass, including every excluded symbol in
both y/m forms and a resealed image configuration that tries to enable hwmon.
The new policy rejects the earlier c989 kernel with its exact SENSORS_SOC64
reason before image assembly. Its partially built managed base was stopped by
its exact authenticated own container ID; logs/cache and the completed kernel
remain retained. It was not a compiler failure, successful image or board trial.
No same-head base replay occurs. The corrected committed producer must rebuild
and verify a new kernel/base/image before any physical approval or trial.

Powerboat retains the pinned source/hash audit, stopped controller and precise
policy refusal under `sdm-committed-producer-1684bf394/`. The eight supplier C/header
outputs are unchanged by this correction; the selected kernel configuration
changes, so prior whole-kernel and physical evidence is not transferred.
