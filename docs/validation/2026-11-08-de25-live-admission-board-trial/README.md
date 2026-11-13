# Bounded native cycle with one-use live JTAG admission

The [physical trial](hardware.md) passed three launches and two menu returns,
then stopped at the native worker limit during final Quit. Its original result
remains failed; final Park and the full cycle are unproven. The procedure is
consumed and must not replay.

This procedure tests the changed host transport contract on TRWU8CBL. Each
recovery/native image operation reserves a distinct attempt before its live
vendor-client scan, consumes the verified proof before programming and records
the actual programmer's startup. The trace file is owned by the programmer
account with mode 0600 before that account starts. A real root-to-UID1000
host-only trace run verifies this handoff without invoking Quartus or devices.

The previous two native-image attempts remain consumed. This changed procedure
allows one additional attempt, for a maximum total of three. It repairs the
admission lifetime and trace-file access; it does not claim to explain error 87.
The matched native image is unchanged: producer `f8653867f`, boot identity
`aa11ddf99761b275`. No FPGA compilation, firmware rebuild or SD image reburn is
needed. The top board retains its spare card and continuing claim; factory
restoration remains waived by the user's instruction. The bottom kit is excluded.

[operator-source.json](operator-source.json) binds every device/validation helper,
the canonical admission policy, vendor probe and installed tools, test receipts,
prior failed trial and unchanged compiled artifacts.
[image-readmission.json](image-readmission.json) preserves the producer identity
and prior counts. Before programming, the root preflight must verify the clean
committed source, complete input hashes, owned claim, unique top USB identity,
free UART and current matched package/firmware/memory contracts. The complete
prepared chain and preflight must then be retained with full archive readback.

The trial performs one recovery, twelve fixed uploads/readbacks and twelve fresh
final reads, followed by the complete paired native menu/core/return sequence:
ZX81, ZX80 and ZX81, four menu sessions, seven configuration activations and
owned final Park at logical generation 7 / bridge generation 40. Every menu
frame and shared USB ownership transition remains checked. Physical controller
action is not required for this serial-controlled cycle.

Existing bounds remain: startup/launch/menu restore 100 seconds, configuration
20, core close and final park 5, programmer 90, native phase 450/process 480,
uploads 300/process 320, recovery process 270, outer 840/host 890. Each new live
scan is bounded at 30 seconds inside those envelopes; each trace file is bounded
at 8 MiB. The original two-second passive failure tail and 1 MiB console bound
remain. First failure is terminal; neither commands nor consumed phases replay.

Off-board checks cover both actual staged loader functions, successful and failed
scans, ownership loss, programmer failure/timeout, replay, source/attempt counters,
the real trace-account handoff, controller exchanges, twelve-file upload failure
paths, receipt mutations and cleanup lifecycle. The previous full parent suite
passed 1,678 tests with three existing prerequisite skips on the unchanged
admission-policy source `a9fb400d5`.

This is a bounded diagnostic procedure. A prepared image, source checks or JTAG
completion alone do not qualify native boot, the full launch/return cycle,
physical display/input, normal package admission or DE10-equivalent readiness.
The physical outcome must be recorded separately after the test.
