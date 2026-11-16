# Fresh admission for each host image-programming operation

The previous operator admitted the JTAG device once, before recovery, and kept
that admission across recovery programming, SD uploads and native programming.
The staged replacement requires a new live vendor-client scan before each
image operation. It durably reserves the attempt before scanning, records the
successful proof before programming and rejects reuse even through a new
Python instance. A failed scan, changed claim/server/configuration/clock or
changed probe/client bytes prevents programming. No fallback or retry is added.

The new [host policy](../../../platform/embedded/nano/jtag_programming.py) is
used by both actual staged recovery/native programmer functions through
[program_gate.py](operator/program_gate.py). The programmer retains its
90-second limit, UART receive handling, initial phase fuses and original
clock/server/USB cleanup. Image programming is wrapped in a bounded syscall
trace with an 8 MiB per-file limit and termination of tracees if the tracer is
killed. A read-only listing neither mints nor consumes programming admission.

Eight policy tests cover the unchanged physical scan fixture, per-operation
consumption, failed/incomplete/listing-only proofs, identity changes, durable
write failures, exceptions and concurrency. Four operator tests execute the
actual two loader functions with injected process/clock I/O, covering success,
scan failure, claim loss, programmer failure, the original programmer timeout
and replay rejection. They also compare the retained receive/failure-tail,
phase-fuse and cleanup helper functions against the previous operator.
These operator source copies are a preparation milestone; the complete new
hardware preflight, input manifest and attempt accounting remain to be prepared.

Two additional bounded physical diagnostics informed this change:

- A read-only `quartus_pgm -a` under syscall tracing passed at 6 MHz. The trace
  identifies the real `linux64/quartus_pgm` process, both mapped vendor JTAG
  client libraries and a delegated `jtagconfig --cable ...` device-list child.
  Both observed network connections target the owned private server. The
  clock remained 6 MHz after the query. Thus the listing is not sufficient
  evidence for the programming-time scan.
- A separate forced live scan with both vendor client variants loaded passed
  at 6 MHz, with its scan symbol bound to `libjtag_client_dcrt.so`. It returned
  `4362C0DD`, mask zero and instruction length 10. This improves on the earlier
  standalone probe, which linked `libaji_client.so.0.0.0` instead.

Ten tracing-wrapper cases and seventeen probe API cases passed. Independent
audits verify saved command/capture hashes, frozen sources, exact original USB
permissions, 15 MHz restoration, private-server exits and a free UART. Neither
diagnostic opened the UART, programmed an image, reset the CPU or wrote the SD
card. The bottom kit was untouched and the continuing top-kit claim is held.

[result.json](result.json) binds this evidence and the staged source. The
retained SDK's 2,105 source hashes, entry's 2,149 source hashes, BL31's 98 producer
hashes, composition's 26 selected inputs and the U-Boot producer still match;
the new host policy is not an input to those compiled artifacts.

This repairs the admission lifetime and adds useful failure tracing. It does
**not** explain the earlier error 87 or demonstrate that programming now
succeeds. Both prior image attempts remain consumed, and no third image load
was attempted. The complete native launcher/core/return cycle remains open.
Next, finish the fresh bounded procedure around this changed transport path,
retain the previous counts and qualify the exact rebuilt operator before use.
