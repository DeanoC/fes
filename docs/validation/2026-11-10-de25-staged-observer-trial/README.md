# Bounded keyboard test after the host observer repair

This is a fresh, single-attempt procedure using the materially repaired host
observer from `a63d46d90`. It selects the unchanged matched native boot
`a02d808ea11eb54f`, compiled from `db5fc2d66`, on top TRWU8CBL only. The
previous attempt remains terminal and failed; none of its captures, cold-boot
proofs, scans or consumed grants can authorize a new device operation.

The earlier wording that retired the consumed image applies to that attempt's
authority. Reusing the verified boot bytes here is explicit: no native or
firmware defect was established, and the host decoder rejected a legitimate
held selected-plan snapshot. This new procedure records one prior native
reservation, one completed programming and one failed host capture. It does
not reset those counts or add a producer nonce. A checked history contract
requires that exact failure, passing programming/cold proof, independent
cleanup, fully retained evidence and the tested decoder repair. A different
failure, failed programming, changed native bytes or missing repair rejects.

All 44 operator-model tests pass, including the actual injected serial runner,
physical keyboard Stop without a UART substitute, firmware/RAM/MMU decoders,
stepped receipts, clock restoration, fresh programming policy and history
mutations. The earlier repair also passes 48 focused tests and all six parent
checks with 1,851 regressions. Fresh file admission covers 5,669 files/eight
trees; the top USB/UART identity is stable for 60 seconds. These are preparation
checks and grant no hardware authority. A committed exact-source live preflight
must independently recheck files, ownership, tools, unique top transport and a
free UART before the attempt.

The bounded sequence remains menu → ZX81 plus the 917-byte tape → physical
Ctrl+Alt+Escape → menu → ZX80 → owned Quit → menu/final Park. Five FPGA
configurations and three menu sessions must join exact package/ROM/tape and
ownership evidence. The physical Stop must have fresh USB report progress
and its original logical generation; no UART replacement is permitted.
The operator confirms availability at the kit and receives a separate signal
only after the verified first launch. Visible transitions require a separate
operator observation.

Bounds remain human wait 60 seconds, core close 5, menu restore 100, initial
Ready 100, native 450, outer 800 and host 890. First failure closes the attempt
with a two-second passive tail and no retry. Every hardware operation uses a
new private top-only transport, fresh scan and new one-use grant under the
shared Quartus lock, fixed 3 MHz/automatic-adjustment-off, then restores the
original 15 MHz/automatic-adjustment-on and exact USB permissions. Vendor
tools run as deano. Historical programming failures remain two, with none
since the qualified transport repair.

Only the installed spare card is used; the factory card stays protected and
restoration remains waived. Recovery performs the existing 13 fixed Ethernet
uploads and fresh SD readbacks. Native Ethernet and native SD writes stay
disabled, and preferences remain volatile. The bottom kit is excluded.
Old-output cleanup remains paused. No pass, containment, visible return or
ordinary appliance acceptance is predeclared.
