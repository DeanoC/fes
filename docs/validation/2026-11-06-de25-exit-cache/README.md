# Qualified private cold BSS exit clean

Implementation `66812745d` addresses the interval located by the
[consumed runtime trace](../2026-11-06-de25-runtime-startup-trial/README.md):
29 runtime markers completed through `bss-clean-before`, after the DDR data
clean returned, but no BSS-clean return, exit or loader prompt followed. That
trace does not identify an exact stalled/faulting PC. This new input replaces
only the cold clean of the compiled private BSS; it does not replay that input
or claim the previous stop was conclusively caused by one cache instruction.

The version 3 RAM contract fixes BSS at `58000..5dec0` and requires the private
exit-clean policy. The selected source replaces that BSS range call with the
pinned vendor `dcsw_op_all(DCCSW)` helper. It preserves writeback intent across
the accessible hierarchy, leaving the original DDR clean and warm source
unchanged. `DCCSW` retains the vendor erratum alias to clean/invalidate; other
selectors fail assembly. The linked image selects mode 2 / `DC CSW`.

Fresh checks before and after the operation require the reviewed A55/EL3/
primary affinity, MMU/data-cache-off and endianness state, DAIF masks, CPU/
reset/power and master containment, logging flag 0 and CSSELR 0. The arguments
must match both compiled symbols and the fixed aligned endpoints. A mismatch
parks without retry, maintenance skip or address-based fallback. The macro
introduces no stack access, private-data store, detection/interrupt/status
control write or additional helper. The actual linked helper is register-only.

The whole linked EL3 image contains four SCTLR writes: cold/warm setup adds
only I/A/SA while preserving M/C/endian, and two generic power-down helpers
clear C. There is no EL3 TTBR/TCR/MAIR write. Together with the initial full
hierarchy invalidation, uncached SPL/early-entry checks, no lower-world entry
before this point and fresh exit admission, this bounds the cache-allocation/
alias history without assuming cache-off means empty. Fresh ownership is
required because writeback or invalidation can affect shared caches.

The [Cortex-A55 r1p0 TRM](https://documentation-service.arm.com/static/5e7e1405b471823cb9de57ae),
A6.2.3/A6.2.4, discusses maintenance with data caching disabled and possible
clean/invalidate behavior for set/way instructions. The implementation accounts
for invalidation as well as cleaning. This source/manual reasoning and QEMU
execution do not establish physical cache coverage on the board's r2p0 CPU.

EXIT_CACHE `clean-before`/`clean-after` records enclose the operation and final
checks, within the runtime BSS-clean bracket. The current decoder requires
ordered completion before BSS return/cold exit/U-Boot. Missing, malformed,
duplicate, reordered, misplaced or exception-interrupted progress remains
terminal. The before record marks uncertainty, not proof the operation ran.
Even completion grants no boot ownership or ordinary package capability.

Validation:

- 93 focused tests and all ten affected steps pass, including 1558 parent
  tests/two environment skips and 1581 FPGA tooling tests/75 tool-fixture skips.
  The affected run took 687.8 seconds under its original 900-second cap.
- 66 ARM exit cases execute the real new macro and pinned helper under QEMU.
  They cover both vendor selectors on A53/max, bad ranges and fixed image
  bounds, CPU/affinity/flag/reset/power/selector rejection, pre/post drift,
  cache-enabled rejection, real lower-EL entry, injected control observations,
  faults at both selector and all 20 scope reads, cache-instruction failure,
  blocked/all-ones UART, blocked post-clean UART and a 384-byte unterminated
  string. Identity/reset/UART register files are modeled. Geometry-counter
  cases replace operations with increments; actual CSW/CISW cases remain
  separate. CCIDX1 geometry is explicitly injected, not a QEMU CPU feature.
- Three invalid selector definitions fail with the intended assembly error.
  Source pins, actual mode 2, the 272-byte helper, both fresh scopes/selectors,
  exact BSS arguments, UART bounds and whole-image controls pass ELF audits.
- The current 1428-byte entry guard/21 C wire assertions, 31 runtime phases,
  initial cache/logging/fault paths, compiled SPL branch/allocation exclusions
  and full final matched-chain checks pass. Installed source binding has 18 files.
- All 20 existing firmware-driver objects, both vendor C startup objects and
  all 2098 native/entry source hashes remain byte-identical. Native source is
  `31d716957`; the sealed FPGA payload is unchanged.

The initial lower-EL harness tried to install an EL3 vector after entering EL1;
the next version also lacked the lower-world execution-state setup. Both
failures are preserved. A bounded interrupt debug trace identified an illegal
return state. The corrected harness removes the privileged prelude for lower
EL tests and establishes HCR/SCR state before ERET; all final cases pass. These
were off-board harness corrections, not firmware retries or relaxed guards.

Final BL31/SPL/U-Boot/shared-lock file composition passes. The FIT is
8,192,000 bytes; chain identity is
`43e6315533c2da45c40d4cdc96f5edd41f0099add4c854eaac92292057fa3683`.
Only the `native-bl31-final`, `native-uboot-final` and
`native-composition-final` outputs under `out/nano-exit-cache-20261106` are
selected. No FPGA core rebuild was needed.

This firmware has zero hardware attempts. TRWU8CBL remains claimed after the
preceding stopped boot; spare retained, factory card/bottom kit untouched,
CPU/FPGA halt unproved. The user then reported an accidental power interruption
and restored power. Passive host enumeration confirms the top board and its
serial connection have returned; its boot/reset state requires fresh admission.
No test or card write was running. No programming, upload or SD write occurred
during this milestone. Ordinary core loading and native ROM/Run/return remain unqualified.

Next freeze/test an exit-cache-aware operator, require both EXIT_CACHE records
in its service gate and retain partial exit state in the two-second passive
failure tail. Reuse/verify the seven catalog payloads, bind this FIT as the
eighth upload and freshly admit claim/device/UART/card/recovery before one
bounded trial. Prior 18 campaign stops/eleven pre-native stops remain retained.
Keep all prompt50/startup100/launch100/config20/control5/native450/child480/
work740/host850, passive2-second/1MiB and ROM16-byte/400M-tick/64-poll bounds.
Stop first failure without fallback/replay; restore 15M/exactUSB/privateJTAG/
UART, retain claim/spare and leave factory card and bottom kit untouched.

Exact hashes/results are in [result.json](result.json). The source pack and
complete archive are verified by import/readback at
`/home/deano/fes-retained/de25-exit-cache-20261106-r1`, with prerequisite
`de25-runtime-startup-stop-20261106-r1`. Build/model/audit evidence does not
establish physical boot or ordinary runtime admission.
