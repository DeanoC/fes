# Fresh native SD scratch-write diagnostic

Base: `12ecb6e2e`; compiled writer `0db693965`, native build
`1aad09d24ae28f1c`. This procedure uses the already prepared, fully checked
real-spare SDK/entry/SD-only DDR BL31/U-Boot/SOF. No production bytes changed.
The preceding seven selected software checks remain applicable to those bytes.
The new private operator models pass its actual decoder wiring, nine negative
upload/lineage cases and 25 native-success/recovery-lineage negative fields.
They also exercise the actual programming wrapper's three distinct fuse names
and their mapping to the existing public recovery/native policy roles.

The sequence is one qualified recovery boot, one fixed `FES/UPLOAD.BIN`
Ethernet upload and fresh SD readback, then one matched native boot. Native
verifies the expected CID/capacity, MBR layout/hash and original tail hash
before writing exactly eight sectors, 7,774,200–7,774,207. It reads back,
contains, resumes, reads again and ends SD-contained. The native decoder
requires every ordered phase with the matched owner, generation and pattern.

Only a complete successful native receipt and final containment permit the
separately consumed `readback` programming operation. It boots the qualified
recovery loader again, then independently reads the eight sectors and checks
the protected prefix, MBR, recovery FIT and recovery manifest before and after.
The latter stage uses a fresh volatile boot marker and live memory/LMB ledger.
It sends no SD write or network command and records that `mmcinfo` observes
the card summary, not its full CID. The recovery receipt is bound to the exact
native receipt. Neither recovery boot is a retry after a failed gate.

`operator-source.json` freezes 2,409 checked files and three complete eligible
source trees. The existing public `ProgramAttempt` contract is unchanged:
logical operation names `recovery`, `native` and `readback` have separate
durable scan/consume paths, with public roles recovery/native/recovery.
Each operation requires a fresh vendor forced scan and consumed-once grant.
Prior native programmings of this new writer image are zero. Historical
transport failures remain two; there have been none since the qualified repair.

Native operation bounds remain 90 seconds to program, 50 for the loader prompt
and 100 for the diagnostic. Each recovery boot retains its 240-second bound;
readback has a separate 150-second bound. The new five-stage sequence has an
850-second outer bound and a host cap below 900 seconds. The first failed gate
stops all subsequent board commands and preserves a two-second receive-only
tail where a console is open. Partial native execution is reported as a
possible/unknown write; a missing WRITTEN record never establishes no write.

Only top TRWU8CBL and its installed disposable spare are selected. Its original
factory card remains safe, factory restoration is waived, and the bottom kit
is excluded. Qualified fixed 3 MHz/auto-off transport, the top-filtered private
server, uid-1000 vendor tools and shared Quartus lock remain required. The host
must restore exact original clock/auto settings and USB permissions, stop its
private service and release the UART. A reconnect stops the attempt. Old-output
cleanup remains paused.

Preparation itself creates no live authority. A clean committed source,
fresh 60-second USB stability observation and full current file/tool/claim/UART
admission are still required before starting. No hardware result, native write
success, native FAT persistence, normal core install, native Ethernet or full
appliance acceptance is predeclared. Raw helpers and evidence live under
`out/nano-sd-write-trial-20261110/`.
