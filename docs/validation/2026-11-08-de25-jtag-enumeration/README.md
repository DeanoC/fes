# Read-only enumeration after the programming stop

A separate top-only diagnostic started a fresh private JTAG server after both
failed image attempts were retained. Its setup, enumeration, 15M clock read and
final enumeration commands all returned zero. The saved output lists only
TRWU8CBL's private cable and the expected `4362C0DD` device, without an error
footer. No FPGA programming, HPS reset, card write or clock write was issued.

The temporary diagnostic helper initially hit an import error before device
access. The actual query subsequently completed, but its reporting step called
the existing zero-argument `jtag_cable` admission accessor as though it parsed
text. That original diagnostic result remains failed. An independent offline
audit validates the saved exact command list/output and cleanup. Its initial
clock-output check assumed an optional tool notice was always printed; the
corrected check accepts only the exact owned notice, when present, and `15M`.
No device command was repeated to repair either reporting error.

[result.json](result.json) binds those records. The audit independently verifies
exact original USB permissions, private process exit and an empty UART owner
inventory. Logs, failed helper results and the audit remain under
`out/nano-jtag-scan-diag-20261108`.

This shows fresh private-server enumeration is currently available at 15M. It
does not establish fresh physical-scan semantics for the vendor enumeration,
explain programming error 87, or qualify the failed 6M programming path. The
two image attempts and consumed procedures remain unchanged. No later FPGA/CPU
state, native Park, return cycle or ordinary core-load acceptance is inferred.

Continue transport diagnosis before any further programming: distinguish the
programmer's access from enumeration, identify the error using primary tool/API
evidence, and preserve exact executable/environment identities. The claim remains
held; the bottom kit and shared JTAG daemon were excluded.
