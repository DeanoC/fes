# Bounded initial loader progress

Implementation `7f3824490` instruments the interval identified by the
[consumed hardware trial](../2026-11-06-de25-exit-cache-trial/README.md).
That input passed private BSS exit cleaning and reached U-Boot. One `fesboot`
command then stopped after its memory/LMB report, before any native entry
marker, owned receipt or App Ready. The exact stopped/faulting PC is unknown.

The initial loader now emits 26 ordered receipts around memory validation,
the CPU snapshot and each of four PSCI affinity calls, reset/watchdog/UART
read groups, CPU validation, watchdog stop and owned observation. An after
receipt follows a return; it does not prove the result passed later policy.
The owned observation must retain its original identity/generation checks.
The late snapshot remains untraced. Removing instrumentation and recombining
the original short-circuit CPU/watchdog condition reproduces the consumed
loader source byte-for-byte. The linked-code audit checks this inverse and
preserves calls, arguments, memory/EL, CPU, watchdog and ownership policies.

Markers use direct UART writes with 100,000 polls per wait, a 128-byte string
limit and final transmit drain. There is no scheduler, timer, watchdog or
firmware call in the marker helper. An all-ones/stuck status or unterminated
string parks. New MMIO writes target only UART data. A last before receipt
still leaves final UART drain, control transfer and the call itself within the
unobserved interval; it cannot identify an exact instruction.

The decoder retains bounded partial progress, strictly checks order/scope and
requires current guarded-RAM traces to finish before late handoff. Missing,
duplicate, reordered, malformed, misplaced or failed receipts cannot complete
native boot. A firmware-call before record marks uncertainty, not proof that
the call executed. Progress supplies no ownership or package permission.
Archived observations remain tied to their named source and format selection.

Validation:

- All six affected-check steps pass, including 1,560 parent tests with three
  skips. The complete affected run took about 296 seconds under its 900s cap.
- Three focused test methods and 56 native-trace regression tests pass. They
  exercise complete and fragmented boot contexts, missing/duplicate/reordered
  markers, bad scope, failures, floods and permanent rejection.
- Seven host C cases execute the actual marker and CPU-snapshot bodies with
  architecture reads, MMIO and firmware services modeled: success, stuck UART,
  all-ones UART, blocked output after a byte, first CPU call not returning,
  unterminated text and the untraced late path. Exact poll/write counts and
  all four original call arguments/state fields are checked. These tests do
  not execute the new marker under ARM emulation or establish physical UART
  or firmware behavior. The actual ARM helper instructions and call inventory
  are audited in the selected U-Boot ELF.
- BL31, SPL/U-Boot and matched composition pass. Runtime, guard/vendor/fault,
  decode-logging, initial-cache, private exit-cache, loader and final-chain ELF
  audits pass on the selected outputs. The unchanged 272-byte cache helper
  is tied byte-for-byte to its retained ARM execution evidence.
- All 20 firmware-driver objects and both vendor C startup objects remain
  byte-identical. All 2,098 retained native/entry source hashes match, and the
  application/entry files match their verified retained archive. The selected
  firmware still excludes registered BL32/RMM initialization before cold exit.

Only `native-bl31-final`, `native-uboot-final` and `native-composition-final`
under `out/nano-loader-progress-20261106` are selected. The FIT is 8,193,712
bytes. Its chain is
`66e99a26fe7bbfbc56d939423b1dd361026cdbf2bef103f3a51110726b5c73cf`.
The native application remains source `31d716957`; sealed FPGA inputs remain
unchanged. Composition holds the normal-user shared Quartus lock and performs
file conversion only. No board programming, upload or SD write occurred in
this milestone; this new compiled input has zero hardware attempts.

TRWU8CBL remains claimed after the preceding stopped test. The spare stays in
place, factory restoration is waived, and the bottom kit/factory card remain
untouched. CPU/FPGA halt remains unproven. Native startup, ROM completion,
Run/return, ordinary package admission and DE10-equivalent behavior remain open.

Next freeze a loader-progress-aware operator and require all 26 receipts in
its healthy-service gate. Retain partial loader state and firmware-call
uncertainty in the original two-second receive-only failure tail. Verify/reuse
the seven catalog files, bind this FIT as the eighth upload, freshly admit
claim/device/UART/card/recovery and run one bounded attempt. Keep all existing
50/100/20/5/450/480/740/850-second and ROM16-byte/400M-tick/64-poll limits.
Stop at the first failure, retain the trace and claim, restore 15M/exact USB/
private JTAG/UART resources, and do not replay the consumed `66812745d` input.

Exact source and evidence hashes are in [result.json](result.json). The source
pack and full evidence archive are verified by import/readback under
`/home/deano/fes-retained/de25-loader-progress-20261106-r1`, with prerequisite
`de25-exit-cache-stop-20261106-r1`.
