# Direct IN toggle notifications and descriptor byte-88 failure

Exact native/firmware and observer source:
`1a168998c46b03ac20a7ffe464bf39ce1325216a`.
Chain `bb1ead25063f14279202076a387b04700ecc5a1495026bb2e42972e8fad0b645`.
One actual attempt ran on exclusive TRWU8CBL, with no retry of this image.
Normal DE10-style core-load readiness is not established.

The driver now treats direct-IN RX status5 as a non-payload notification and
acknowledges HCINT data-toggle status while preserving the current packet's PID
and deadline. OUT/split toggle errors remain fatal. Actual data with the expected
size/PID, completion and retirement remain required. No setup, HCTSIZ or data
toggle is replayed solely because the notification arrived.

Errors retain the request type/value/index/length, setup/data/status phase,
byte offset, endpoint, PID, split state and notification counts. A later failure
retains the first RX notification's snapshot, including transmit queues/frame.

The full TamaGo host race suite, targeted DWC2/ownership/parser tests, Nano
cross-build and `make check` pass. Tests cover delayed/coalesced notifications,
no-data/wrong-PID rejection, timeout and frozen-clock bounds, per-poll work,
setup/data/status failure context and firmware-owned closure. The matched SDK,
entry, BL31, U-Boot, local PFG composition and committed-source audit pass; the
FIT is 2,699,152 bytes. Operator/uploader/receipt/failure-tail fixtures pass.
These checks are software/model evidence.

The actual test passes qualified recovery, one `FES/UPLOAD.BIN` upload/readback
and a fresh SD reread. Native boot, all nine CPU checkpoints, trusted context
and 328 HPS pin reads pass. The new context identifies the precise failing
operation:

```
control address=1 type=0x81 request=0x06 value=0x2200 index=0
length=98 phase=data offset=88 ep0=8
IN expected PID=0 split=false received=false
toggle_rx=1 toggle_irqs=1 -> packet timeout
first RX peek/pop=0xb0000 channel=0x80528008 HCINT=0x400
HCTSIZ=0x80008 nonperiodic=0x1080040 periodic=0x100040
frame=0x849e000f
containment: channel=0xc0528008 did not clear enable within 500ms
firmware status 4
```

Thus the packet notification was consumed and its interrupt processed, but no
valid packet arrived before the deadline. Descriptor parsing, USB Ready,
reports, checked Stop and CPU halt remain unobserved. The first snapshot is
from notification time; it is not a later observation of queue/frame progress.

The earlier model was too permissive: it delivered a delayed valid packet after
the notification was acknowledged. The primary PIO path has an additional
requirement. Synopsys-authored
[Linux v6.12 `dwc2_hc_continue_transfer`](https://github.com/torvalds/linux/blob/v6.12/drivers/usb/dwc2/hcd.c#L1477)
queues additional IN requests within the active transfer after checking request
queue space. Our driver currently queues only the initial request, then waits.
The source gap is established; whether correcting it resolves this physical
failure requires another implementation/model check and a fresh matched image.
The reference and exact downloaded hashes are retained under the build's
`reference/` directory. No Linux code is linked into the native image.

The independent replay verifies exact request/packet/snapshot fields, failed
admission, matched boot/CPU proof, upload protections and cleanup. Root read-only
cleanup confirms unchanged 15 MHz, exact USB permissions, no owned private JTAG
process and a free UART. The other kit and original factory card were untouched;
the continuing claim and spare remain held. No further native command or factory
restore followed the failure. USB containment remains unproven.

Evidence is in `out/nano-usb-toggle-20261104/` and
`out/nano-usb-toggle-trial-20261104/`. Next: make the model require queue-backed
PIO continuation, implement bounded continuation without resetting HCTSIZ/PID
or replaying setup, and validate failure/ownership paths before another boot.
Combined devices, physical input routing and ordinary package/ROM/media/core
admission remain incomplete.
