# One bounded decode-logging boot diagnostic on TRWU8CBL

This freezes one fresh operator for firmware `b95d8d626` and the byte-identical
native application/entry from `31d716957`. The sibling [validation](../2026-11-06-de25-decode-log/README.md)
binds the matched image, 150 ARM cases and actual linked write/lifetime audits.
Only the reviewed DSU decode-logging bit may be enabled and restored, after
fresh zero baselines and CPU/reset/power containment checks. Status, interrupt
and ECC controls must remain unchanged. Original vendor cache operations remain.

The operator's healthy-boot gate requires all seven ordered logging receipts,
including verified restoration, alongside ENTRY/PASS, all 15 vendor markers,
empty exception/status/RAS states and the existing ownership proofs. Ten added
model mutations reject missing, reordered, duplicate, failed and incomplete
logging evidence. Eleven fragmented cleanup cases execute the real decoder
and actual receive-only operator functions over saved off-board ARM traces
and damaged variants. Verified restoration, idle and interrupted cleanup are
retained separately from an unknown outcome; every original exception stays
fatal. Passive capture preserves its original two-second and 1MiB bounds.
It sends no command, retries no restoration and grants no boot permission.

All upload/readback, source/provenance, private-JTAG, clock and exact-device
fixture groups pass. One preparation failure found the catalog had not yet been
copied into the new output directory; all seven catalog files are now verified
byte-identical, and the eighth upload is the matched new FIT. A failed AST check
in the new offline fixture is also retained. Neither correction touched a board.
The exact files are in [operator-source.json](operator-source.json).

Prior15 campaign stops remain retained: one programmer failure, two host setup
stops, four ROM deadlines and eight pre-native firmware stops. The new firmware
has zero physical attempts at freeze. Do not replay the consumed b42 input.

Deano reported a power interruption during host preparation. Current boot,
card, CPU and FPGA state must be established afresh; previous parked-state
observations do not qualify it. Read-only preflight first checks the continuing
top-board claim, source hashes, exact USB identity, free UART, disk and network
route. Under the normal-user shared Quartus lock, the private JTAG session must
admit exactly TRWU8CBL/chip4362C0DD at6M before recovery programming. Recovery
must establish the checked loader and spare-card/protected-file state before
eight fixed uploads and eight fresh readbacks. No card swap is assumed.

Keep prompt50/startup100/launch100/config20/control5/native450/child480/work740/
host850 limits and the ROM16-byte/400M-tick/64-poll guards. One attempt; stop on
first failure without replay/fallback. Passive fault capture includes the
logging cleanup after the fixed and RAS inventories if they complete within
the existing bound. An incomplete record leaves restoration unproved.

A healthy boot still must complete all three Tenfoot ROM launch/return/Quit
cycles, matching activation receipts, USB report and pause/resume checks.
No physical control action is required. Afterwards restore/read back15M,
exact USB permissions, stop the owned private JTAG server and independently
verify free UART. Retain claim/spare; factory card and bottom kit stay untouched.
Factory restore is user-waived. Cleanup is not CPU/FPGA halt proof.

This source freeze precedes live admission and physical evidence. It does not
qualify ordinary core loading, HDMI, native Ethernet or DE10-equivalent use.

## Physical result

One attempt on operator `7aa22f04d` / firmware `b95d8d626` completed the checked
recovery boot, eight fixed uploads, eight fresh SD readbacks, exact6M chip
admission/programming and SPL FIT verification. The recovered spare card and
loader were established afresh after the reported power interruption.

ENTRY/PASS, the expected eight vendor markers, and decode setup/zero-baseline/
enable receipts preceded the original EL3 SError (`ESR=be000011`). Its saved PC
is `8001bb38`, the linked original `DC IVAC,x0`; saved `r0=50240`, end`5dec0`.
The asynchronous saved PC and FAR still do not independently identify the
causing instruction or physical transaction.

The newly enabled diagnostic captured a valid, stable CCU status
`CAIUUESR=20030071`, location registers `50000/0`, and detection control8 with
interrupt control0. The [pinned primary register map](https://docs.altera.com/v/u/resources/864455/agilex-5-cs-and-r1-suffix-opn-hps-register-map)
decodes this as error type7 (decode), info`2003`, subtype3 (illegal DII access
type), transaction ID128. The checked zero status before and immediately after
enabling logging establishes a new error within this observation window;
status was not acknowledged. The location register matches the beginning of
the selected NOBITS range. This is stronger evidence than the earlier clear
records, but does not independently prove the exact initiating instruction.

All43 fixed observations and20 RAS2 actions completed. Both architectural
records still returned CTLR/status/MISC0 zero; original selector0 restoration
was verified. After those inventories, the firmware checked restoration of the
decode control to0 and unchanged captured error status. The two-second passive
collector retained that terminal receipt. Independent raw replay reproduces
the failure, complete inventories and restoration; an exception remains fatal.

Active commands stopped at the first exception, and `fesboot` was never sent.
No BSS, stack, C setup, native startup, ROM completion, Run or return is proved.
Independent15M readback, exact USB permissions, owned JTAG-server termination
and free-UART checks pass. Claim/spare remain retained; the factory card and
bottom kit were untouched. CPU/FPGA halt is unproved. There are now16 campaign
stops, including nine pre-native stops; this firmware input has consumed its
one physical attempt and cannot be replayed.

The unchanged pinned U-Boot memory map describes OCRAM as Device-nGnRnE and
this SPL selects data-cache-off. The vendor BL31 map describes OCRAM as
non-cacheable. Together with the new illegal-access syndrome, these support
investigating by-address PoC cache maintenance for the private OCRAM range.
These source observations do not establish the complete linked cache history
or qualify a replacement. Next prove that history/absence of cacheable aliases
and prototype an explicitly qualified non-cacheable NOBITS initialization path,
retaining DDR/warm maintenance, ownership, fault reporting, ECC and all limits.
A blind cache-operation skip is not established by this result.

Exact evidence: [physical result](physical-result.json),
[field/source interpretation](status-diagnosis.json), and
[independent cleanup](cleanup-evidence.json). The complete source, failed
preparation logs, raw traces, receipts and verified archive are retained at
`/home/deano/fes-retained/de25-decode-log-stop-20261106-r1/`, with the matched
build prerequisite at `de25-decode-log-20261106-r1`.
