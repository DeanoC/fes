# Feature-gated current RAS snapshot after early Nano firmware faults

Implementation1e8d79ef4 adds a read-only architectural snapshot after the
existing39 CPU/CCU/OCRAM reads and original exception. The prior physical
fault inventory completed but did not identify the cause. Actual Nano MIDR
412fd050/PFR011112222 establishes A55r2p0/RAS1 for planning this new diagnostic;
no board action or new physical boot is claimed by this validation.

The first additional read is PFR0. Only RAS encoding1 permits DISR and ERRIDR.
NUM0 or reserved bits skip even ERRSELR; SEL>=NUM skips every ERX access. The
current record's FR/CTLR/status/MISC0/status and selector are read in order.
The selector is never written, and the final selector must match for END. Each
access follows a completed before marker; failed reads leave exact partial
progress under park-only fallback vectors. Explicit SKIP states are checked
against the actual feature/count/selector values. ADDR/MISC1, pseudo-fault
registers, status/selector/control writes and error acknowledgments are absent.
Only one existing selection is covered; no all-core/cluster coverage, freshness,
RAM access, health, recovery, startup or package permission follows from END.

83 focused tests,11 final RAS-parser cases,75 existing actual-ARM handler cases
and40 new actual-ARM control/failure cases pass. The selected RAS values in38
new cases are injected sources while production branches/logger/fallback run
as actual instructions;10 cases fault individual reads. Two unmodified QEMU
CPUs execute the actual MRS path and skip unsupported RAS values0(A53)/2(max).
They do not prove the selected-record registers work on hardware. Count/selector
coverage comes from injected values. All affected integration steps pass,
including1529 parent tests/3skips; the skipped shared-ROM linker test separately
passes with Go1.26.5. Remaining skips are macOS-only layout and absent cached
upstream U-Boot image. No changed source was excluded from integration checks.

Actual BL31/SPL/U-Boot builds, ELF/flat-image/source binding, normal vendor-vector
restore and linked RAS gate audits pass. The1428-byte early ownership guard,
private NOBITS range, original vendor cache operations and all existing limits
are preserved. The new fault handler contains only UART stores, no stack/call/
return or selector/status writes. Source-bound audits verify39 fixed reads plus
at most10 RAS reads and the branch targets that skip unsupported accesses.
Shared-lock file-only composition preserves the1658880-byte sealed core payload.
The retained native31d716957 application/entry are byte-identical, with all2098
selected source hashes unchanged. FIT/chain identity are in result.json.

No hardware upload/programming, physical RAM acceptance, native boot, ROM
completion or DE10-equivalent core-load acceptance is established. The next
step is a fresh frozen operator with independent live admission and one bounded
TRWU8CBL trial; never replay the stopped f7e image or widen its limits.

Primary sources: [Arm RAS specification](https://documentation-service.arm.com/static/5f8db40c4966cd7c95fd58e3)
sections4.4.3-4.4.9 and4.6.8, and [Cortex-A55 TRM](https://documentation-service.arm.com/static/5e7e09f6a3736a0d2e862d2f)
sections A8.6/B2.40-B2.50 distinguish core and DSU records, selector bounds,
read/write behavior and the mandatory MISC0 read. This diagnostic preserves
status and explicitly leaves the unselected record unobserved.
