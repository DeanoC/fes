# One bounded two-bank fault diagnostic on TRWU8CBL

Fresh operator for firmwareb42b86e14/native31d716957. The sibling validation
binds the145 ARM cases and matched image. Candidate proof requires43 fixed
reads,20 RAS2 read/readback actions, exactly three ERRSELR writes with fixed0/1
selection and saved-selector restoration, barriers and exact skip targets.
Detection/status/interrupt controls remain read-only. Primary map ZIP retained.

Five fixture groups,24 source/provenance rejections and17 device-admission cases
pass. The outer model additionally rejects nonempty, missing or invalid RAS2
states, observed originals, uncertain/restored selector claims and unknown keys
in otherwise-successful startup.29 helper files are frozen. All fixtures are
host-only; they open no devices. New b42 input has zero hardware attempts.

Prior14 campaign stops remain retained: one programmer failure, two host setup
stops, four ROM deadlines and seven pre-native firmware stops. Do not replay
consumed1e8d. One fresh attempt; stop first failure without retry/fallback.
Original exception is fatal even if every record and selector restore completes.
A failed selector operation/readback may leave selector restoration unproved;
park-only containment and retained claim remain required. Do not enable error
detection on uninitialized nodes or acknowledge error status.

Read-only live preflight verifies the continuing top claim, exact hashes/USB,
free UART and disk. Hold the normal-user shared Quartus lock and admit chip
4362C0DD at6M before recovery programming. Eight fixed uploads and fresh SD
readbacks precede native boot. Keep prompt50/startup100/launch100/config20/
control5/native450/child480/work740/host850 and ROM16-byte/1-second/64-poll bounds.
A healthy startup must have guard ENTRY/PASS, all15 vendor markers, no exception,
empty PROBE/RAS2 states and the existing ownership proofs. If it succeeds, all
three Tenfoot ROM launch/return/Quit cycles with matching receipts, USB reports
and pause/resume proofs are still required. No physical control action is needed.

Afterwards restore/read back15M, exact USB permissions, stop private JTAG and
independently verify free UART. Retain claim/spare, leave factory card/bottom kit
untouched; factory restore is user-waived. Cleanup is not CPU/FPGA halt proof.
This freeze precedes live admission/hardware evidence. No ordinary admission,
HDMI, native Ethernet or full DE10-equivalent core acceptance is granted here.


## Physical result

One attempt on operator93802ba41/firmwareb42b86e14 completed recovery,
eight fixed uploads/fresh SD readbacks, exact6M chip admission/programming and
SPL FIT verification. Early ENTRY/PASS and eight ordered vendor markers were
followed by the same EL3SError: ESRbe000011, savedPC8001b1f8 atDCIVACx0,
r0=50240/end5dec0/line40hex. The asynchronous saved PC/r0 still do not prove
the causing instruction or transaction. Original exception remains fatal.

All43 fixed reads and20 RAS2 reads/readbacks completed. Both fixed selections
and original-selector0 restoration were observed, with independent raw replay
matching the runner. Both records returned FRa9a6, CTLR0, status0 before/after
and MISC0. DSU/OCRAM CCU detection and interrupt controls all read0. The core
ED0 disables detection/correction on reads; the DSU TRM describes shared-record
ED0 as disabling reporting/logging while detection/correction continues. Their
clear status therefore cannot rule out a fault. No control or status was changed
except the three qualified selector writes, and the selector was restored.

Active commands stopped at the first exception, followed only by passive drain
through RAS2 END. No fesboot command, BSS/stack/setup completion, native startup,
native CPU exclusion, ROM completion, Run or return was reached. No retry.
Independent15M readback/exactUSB/privateJTAGgone/UARTfree cleanup passed. Claim/
testcard retained; factory card and bottom kit untouched. CPU/FPGA halt remains
unproved. There are now15 campaign stops, including eight pre-native stops;
b42 has consumed one native attempt. The stopped input is fused.

The initial report generator retained its predecessor's39-read ceiling and a
stale versioned fixture filename; those two failed offline runs and their
sources remain saved. Corrected v3 resolves the actual completed receipts and
checks43+20 observations. Its independent replay and the v2 interpretation pass.
No hardware action was repeated to fix report generation.

Next prototype controlled CCU decode-error logging around the original vendor
cache operation, with strict baseline/readback/restore checks and hardware-free
success/failure tests first. Do not enable cache/ECC detection on an uninitialized
node, change interrupts, clear status, bypass maintenance or widen bounds.
The [DSU TRM](https://documentation-service.arm.com/static/5e7e1bd8b2608e4d7f0a35b4)
B2.3/B2.10 distinguishes reporting controls from detection and valid status.
