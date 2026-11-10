# Corrected bounded native installer trial

The preceding [attempt](../2026-11-11-de25-native-installer-controller/hardware-result.json)
booted recovery successfully and stopped before any SD write because the host
parser rejected U-Boot's same-line MMC progress/completion record. The captured
response now passes upload and fresh-read regression tests; wrong device,
block, count, duplicate/error completions and trailing output still reject.
The original failed trace, archive and source remain retained without replay.

Source `96c5a5b91` passes all six selected software gates. The same native image
`c491ac67743d3526` and compiled source `49752d8b2` remain selected. Recovery was
regenerated through its normal producer with current host bindings; its FIT,
SPL, SOF, protected manifest and prefix remain byte-identical.

The separately frozen operator admits ten fixed uploads, manifest last, then
an empty native library and ordinary Tenfoot Install, Library, one ZX81 launch
and final owned Quit. Model flow, injected actual UART exchange and saved-wire
receipt replay pass. Full native file admission covers 5,639 files/eight trees.
Existing phase limits, qualified private 3MHz/auto0 JTAG, shared lock, continuing
top-only claim, fresh transport/UART checks and single-use grants remain in
force. First failure stops with no retry; exact clock/USB/server/UART cleanup
is retained. A fresh discovery boot is a separate experiment, so this first
install attempt cannot establish persistence or full appliance acceptance.

Only TRWU8CBL and its installed spare card are selected. No physical keyboard
action is needed. Native Ethernet stays disabled and physical display
verification remains separate. Factory restoration is waived; its original
media stays protected. Cleanup of old output remains paused.


The [physical result](hardware-result.json) at source `4c09f3ba6` passed recovery
programming/boot and all ten fixed uploads plus ten independent fresh SD
readbacks. The corrected MMC parser accepted the real combined completion,
and protected recovery files stayed verified. These are upload/readback results,
not native boot or installer acceptance.

Native programming then stopped with Quartus errors 213019/code87 and 213002.
The pinned vendor AJI header calls 87 `AJI_BAD_JTAG_CHAIN`; it does not establish
the physical cause. The preceding locked forced scan passed with one
4362C0DD device, IR10/mask0, 3MHz and auto-adjustment0. The programmer trace
contains no SOF file open before the error. No `fesboot` was sent; installation,
launch and persistence were not attempted. The original first-failure capture
and two-second receive-only tail remain terminal. No retry or grant reuse was
performed. Further programming requires a transport fix.

Clock readbacks returned to15MHz/auto1. Independent host inspection confirms
exact USB permissions, private-server exit and complete root UART release.
The last qualified recovery prompt preceded the failed native programming;
subsequent FPGA/CPU state is unproven. No native Park/containment is inferred.
The [retention proof](retention-proof.json) covers167 archived files, full
archive readback, an independently restored source, strict fsck and pack checks,
with the earlier native candidate capsule declared as a dependency. Top-kit
ownership remains with this task; the bottom kit and factory card are untouched.

The next bounded investigation must explain the difference between the passing
locked probe and the programmer's own failing scan, using the retained traces
and unchanged vendor-client identities before any new programming attempt.
