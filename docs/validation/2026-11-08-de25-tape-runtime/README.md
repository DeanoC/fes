# DE25 native initial-tape selection and matched boot chain

Source `211ab49464df5ed93c31f33312a6567f2c4b32e3`, based on `8f4769d8805fb5a7c241232e63f876134a844c0f`. The eligible Nano CPU transport now
joins the fixed configuration arena to the actual secure C tape service. SDK,
entry and firmware profiles bind the same explicit initial-tape selection.
Incomplete resource sets or mismatched compiled/linked selections reject.
Selected failures retain containment and produce one cached firmware diagnostic.

All 11 affected checks pass: 1,706 parent tests (three existing skips), 1,595
FPGA software tests (79 skips), native launch/return/failure integration and
producer/RTL checks. Focused checks pass 43 selection tests, three board-hook
race tests with six actual C-service subcases, and 38 strong provider scenarios.
Committed-source `make check` passes 52 generated consumers and 37 fixture copies.
No shared protocol values or generated definitions changed.

A fresh matched SDK, entry, BL31, U-Boot/FIT/SPL and HPS-first SOF were built.
Composition held the shared Quartus lock. Post-build checks confirm 2,259 source
and artifact hashes, identical boot profiles, the actual embedded SPL, and an
unchanged FPGA core payload. The secure private segment ends at `0x5e200`,
leaving 7,680 bytes before the original `0x60000` limit.

An additional independent full context-derivation audit timed out at 180 seconds
and supplies no passing result. The composition producer completed its full
native/context checks; the later integrity check verifies recorded inputs and
files without repeating that derivation. All unsuccessful helper logs remain.
The runtime source digest closure matches this commit, while its embedded Go VCS
metadata records a different revision and modified=true; this is not described
as a clean-HEAD build.

TRWU8CBL was untouched. The original one-second tape bound is unchanged.
Next is retained evidence and a fresh bounded trial on these changed bytes.
The preceding ba2f trial remains consumed. Physical tape consumption, visible
launcher/core transitions, input and ordinary appliance acceptance remain open.
See [result.json](result.json) for exact hashes, checks and evidence boundaries.
