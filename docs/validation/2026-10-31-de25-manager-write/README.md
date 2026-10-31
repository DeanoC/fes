# DE25 manager data-write error and deadline proof

Producer `20726374bcc90ce42b485e35f9506460b5b2e6da`, locked vendor kernel
`d7d192a9ddd955fe206aa350afed8f82e368a74c`. This is off-board source-function
and complete AArch64 kernel-build evidence. The physical board remains on
[the earlier verified kernel](../2026-10-31-de25-owner-zx81/README.md);
this new kernel has not been installed or booted. Normal runtime admission is
closed. No kit, firmware, SD or flash operation occurred during this milestone.

The pinned manager's `s10_ops_write` immediately retries every negative
`s10_send_buf` result. Permanent ENOMEM/EIO can therefore spin indefinitely.
Progress bits also take precedence over an accompanying error. The existing
separate service-claim candidate does not repair this manager loop.

The permanent-owner profile now repairs this path only under
`CONFIG_FES_DE25_DMA_PLATFORM`. Permanent errors retain their original errno;
ENOBUFS waits for a callback; error notifications win over progress. A total
30-second data-write phase budget caps waits alongside the existing five-second
buffer timeout. It does not change the request timeout. The expiry check and
remaining budget use one jiffies sample. Ordinary profile-disabled behavior
retains the vendor loop.

## Actual-function proof and controls

[The focused runner](../../../image/kernel/de25-fpga/verify_write.py)
authenticates the complete original source, exact platform patch and complete
patched manager. It compiles the actual changed function, rather than a rewrite,
against injected service, clock, callback and completion helpers. All thirteen
cases pass; the [receipt](function/checks.json) and actual C inputs are retained.
They exercise success, permanent ENOMEM, silent and notified ENOBUFS, concurrent
error/progress bits, missing callbacks, EIO after partial submit, claim-command
failure, spurious completions, submission-time expiry, jiffies wrap and expiry
between clock reads. A profile-disabled positive transfer passes too.

The unmodified function hangs on permanent ENOMEM and is terminated only by
its owned one-second test timeout. An initial repair sampled the clock twice
while calculating the wait; the [clock control](clock-control/result.json)
reproduces its overlong wait as an assertion failure. The final single-sample
implementation passes that same case. Earlier harness-only compile/assertion
failures were corrected before the final proof; they performed no hardware
operation.

## Complete kernel build and affected checks

The final complete kernel runner passes in 217.8 seconds (211.5 in its pinned
container). Image SHA256 is
`4c2eb5885b26fe95a4147d4a29553ec61087205edf8e91b7ff037e40b97f2811`.
The selected opt-in configuration is enabled and `s10_ops_write` is linked in
System.map. The built-in supplier/owner checks and pinned-libfdt compiled-DTB
verifier pass. Configuration, DTB and symbol versions match the first candidate;
the corrected manager changes Image/System.map. Complete source and artifact
hashes are in [kernel-build.json](kernel-build.json) and
[kernel-inputs.json](kernel-inputs.json).

All six affected commands pass at the final producer in 266.8 seconds.
The earlier candidate also passed its full build and six-command affected run;
those results are retained separately and do not replace the final results.
The pinned libfdt verifier compilation retains its existing unused-parameter
warning; no clean-warning or timing claim is made. Final hardware acceptance,
whole-image assembly and managed runtime tests have not run for these bytes.

Full build artifacts and logs are independently retained under
`/home/deano/fes/out/artifacts/de25-write/20726374b`, indexed by
[artifact-inventory.json](artifact-inventory.json).

## Remaining integration boundary

The fake service completion in the function test is not an SDM abort or DMA
fence. `stratix10_svc_done` can still join a thread stuck in the unpatched
service-claim loop, and software cannot interrupt a secure call that never
returns. This record therefore proves neither bounded whole-load failure nor
safe retry, buffer recycling, removal, cancellation or reboot after uncertainty.
The earlier bounded-claim patch remains separate and unadmitted. Before managed
programming, the kernel/service path must preserve poisoned programming state
and resources after uncertainty until an independently verified hardware fence.

The production factory still refuses AArch64/DE25. Its DE10 programmer accesses
Cyclone V registers and cannot serve DE25. The next implementation must use the
kernel-backed programmer within the existing single runtime lifecycle/factory,
then connect the DE25 core driver, agent/catalog/MENU and normal ZX81 lifecycle.
Physical DMA/DDR/SMMU/firewall/reset/drain acceptance and timing signoff remain
open. This repair is progress toward that environment, not normal-kit HIL.
