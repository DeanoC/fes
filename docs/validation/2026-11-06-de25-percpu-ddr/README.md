# Initialized per-CPU DDR and firmware reservation

Implementation `718b5c1427cecce2dc64868de8a9df7b599abf3c` follows the
[consumed affinity-call trial](../2026-11-06-de25-affinity-call-trial/README.md).
That trial reached the secure affinity handler and its `cache-before` receipt,
then stopped without a cache return. It narrowed the interval but did not
identify a faulting instruction or prove that `DC CIVAC` itself executed.

This repair moves only the 256-byte per-CPU array from private OCRAM BSS into
initialized DDR `.data`. The original PSCI object, CPU-index object and
`DC CIVAC` operation remain byte-identical before relocation. It adds no
runtime whole-cache operation and skips no maintenance. The previous plan to
replace the runtime operation is superseded by this smaller data-placement
repair, which avoids introducing shared-cache work during active loader use.

The selected array is at `0x80036000`; its first affinity state is at
`0x80036018`. All 256 bytes are zero and file-backed in one writable ELF load
segment. SPL loads and hashes those initialized bytes. Six independent C
assertions bind count, stride, size, alignment, member offset and member size
to the selected firmware types. The producer rejects aliasing with other
sized symbols, NOBITS placement, nonzero initialization, missing writable
file-backed coverage and placement outside the selected DDR image.

The firmware-RAM contract is version 4. Private BSS ends at `0x5ddc0`, exactly
256 bytes earlier. The existing stack region `0x50000..0x58000` and native
exclusion `0x50000..0x60000` stay unchanged. Both the loader DTB and the DTB
inside the FIT reserve `0x80000000..0x80200000` with `no-map`. Before its first
CPU query, the loader requires exactly one matching `LMB_NOMAP` reservation;
partial, duplicate, broader, overlapping, wrong-flag and invalid ranges fail.
This explicit reservation is new; the older loader DTB did not contain it.
The admission gate runs before the memory receipt and initial CPU snapshot.

Validation:

- Three placement methods, nine firmware-selection methods, fourteen handoff/
  reservation methods and three loader methods pass. Version 3 and the old
  BSS endpoint are rejected. ELF entry and entrypoint-symbol identity are
  checked together.
- Five ARM cases pass. Actual selected inline SMC assembly crosses emulated
  EL2 to EL3 and exercises the selected PSCI transformation and diagnostic
  leaf. The file-backed array starts zero; clearing a separately linked BSS
  leaves an initialized-array sentinel intact. Eight traced queries and
  unrelated-FID/level/affinity controls pass on A53/max; modeled nonreturning
  cache and stuck/all-ones UART cases remain failures. Executed ELF hashes
  are checked before and after. UART addresses, CPU-data helpers, cache
  callback and dispatch/vector are modeled; physical cache behavior remains
  unqualified.
- The combined affected run hit its unchanged 900-second cap during the last
  FPGA tooling step. Its preceding nine sequential steps completed, including
  1,568 parent tests with three skips. The final step was then run separately
  under a 900-second cap: 1,581 FPGA tooling tests pass with 75 skips. The
  original aggregate remains recorded as a timeout, not a passing run.
- BL31, SPL/U-Boot and matched file composition pass. Audits check the actual
  initialized ELF/flat-image bytes, original PSCI/cache objects, linked CPU
  address calculation, both compiled DTB reservations and the reservation
  gate's compiled position before memory/CPU proof. Existing call, loader,
  runtime, initial/exit cache, exception, guard and final-chain checks pass.
- The first placement audit assumed a particular disassembler symbol alias;
  its correction verifies the numeric target and the displayed alias's ELF
  symbol address. A copied chain report retained the previous BSS endpoint;
  the selected correction uses the version-4 contract. The intermediate
  helper-name error and all failed audit logs remain in retained evidence.
- All twenty existing firmware driver objects, both vendor C startup objects,
  all 2,098 native/entry source hashes and retained application/entry bytes
  remain identical. The sealed FPGA payload is unchanged.

Only the `native-bl31-final`, `native-uboot-final` and
`native-composition-final` outputs under `out/nano-percpu-ddr-20261106` are
selected. The FIT contains 8,194,608 bytes; its chain identity is
`d254071d002ee79af752344db3d9114637ba10a96c3455099d61222327c5a051`. Composition held the normal-user shared Quartus lock and
converted files only. No programming, upload or SD write occurred in this
milestone. Physical DDR reservation/cache behavior, ordinary package admission
and DE10-equivalent core-loading acceptance remain incomplete.

The user's subsequent power interruption is recorded. Exact TRWU8CBL USB
identity is visible, but all previous live admission is discarded. The spare
card and continuing claim are retained; the factory card and bottom kit are
untouched. The next operator must bind this new format-4 chain, require all
48 CALL, 26 LOADER, 31 RUNTIME and both EXIT_CACHE receipts, and freshly admit
claim/device/UART/recovery/card before one bounded test. Keep every existing
50/100/20/5/450/480/740/850-second and ROM16-byte/400M-tick/64-poll bound.
The first failure is terminal; restore 15M/exact USB/private JTAG/UART and keep
the trace. The consumed `23db3f0d5` input is not replayed.

Exact source/evidence hashes are in [result.json](result.json). Retention
uses `/home/deano/fes-retained/de25-percpu-ddr-20261106-r1`, with prerequisite
`de25-affinity-call-stop-20261106-r1`.
