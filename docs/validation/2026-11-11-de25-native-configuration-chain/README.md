# Prepared native configuration catalog and matched boot chain

Code `e4e695d5b`, against `7b465607c`, fixes a catalog restriction found while
selecting hardware inputs: one FPGA package can have several valid ROM choices.
The serving source reference now includes SHA-256 of the complete canonical
prepared install record, including entry ID/title, package, metadata and ROM/tape
choices. Distinct entries sharing a package receive distinct references. Duplicate
entry IDs still refuse; no old unscoped-reference fallback is added. The reference
identifies content and supplies no boot, storage or firmware authority.

The new race-enabled test produces/stages two ROM receipts for one package,
installs through the normal API, verifies distinct setup entries and alternates
launch/return with exact ROM bytes and activation receipts. Unknown/unscoped
references refuse before device calls. Existing multi-package, source schema,
whole-batch failure, FAT persistence and full Tenfoot App tests also pass.

All seven selected gates have passing test evidence. Six selected commands exit
normally. The native verifier's supervisor exited 143, while the independently
recorded child PID/start ticks stayed live. That same child was observed through
its terminal passed manifest and PASS log, with matching frozen source hashes;
it was not restarted. Its exit status could not be reaped after reparenting. The
original 850-second deadline remained in force, and terminal evidence was present
within it. The interruption and observation report are retained separately.
Parent regressions pass 1,935 tests with three skips. The actual AArch64 TamaGo
cross-build and source checks pass. Counts and hashes are in `result.json`.

The retained actual ZX81/ZX80 exports pass the host staging producer and the
current compiled loader preflight. The verifier's firmware caller rejects all
calls; the observed count is zero. An older shared-package export correctly
fails on obsolete metadata. Its failed result is preserved. A new export derives
current metadata from the SDK maps and independent retained-linker ROM receipts;
that separately passes compiled platform preflight. Its alternate firmware is a
ROM-loop diagnostic and is not proposed as the second playable hardware core.

Matched entry, secure firmware, U-Boot/SPL/FIT and HPS-first SOF preparation passes
for the current code. The FPGA core payload remains unchanged. The FIT keeps the
existing 8 MiB bound with 9,576 bytes spare. An earlier `7b465607c` chain is retained
as a superseded candidate. No programming grant or board state is adopted from it.

The current staging directory contains both actual cores, the bounded tape
fixture, current menu and empty bootstrap controls. Fourteen fixed upload roles
are individually hash/length checked, with the optional catalog last. Existing
installed records are preserved; an empty bootstrap does not imply an empty card.
The plan does not execute and confers no live admission. The two-install trial
driver, fresh kit/card/source/transport admission and bounded physical run remain
to be prepared. No UART/JTAG open, upload, SD write or board programming occurred.
Native Ethernet, broader core/device coverage and full DE10 appliance parity
remain open. Artifacts and rejected/superseded evidence are retained at the path
in `result.json`.
