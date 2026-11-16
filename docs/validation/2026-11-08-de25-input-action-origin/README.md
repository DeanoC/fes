# Native input-action origin diagnostic

The preceding keyboard/tape trial reached boot-keyboard Ready and two menu
frames, then began a launch before the serial controller accepted startup.
Only `fesboot` followed by CR had been sent. One USB report was counted, but its
contents were not recorded, so neither USB nor UART is established as the
cause. The user later borrowed the keyboard; its timing relative to the trace
is unknown. Boot76cd915c046abe45 remains failed and consumed1/max1. Its complete
5,812-member archive was read back and its source bundle imported/verified at
`/home/deano/fes-retained/de25-keyboard-poll-board-result-20261108-r1`.

Source930775b1d9ff9700776502c7db9bbb7b8955fb81 adds bounded diagnostic records
for decoded Select, Stop and Quit, identifying UART, USB boot-keyboard or USB
gamepad origin and current service state/owner/generation before App delivery.
It records no printable text or raw report and performs no extra device read.
Buffered menu actions report once at delivery; queued UART Quit records owned
Stop and later Quit separately. Emission failure prevents frame delivery.
These observations prove neither App acceptance nor lifecycle completion and
supply no device authority. Input arming and deadlines are unchanged.

The actual automated startup loop now rejects an unsolicited lifecycle-action
marker before initial Ready or any collector App command. Five host tests run
that loop with real model readiness and injected bytes/clock, covering all three
origins/actions, split packets, malformed markers and its original100s bound.
Native runtime/input race tests pass, including full App launch/return/failure
cycles and strict origin observations. Seven selected parent checks pass;
the full suite passes1,724 tests with three existing skips. Nineteen lifecycle
fixture cases and six stale/forged worker-prefix refusals pass. The initial
host fixture/plan/dependency-order errors are retained beside their repairs.

Matched boot69a03065693442b5 rebuilt SDK, entry, BL31, U-Boot and composition.
Every producer phase and the matched-chain job passed within its original cap.
The redundant aggregate monitor covering SDK plus matched-chain was stopped;
its independently bounded producer chain continued with no phase interruption
or wider deadline. The actual firmware ELF/bin and load segments pass the
original OCRAM contract. The FIT stays within8MiB; original CPU, reader, source,
owner and drain contracts are unchanged. Complete native-file admission checks
5,525 files/eight trees, with fresh worker hashing and no cached live authority.

The new image has zero hardware attempts. Its full thirteen-file network upload,
readback and paired menu/core/tape procedure is frozen separately. Keyboard
return, current read-only kit admission and one-use live scans are still required
before hardware execution. This is host preparation; core-load, physical input,
HDMI and normal appliance acceptance remain open.
