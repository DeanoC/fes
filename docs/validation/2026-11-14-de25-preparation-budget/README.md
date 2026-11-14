# DE25 four-second preparation budget

Source `96063aba1753450e9ab0762ee8cc6b887697a19c` raises input preparation from one to four seconds. The earlier physical image stopped during legitimate warm table preparation: 5,120 loads and 2,816 stores, no command-queue submission or further configuration DATA. The exact one-second value was a software startup guard, not a core real-time requirement. The [actual preceding hardware result](../2026-11-14-de25-usb-recovered-warm-stop/README.md) remains a failed trial.

Clock descriptions were corrected in source `5c5a9a5b48470c45dc450fb1834540209c333891`: preparation starts at its first guard; the independent twenty-second overall source lifetime starts before the extra SHA-384 check. Both existing origins are preserved. The corrected production header preprocesses byte-identically to the implementation, and the fixture executable tokens and Python assertion bodies are identical apart from one test method name. The final matched artifact is rebuilt from the corrected source, rather than inheriting its predecessor's artifact admission.

| Scope | Budget and origin |
| --- | --- |
| Input preparation, nested final validation and later arm | Four seconds from the first preparation guard; no stage may rebase it |
| Preparation command construction and accepted completion | Unchanged 100 ms |
| Source release | Unchanged one second, independent of the completed transfer |
| Retirement staging / consumption / final scope | Unchanged two / one / four seconds |
| Overall source lifetime | Unchanged twenty seconds from before the extra source digest |
| Arm-only and fault-only setup | Unchanged one second |

Selected contracts are now formats 19/20/21. Full old contracts 16/17/18 and partial version/budget substitutions reject. Owner, epoch, generation, source integrity, complete old/new table comparisons, callback limits, queue ordering and failure retention are unchanged. Preparation's command completion still rejects a late consumer even when final validation has more time. An expired preparation cannot restart or arm.

All 61 focused source/retirement/profile/trace checks pass. New cold/warm injected-cost cases complete the full table work and arm beyond one second; the slowest successful case is 2,726,502 microseconds. The same fixture fails against the exact preceding one-second setup/queue/retirement sources. Excessive I/O or prior preparation cost still expires before any new register write or queue attempt, retains the old controller and prevents retry. These are model costs, not a physical latency measurement or attribution of the hardware slowdown. A first fixture compilation failed on nested test-main renaming; the corrected reusable fixture and final checks pass, and the initial failure summary is retained.

All 11 selected affected phases pass in 1669.775 seconds. The parent suite reports 2,746 tests/15 skips; the FPGA suite reports 2,181 tests/88 skips. Selection was computed on the implementation commit, and the comment-only successor was committed while that campaign was running. The identity record verifies this transition; no executable production or assertion change was made.

[Focused model retention](retention-proof.json) preserves the implementation and controls. [Affected checks](affected-checks.json) and [clock-source identity](clock-identity.json) record broader scope and the comment-only source transition. Final direct clock models and repository consistency checks pass. These records do not claim a frozen whole-campaign run on the final documentation head.

The final normal SDK, CPU entry, BL31, SPL/U-Boot, HPS-first composition and complete native file admission pass on source `5c5a9a5b4`. [Matched identities](matched.json), [integration checks](integration.json) and [matched retention](matched-retention-proof.json) bind the actual files. The selected exported setup and command headers contain the four-second values and the boot contract is format 21. The independent release/command/retirement/transfer limits are checked. Core payloads are unchanged. Packaging holds the shared Quartus Pro lock; no synthesis, fitting or physical programming occurred. Private NOBITS still has 192 bytes spare with four unchanged 8 KiB stacks; this is not full EL3 stack qualification.

A new files-only restoration adapter separately passes eight scope models and full original-file admission. Worker parsing, coordinator roles and hooks reject all native dispatch, including the original image that previously failed USB Close twice. Its positive model restores/readbacks files only; its readback-failure model stops without retry. It is not frozen or executable until a fresh human power-cycle confirmation is recorded. No such confirmation has arrived for the latest private SMMU failure at this checkpoint.

Current hardware remains stopped after the earlier one-second trial, with FIT `4831f267…` on the spare and original FIT retained on the host. All host handles were independently closed and the failed trace retained. The new four-second artifact has not been programmed. Repeated source rearm, final USB shutdown and Linux-equivalent general core loading remain unqualified. QSPI, factory card and bottom kit remain untouched; the continuing top-kit claim is held.
