# One bounded current-RAS diagnostic trial on TRWU8CBL

Fresh operator for firmware1e8d79ef4 and byte-exact retained native31d716957.
The matched chain and115 ARM cases are in the sibling RAS validation. Only the
currently selected error record is observed, with no selector or status write.
The primary register-map ZIP is retained/pinned for the unchanged39-read prefix.
Candidate proof requires actual linked RAS feature/NUM/SEL gates and ten marked
read sites. Successful startup requires no exception and empty PROBE and RAS
states, as well as all15 vendor markers and the existing boot/ownership proofs.

Five fixture groups,24 source/provenance rejections,17 device-admission cases
and the updated outer-result model pass. The latter rejects every nonempty,
invalid or missing RAS state even in an otherwise-successful report. Initial
source-fixture-v2 stopped on a temporary-directory name mismatch before source
admission; the saved failure and corrected v3 result remain in this procedure.
No device was opened by fixtures.29 operator source files are frozen below.

Prior history retains13 campaign stops: one programmer failure, two host setup
stops, four ROM deadlines and six pre-native firmware stops. New1e8d has zero
operator/programmer/native attempts. Predecessorf7e has one native failure plus
one host-only stop and is not replayed. Permit one new attempt; stop first
failure without retry/fallback. No physical controller input is needed/claimed.

Read-only live preflight rechecks exact source/artifact hashes, the continuing
top-kit claim, exact USB adapter, free UART, private JTAG ownership and disk.
Use the normal-user shared Quartus lock; at6M recheck chip4362C0DD before any
recovery programming. Eight fixed owned uploads/fresh SD readbacks precede the
native attempt. Bounds remain prompt50/startup100/launch100/config20/control5/
native450/child480/work740/host850, with unchanged ROM16-byte/1-second/64-poll
limits. Original exception stays fatal regardless of RAS END/SKIP or completion.
Passive UART drain sends no new command. If startup succeeds, all three serial
Tenfoot ROM launch/return/Quit cycles, receipts, USB/pause/resume checks remain.

Restore/read back15M, exact USB permissions, stop owned private JTAG and verify
free UART independently afterwards. Retain claim/spare card; leave factory card
and bottom kit untouched. Factory restore is user-waived. Cleanup is not CPU/
FPGA halt proof. This freeze precedes live preflight and hardware evidence; it
claims no ordinary admission, native Ethernet, HDMI or DE10-equivalent acceptance.


## Physical result and coverage limit

One native attempt on operator91bda1280/firmware1e8d79ef4 completed qualified
recovery, eight fixed uploads/fresh SD readbacks, exact6M chip/programming and
SPL FIT checks. EarlyENTRY/PASS and eight ordered vendor markers preceded the
same EL3SError: ESRbe000011, savedPC8001b1f8 atDCIVACx0, r0=50240/end5dec0.
Asynchronous saved PC/r0 do not establish the originating transaction. The
original exception stayed fatal; no fesboot command was sent.

All39 fixed reads and all10 architectural RAS reads completed, with independent
raw replay matching the capture. PFR011112222 permits RAS1, DISR0, ERRIDR2,
selector0 both times, FRa9a6, CTLR0, status0 before/after and MISC0=0. Thus the
existing core0 record is physically accessible and its selector stayed stable;
record1/shared DSU was not accessed. Cortex-A55 ERR0CTLR.ED=0 disables error
detection/correction on reads (TRM B3.3), so its zero status cannot certify health
or rule out the path. Enabling detection requires the node to be initialized;
this experiment did not write detection controls. CCU detection enables exist
and default0 in the pinned map; their live values are not known from this run.
Clear sampled CCU/OCRAM/core status does not justify a cache/ECC bypass.

The first fault stopped active commands, with passive drain through RAS END.
No BSS/stack/setup completion, native startup/CPU exclusion, ROM completion,
CPU Run or return was reached. No retry. Independent15M readback/exactUSB/
privateJTAGgone/UARTfree cleanup passed; claim/testcard retained, factory card
and bottom kit untouched. CPU/FPGA halt is not established. There are now14
campaign stops, including seven pre-native stops; new1e8d has one native attempt.

Next inspect detection-control coverage and record1. A bounded selector-only
snapshot must separately prove constant selections, readbacks, ISB ordering,
restoration on completion and contained partial failure, without status clears
or enabling detection on an uninitialized node. Preserve the original exception,
vendor cache operations and all existing limits. The stopped1e8d input is fused.
