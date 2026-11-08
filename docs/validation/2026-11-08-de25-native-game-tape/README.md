# Initial game tape through the native launch owner

The existing paired board test launches two verified computer packages and
returns to the FPGA menu. This change supplies the missing initial game-media
path: a canonical program choice may select a tape separately from its firmware
ROM, and the existing Tenfoot catalog/backend carries that choice into launch.
Only the native computer profile with the declared tape interface admits it.

The store reads exact size/SHA-256 bytes under the prepared SD lease, closes the
file and keeps a private copy before containment. Loader preflight enforces the
board driver's 1..16,375-byte range. After configuration and held CPU samples,
the existing media driver sends chunk data and CRC, checks completion, and the
service checks held fetches again before Run. The original timing, ownership,
ROM, configuration and return checks remain active. Failure after containment
retires the owner without Run, SD resume or replay. Running status binds the
tape hash/size to that generation; return clears it.

The uploader adds one fixed destination, `tape` → `FES/GAME.P`, bounded at
16,375 bytes. It uses the existing recovery card identity, memory ledger,
network transfer and fresh readback path. This change issues no card write or
device command. Both native serial observers require a complete tape receipt,
its selected hash/size, the native computer identity and a live generation.

Tests use actual archive/ROM retention, compatibility, configuration, the C
publication decoder, computer/media driver and Tenfoot App. SD, secure calls,
mailbox and time are injected. They exercise private retained bytes after card
mutation, file closure, 1/1,025/16,375-byte deliveries, odd padding/multiple chunks,
fresh launch/return and cleared receipts. Rejections cover missing/changed data,
size/profile/menu restrictions and malformed choices. Lost writes, rejected
commit, ownership changes, cancellation, elapsed limits and activity while held
prevent Run; repeated requests cannot replay a retired transaction. The actual
App also surfaces a failed tape launch.

These are software results. They do not prove physical media transfer, ROM
consumption, visible output or normal appliance acceptance. The changed Go code
requires a newly matched SDK/entry/firmware/boot chain before any new board test.
The preceding [paired-cycle hardware pass](../2026-11-08-de25-verified-files-board-trial/hardware.md)
belongs to its older compiled source and remains independently retained.
