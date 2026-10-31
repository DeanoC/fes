# Prepared persistent DE25 bootstrap candidate

FES packaging scope, base `e76179a112bfa4aa5baec03046747d82afe57e0e`.
This is off-board preparation only. No board claimed or accessed, no flash,
SD, boot configuration, runtime or shared contract changed.

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
