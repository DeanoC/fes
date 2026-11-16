# Qualified cold set/way startup for private OCRAM

Implementation `0cab5df0c` addresses the newly captured illegal-DII-access lead
without removing cache maintenance. In the selected version-2 RAM profile,
the private NOBITS range uses the pinned TF-A `dcsw_op_all(DCISW)` routine
instead of address-based PoC invalidation. DDR range maintenance and warm entry
remain unchanged. This is a built, off-board-validated prototype; it has zero
physical attempts and does not yet qualify native boot or ordinary core loads.

The helper requires the exact compiled private base/length/aligned end, active
logging lifetime, reviewed primary A55 r2p0, MMU/data cache off, and fresh
CPU/master-reset and power containment. CSSELR must start at0; ownership and
selector0 are checked again after maintenance. It emits two bounded CACHE
receipts. Only then may logging be restored and ordinary BSS initialization
continue. Missing, duplicate, partial or faulted receipts cannot qualify boot.
The previous RAM-profile version is rejected rather than retained as a fallback.

This explicitly handles stale cache contents instead of treating cache-off as
proof of emptiness. The [A55 TRM](https://documentation-service.arm.com/static/5e7e1405b471823cb9de57ae)
A6.2.3 describes data/unified cache bypass and absence of L2/L3 instruction
allocation with C0, while maintenance remains effective. The selected whole
SPL binary contains no SCTLR/TTBR/TCR writes or exception-level transitions;
its build requires data-cache-off. In BL31, the only earlier SCTLR write adds
I/A/SA, leaving MMU/data cache off. Reviewed early callees do not change those
controls. These are linked-code and source-bound handoff proofs, not a claim
that pre-boot caches were empty or that physical cache coverage has passed.

The [architectural DC ISW definition](https://support.arm.com/documentation/ddi0601/2026-03/AArch64-Instructions/DC-ISW--Data-or-unified-Cache-line-Invalidate-by-Set-Way)
requires valid cache geometry. The exact 272-byte vendor helper reads that
geometry and preserves the retained registers without a stack, data-memory
access or nested call. Its selected mode0 reaches the ISW loop; the clean-only
and clean/invalidate loops are unreachable from this selector. The
[A55 errata notice](https://documentation-service.arm.com/static/631b60c9f72fad1903829087?token=)
requires exclusion of other cache clients; the reviewed r2p0 lies outside the
r0p0/r0p1 range of its documented 827496 index defect. This is not a general
errata signoff. The cold CPU/master guards remain mandatory.

A second correction rejects a newly logged error even if CPU exception delivery
is delayed. The normal path first verifies logging restoration and unchanged
status, then stops on nonzero status. The fatal path retains its original
exception and status without acknowledgement. A successfully restored control
is never a healthy-boot grant after an error.

Final validation:

- 107 focused tests and all ten affected software steps pass. These include
  1,547 parent tests with two environment skips, 1,581 FPGA-tooling tests with
  75 tool/fixture skips, generated consumers and native C/RTL/producer checks.
  The shared-ROM linker runs with Go1.26.5.
- 54 ARM cache-policy cases cover real emulated set/way execution, wrong ranges,
  state/owner changes, every shared scope-read fault before/after maintenance,
  selector/access failures and a logged error without an exception.
- 66 ARM logging cases and six full-handler/inventory/cache integration cases
  pass, giving 126 ARM cases on final code. MMIO and admitted identity are
  injected. Physical CCU behavior and cache coverage remain unqualified.
- Instrumented loop counts match independently reported geometry:16,896 visits
  on emulated A53 and33,280 on emulated max. Both expose CCIDX0. A separate
  explicitly injected CCIDX1 model checks1,028 visits. Counting variants replace
  the operation with a counter; they do not perform physical cache maintenance.
- Final rebuilt BL31, SPL/U-Boot FIT, shared-lock HPS-first file composition and
  instruction/allocation/branch audits pass. The 1,428-byte guard and 19 C wire
  assertions remain enforced. Reachability analysis also confirms the unchanged
  `zeromem` device-memory path cannot reach the separate DC ZVA path.

The final FIT is8,187,904 bytes; chain
`e86db075cff9809a7679ca227bf1a69b6b1d71e39b4891bb1336c846da04615c`.
Only `native-bl31-final`, `native-uboot-final` and `native-composition-final` are
selected. The native application/entry from `31d716957` remain byte-identical,
with all2,098 input hashes checked. The sealed FPGA core payload is unchanged.
Exact source/evidence identities are in [result.json](result.json).

Earlier candidate builds and failed preparation logs remain retained. One audit
wrongly assumed emulated max exposed CCIDX1; it was replaced with measured
geometry plus the explicit extended-format model. A fixture used a register
clobbered by UART formatting, and a collector stopped at a partial terminal
prefix; both are corrected. One audit ran before its dependent final SPL existed.
The later error-status guard required new builds and a fresh full affected run;
earlier passing candidates are not substituted for those final results.

No board action occurred in this milestone. TRWU8CBL remains claimed after the
previous b95 fault; that input consumed its attempt. Its spare card is retained,
the factory card and bottom kit are untouched, and CPU halt remains unproved.
The current source/evidence archive is
`/home/deano/fes-retained/de25-setway-20261106-r1/`, with prerequisite
`de25-decode-log-stop-20261106-r1`; member readback and source-pack import are
verified before announcing retention.

Next freeze/test a new bounded operator with explicit CACHE receipt checks and
a restored-with-error failure outcome. Reuse the verified seven-file ROM catalog
and bind the new boot FIT. After fresh recovery/card/USB/clock/claim admission,
run one bounded test on TRWU8CBL. Keep every existing ROM16-byte/400M-tick/
64-poll and prompt/startup/launch/configuration/control/process deadline. Stop on
the first failure; retain the trace and exact host cleanup. Normal package/ROM
admission and DE10-equivalent core loading remain open until physically verified.
