# Intel-compatible scan-only API observation

The preceding diagnostic observed an exact top-cable name match and raw vendor
chain type2, then refused its additional pre-scan type==1 gate. It made zero
forced scans. Public pinned Intel headers define unknown0 and JTAG1; the
meaning of the returned2 remains unproven. Both preceding diagnostics and the
native7abe reservation remain consumed and retired.

Intel's pinned `jtagconfig.cpp` enumeration (lines479 onward) locks the cable and
uses the scan/read API without first rejecting `AJI_HARDWARE.chain_type`.
Its scan implementation verifies a valid locked chain, then requests a fresh
scan from the server. This separate observer follows that sequence. It requires
the same exact owned/filtered USB cable and6MHz readback, issues at most one
forced scan, and records raw chain type as observation. It does not interpret2
as a known device type, substitute cached identity, switch a programming mode,
or make any programming/reset/recovery request.

The result can establish an actual scan return code and, only on success, the
fresh exact4362C0DD/IR10/count1 identity. Four bounded captured-data parameters
and bounded vendor messages are recorded even after a failed forced scan.
No result from this observer can satisfy the programming-grant decoder. That
separate authority's source is unchanged and hash-bound in this experiment.
A failed scan remains a failed scan regardless of available captured data.

The actual probe compiles against pinned primary Intel headers and selected
vendor libraries. Controlled API tests cover raw unknown/unrecognized types,
wrong/unterminated exact names, successful/failed scans, captured-data bounds,
messages and programming-grant rejection. Failure-injected host orchestration
and independent read-only cleanup preserve original15MHz, exact USB ownership
and mode, private service identity and unused UART. Retention links to the
preceding hardware record, with full archive readback and verified Git bundle.

This remains a top-TRWU8CBL scan-only observation. It performs no FPGA
programming, HPS reset, SD/UART commands or bottom-kit operations. It does not
boot the new native firmware or test the tape deadline. The source finding and
any new scan outcome do not by themselves explain the historical87 or establish
ordinary DE10-equivalent appliance acceptance.

The bounded physical observation passed one explicit forced scan at6MHz:
find/name/lock/clock/forced-scan/cached-after/unlock all returned0. Raw pre-scan
chain type remained2. Fresh identity was count1,4362C0DD,mask0,IR10. CapturedDR
was `(4362C0DD) [32]`, CapturedIR `(001) [10]`, and captured/normal bypass both
`(0) [1]`, each with its explicit terminating NUL. Auto-adjust was enabled before
and after; the after-scan clock readback stayed6000000. No vendor callbacks
were emitted. The runtime returned0, with no programmer/reset/SD/UART commands
and no programming grant. The observed host-probe type refusal was an extra
assumption; type2 did not prevent this locked scan from succeeding.

Independent cleanup passed: exact unchanged USB identity and owner/group/mode,
recorded original15MHz readback, private server gone and UART free. The board
remains last verified in recovery U-Boot; this observation did not boot native
firmware or repeat the retired7abe procedure. The original87 cause remains
unproven because server, discovery sequence and time differ. Do not reuse this
diagnostic as a programming grant or claim the native tape deadline resolved.

The next integration step is a source-controlled fresh programming-scan producer
using the now observed vendor sequence, while retaining its exact cable/clock,
fresh forced scan and post-scan identity, per-operation one-use reservation and
first-failure stop. That producer needs controlled success/failure tests, vendor
build identity and an independently bounded procedure before more programming.
Only then can the finer native ownership timing experiment address the one-second
tape-loading failure. Normal launch/return/failure, display and full appliance
acceptance remain open.
