# Bounded secure-control hardware trial

Fresh matched image `bd103b1a199bc2be`, compiled source
`599ffbdb07291fa6ab2b0e5f2f64d58a4c9e2888`, replaces the remapper-only helper
with pinned BL31 MMIO address selection. Existing System Manager page accesses
reuse its secure RW alias after MMU activation; existing SMMU secure-bank reads
use one new secure RO alias, with writes left on the original physical path.
Pre-MMU and unrelated addresses remain. Original operations, values, widths,
offset allowlists, ownership/CPU/master/window guards and all bounds remain.
Both producer inventories and final installed headers bind the repair. Full
linked selectors and actual production tables pass emulated checks, including
negative controls. This has no new physical acceptance yet.

Previous image `e3de630956f0c154` and every operation remain terminal,
consumed and retired. That image completed the menu FPGA configuration and
CONFIG_STATUS ready, then stopped after SMMU/firewall reports without a first
menu frame. The exact stalled instruction is unproven. Its zero secure-bank
sample cannot satisfy the unchanged later gate. Do not replay that image or
infer that the current repair resolves the physical stall.

Use only the continuing exclusive claim on top TRWU8CBL, spare card installed
and original factory card safe. The user's restore waiver continues; bottom
TRWU8J8G is excluded. No physical input is requested. Require current admitted
files, committed operator sources, more than 30 GiB free, a stable unique USB
connection, a free UART and a verified prepared archive before any device use.
Take the shared Quartus lock as deano before sudo. Retain the qualified fixed
3 MHz / auto-adjust 0 transport and both historical programmer failures. Use
fresh durable one-use recovery/native reservations and fresh per-operation
scans; never replay an image or grant.

Require all 13 Ethernet uploads and fresh SD readbacks before native programming,
then the full native boot proof before the original seven configurations,
three launches/four menu states and tape exercise. Native 450 s, outer 800 s,
host 890 s, initial Ready 100 s and tape 1 s bounds remain. Stop on the first
failure, followed only by the original two-second passive tail. Independently
verify original 15 MHz / auto-adjust 1, exact USB permissions, our private
server gone and UART free. Retain raw console, receipts, consumed records,
source bundle and terminal archive before logging the actual kit outcome.

All six operator model groups, clock adapter checks, full candidate file
validation and the 60-second USB stability check pass. This record prepares one
hardware attempt; it does not report that attempt started or passed. A contained
cycle still cannot establish ordinary appliance readiness. Ordinary verified
package admission, visible menu/game behavior, physical input and normal return
remain part of the goal.
