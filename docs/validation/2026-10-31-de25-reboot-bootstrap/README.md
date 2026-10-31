# Prepared persistent DE25 bootstrap candidate

FES packaging scope, base `e76179a112bfa4aa5baec03046747d82afe57e0e`.
The initial preparation below was off-board only. The later explicitly approved
[physical trial](#physical-trial) programs the bottom board QSPI and verifies
reboot; no SD, runtime implementation or shared contract changes.

The actual [bootstrap packaging tool](../../../scripts/de25_bootstrap.py) ran
with Quartus26.3 Build114 against the existing HPS-first producer79d616e85
and the independently locked Terasic BSP rollback files. The full package is
retained in this task worktree at `out/bootstrap/`; its receipt is copied here
as [bootstrap.json](bootstrap.json), with real conversion/inspection logs.
The hashes below bind the proposed trial, rather than newer parent RTL.

| Artifact | SHA256 |
| --- | --- |
| Proposed QSPI HPS-first JIC | `fefe5246045354d665fbc195dad5350a0e23d1e3bf33516b93b0411832bfde64` |
| Derived volatile HPS-only RBF | `1a0d681a00efdb34ecc3ff1866da5dc343c3fe625fb6019351647144452b18de` |
| Locked vendor BSP rollback JIC | `e3d20c2d066761fd02897422ba361aa19faafcd0240dd2782965754cc61b38a4` |

The regenerated HPS-only RBF and core RBF match the exact physical proof in
[#1303](https://github.com/DeanoC/fes/pull/1303). That proof booted FES with the
HPS-only RBF, then Linux loaded the core and native AArch64 BASIC LOAD/RUN
produced HDMI7/0/1. The JIC itself is NOT programmed or hardware qualified.
The vendor rollback JIC is verified against the BSP lock, not read back or
hardware tested as restoration of this board's current QSPI bytes.

All three file types inspect successfully. Candidate and recovery metadata
report HPS_FIRST and matching HPS IO hashes, no core section; both JICs report
MT25QU128. The prepared receipt retains all source and file identities and
explicit `not-performed` / `not-qualified` status. No later parent video/reset
change inherits hardware acceptance from this package.

Five rejection-focused unit cases pass, including altered producer bytes,
FPGA-first input, wrong rollback bytes, mismatched core split, payload symlinks,
extra files and incompatible flash/core section metadata. The real Quartus
prepare and subsequent verify pass; documentation links and whitespace pass.
The affected parent-check result is recorded in `checks.txt` after completion.

The [trial procedure](../../de25-reboot-bootstrap.md) gates the first write on
explicit authorization, ownership, clean shutdown and a recoverable pre-write
flash readback. It requires three consecutive Linux reboot cycles including
core execution, with no JTAG redelivery, before changing the approved baseline.
On failure it uses proven volatile JTAG recovery and the separately approved
readback/vendor rollback route. Board release remains from #1303: verified
factory FPGA plus approved FES Linux; this preparation does not alter it.


## Physical trial

After Deano explicitly approved backup, installation and rollback if needed,
the bottom board was claimed and Linux shut down cleanly. Quartus Examine
`ei;before-readback.jic;MT25QU128@1` read the original flash in33seconds;
PFG inspected its firmware25.1.1 / HPS_AFTER_INIT_DONE contents, and a separate
flash Verify passed. Two initial invalid Examine syntax forms refused without
performing an operation. The backup is retained read-only outside the task
checkout; its SHA256 is
`5741d2f0a28d0009eda0bd28dfb3e768047a89cbf2819e22ba3eb4303235f2ef`.
It is an actual board readback, distinct from the locked vendor BSP JIC.

The approved candidate programmed and verified in24seconds with no warnings
or errors. Post-program `--initcfg` did not start Linux within180seconds.
The known HPS-only JTAG recovery booted Linux, but its ordinary reboot still
stopped with no new SPL/login within90seconds. Deano then performed one initial
physical power cycle to activate the new flash startup.

Following that first QSPI boot, THREE consecutive ordinary Linux reboots
reached new SPL/kernel/FES login and distinct boot UUIDs without JTAG
redelivery or physical intervention. After the first and second reboot,
Linux FPGA manager loaded the exact selected core (`operating`/`applied`),
with SSH and the same boot UUID intact. The frozen AArch64 client verified
the computer identity, reset execution, uploaded the tape and typed LOAD/RUN.
Both runs displayed `7` and `0/1`. The ROM K prompt was observed before input.

![BASIC after first QSPI software reboot](hardware/round1-run.png)
![BASIC after second QSPI software reboot](hardware/round2-run.png)

[hardware/observations.json](hardware/observations.json) binds the four QSPI
boot epochs, artifacts, final baseline and limits. The final boot is
`e8963972-c3e9-405c-a2ef-93eb09f691e6`; trusted UART and pinned SSH verify
FES/AArch64, the same approved card and vendor kernel. Temporary native/media,
module and bitstream files were removed before reboot; the final check verifies
no modules, owned temporary files, configfs overlay or firmware search override.
A final read-only CONFIG query verifies SDM26.3 and AS_FAST_POR. No helper or
programming occurs during those three reboot cycles. One initial malformed
status query omitted its required JTAG mode and refused before device access;
the corrected read-only query passed. USB permissions return to root:root0664;
all task UART/SSH/capture/JTAG clients close and the idle kit is released.

The working FES SD plus this exact QSPI bootstrap is retained under the
approval to install and restore only if needed. Full baseline/recovery files
were independently copied, reverified and made read-only at
`/home/deano/kit-status/baselines/de25-TRWU8J8G-hps-first-20261031/`.
The original flash readback survives at
`/home/deano/kit-status/rollback/de25-TRWU8J8G-20261031-before-hps-first.jic`.
No rollback was needed, so programming that backup is not hardware tested.
The top board and both DE10 kits are unchanged.

### Evidence retention limit

After final hardware verification, the entire c134 checkout disappeared before
the live trial evidence was committed. Cause is unknown. The committed
preparation and separately preserved baseline/backup survived. Raw trial UART
files and full native receipt files are lost. Do not claim they are retained.

The actual returned tool observations and four returned image responses were
recovered from this session's existing log without rerunning or synthesizing
results. [hardware/tool-observations.json](hardware/tool-observations.json)
preserves those timestamped outputs, with private IPv4/MAC addresses redacted.
[hardware/image-provenance.json](hardware/image-provenance.json) identifies the
returned image bytes and their hashes/tool-call IDs. These are recovered tool
images, not a claim of recovery of the original FFV1 videos or raw frame files.
This limits trace completeness; the displayed pictures and observed reboot,
manager, native-control and final identity checks remain direct physical
observations. No production DMA/reset fencing, failed-load recovery, warm reset,
MENU, managed runtime or normal catalog launch acceptance is inferred.
