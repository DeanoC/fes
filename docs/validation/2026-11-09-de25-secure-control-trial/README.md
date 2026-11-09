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


The trial is terminal and retired. Recovery/native programming and all 13
Ethernet uploads/fresh SD readbacks passed. The selected control views now
permit Tenfoot Ready (58.29 seconds), four submitted menu frames with zero
reported underflows, owned window access, reader drain and menu closure.
The SMMU inventories report 26 reads and secure-bank ID `0x80000018`. Both
menu and ZX81 FPGA images completed configuration and activation. No HDMI
capture or physical input acceptance is claimed.

The first Enter/launch request then failed in the initial tape transfer.
The tape engine reports LIMIT (-6), 917 requested bytes and the last completed
chunk at 512 bytes, with no successful tape completion. Its last accepted clock
sample is 0.9999786225 seconds after origin; the failing clock sample is not
printed. LIMIT is shared by clock and polling limits, so the exact rejection
sample is not directly observed. The App reports reboot_required with resources
held and a ROM activation receipt. Firmware reports successful failure
containment. No running-core or normal core-return cycle was established.
The original two-second passive tail was retained, with no further command.

Independent cleanup passed: actual USB permissions match, the owned private
server is gone, UART is free, and the frozen transport's original 15 MHz /
auto-adjust 1 readback is recorded. The cleanup wrapper stopped at the host disk
floor; its unchanged read-only audit was executed separately without device
operations. Both programming operations are consumed, with two historical
transport failures and no new programmer failure. The host is detached;
firmware terminal return or CPU park was not observed. Top spare remains
installed, original card safe, exclusive claim/restore waiver continue and
bottom TRWU8J8G was untouched.

The raw console, terminal receipts, cleanup proof, operation reservations and
replay sources are preserved here. Full candidate and prepared archives were
verified before hardware use. The final full terminal archive is still pending:
host free space fell below 30 GiB despite reclaiming archived inactive fixtures
and source copies. Keep that gate and finish retention before any producer
change or further hardware operation. The image must not be replayed.

Sampled ownership checks average about 67 microseconds. Their state checks
before and after average 21.17 microseconds each; USB validation averages 17.31
microseconds, versus 0.67 microseconds for the sampled reset-register checks.
These systematic samples are estimates, not an exhaustive profile. Exact ELF
inspection places the MMU state at `0x5c780`, mapped secure Normal NC, and shows
a 112-byte stack frame with a 48-byte callback-table memcpy followed by another
48-byte copy on every platform-scope call. This is a concrete next optimization
target: pass one static const operations table by pointer and place only the MMU
state in existing private initialized DDR data. Preserve every fresh register
observation, guard and failure latch, other BSS/stack mappings, the same firmware
exclusion and the one-second tape budget. No such patch, build or hardware trial
has been applied yet.


Terminal retention is now complete on `chip-30x0-ai-dev`, at
`/data/projects/de25-embedded-retained-20261109/terminal`. An isolated checkout
matches result source `8c82746a7adc3d4ac07b6584faef5057acbdde91`. All 88796
files in the original retainer closure, plus analysis and housekeeping audits,
were transferred and checked for exact bytes/modes. The new archive was fully
read back. Its standalone Git bundle was independently restored with no
alternates, full strict fsck and pack verification. The previously verified
candidate, prepared and transport archives were copied with unchanged hashes.
This changes the storage location, adds no hardware action and leaves the
retired image, all guards and the goal's remaining acceptance requirements as
recorded. See `remote-terminal-retention.json` for the hashes and source IDs.
