# Direct control-IN NAK correction

The consumed USB probe boot `0d772a49dead9616` reached the addressed root hub
and stopped on accumulated NAK plus valid data/completion while retiring a
configuration-header control read. Its raw evidence and source are retained in
`/home/deano/fes-retained/de25-usb-runtime-board-stop-20261108-r1`.

In slave/PIO mode several direct control-IN tokens may be queued for the same
packet. The polling path now acknowledges/counts NAK as a token notification,
matching the pinned Linux v6.12 slave control-IN mask and continuation semantics.
It does not latch a terminal NAK or initiate retirement from that notification.
No SETUP, HCTSIZ, expected PID, buffer or deadline is reinitialized. Real
expected-PID data, transfer completion and observed channel retirement remain
required. Fatal errors, source/link/PIO ownership and periodic/split/OUT NAK
semantics remain. No Linux driver is linked.

Four new Go tests reproduce NAK before/with data and during retirement, then
withhold data, completion or retirement and exercise wrong PID, fatal status,
foreign DMA, disconnect, unexplained halt and the unchanged deadline. Periodic
and split data/NAK contradictions still reject. The regression failed on the
previous driver; the final driver/USB ownership race tests pass. Six affected
checks pass, including 1,719 parent tests (three existing skips) and generated
consumer consistency.

A fresh native SDK/entry/BL31/U-Boot/FIT/SPL/SOF chain is built; full source,
artifact and embedded-SPL joins pass with unchanged FPGA core bytes. Exact
identities and hashes are in result.json. Composition held the shared Quartus
lock. The changed image has no physical acceptance; the consumed image remains
closed. Next is new one-attempt live admission and USB Ready/Stop qualification,
then the combined initial-game-tape/core launch/return path. Ordinary core-load
and full appliance acceptance remain open.
