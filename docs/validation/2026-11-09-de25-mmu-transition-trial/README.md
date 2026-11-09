# Bounded MMU transition trace trial

The [matched format 3 candidate](../2026-11-09-de25-mmu-transition/README.md)
is `59e96369a749fc7d`, compiled from `6a247d75dde1019e7f6db1707029b0ac996177a7`.
It adds 26 ordered cold-only MMU checkpoints. Engine, CPU/owner/memory/tape
implementation bytes and the one-second tape deadline remain unchanged.

The consumed preceding image reached provider success with SCR `0x238`, then
stopped after the MMU before record. Its subsequent progress is unknown.
That complete failure is retained; this genuinely changed image and one-use
procedure do not retry or reset a previous reservation. Both preceding phase
reservations and the original mode-loss reservation remain hash-bound.

Only exclusively assigned top TRWU8CBL is in scope. The mode-0775 probe is
checked before reservation and at each live observation; each recovery/native
operation needs its own durable reservation and fresh forced exact-device scan.
A completed recovery precedes thirteen fixed uploads and independent fresh SD
reads. The native loader requires successful provider setup, both MMU register
records, all 26 steps and completed private-memory readback before launch.
A complete run still requires three launches, four menu sessions, seven
configurations and final Park. The first failure stops application commands.

Bounds remain recovery process270, upload300/process320, native450/process480,
overall800/host890 seconds, plus the existing two-second passive failure tail.
Host cleanup restores the recorded clock and USB permissions and stops only the
owned private server. UART Select supplies actions; no physical key is requested.
The spare card stays installed; factory restoration remains waived. Bottom
TRWU8J8G is outside scope. No new hardware result exists at preparation time;
software/QEMU checks do not establish cache, physical display/input, ordinary
core loading or appliance acceptance.
