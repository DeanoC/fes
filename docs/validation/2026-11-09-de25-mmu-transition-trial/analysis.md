# Transition stop and next diagnostic

The new board trace has fifteen valid ordered steps. The callbacks writing MAIR,
TCR and TTBR0 return; the engine's subsequent register observations and second
admission pass before the SCTLR callback. The last observed step is
`write-before operation=3`. There is no post-write marker. This does not prove
that the SCTLR instruction completed, nor that the first translated instruction,
private stack access or subsequent UART access worked. The witness probe was
never entered. The timer-bounded collector stops without sending `fesboot`.

The linked callback performs `MSR SCTLR_EL3,x20` at `8001ad18`, then ISB at
`8001ad1c`, then branches to the common path at `8001acd8` which calls `step`.
That C reporting path needs the private stack. Runtime exception vectors are
at `8003b800`, while the stack reservation is `50000..58000`. A fault involving
the stack or diagnostic transport can therefore explain a missing ordinary
report. These remain hypotheses, not a diagnosed hardware cause.

Both exact table sets pass translated permission/identity/security checks in
QEMU, including actual secure DDR write/read and stack use. This excludes those
specific descriptor mistakes in the emulated model, not Nano bus or cache faults.
The pinned vendor map also selects non-secure Device PSS and secure noncacheable
OCRAM. The private cache profile deliberately changes the private OCRAM subset.
The [SoC reference manual](https://docs.altera.com/r/docs/814346/26.1/hard-processor-system-technical-reference-manual-agilextm-5-socs/exclusive-access-support?contentId=otqFae9xntORHeIgJlv72g)
explicitly describes cached OCRAM accesses through the CCU. A blanket assertion
that OCRAM cannot be cached is therefore unsupported; no speculative memory-map
or access-permission change was made.

The [register-only fault-vector prototype](fault-vector-prototype.json) now
executes with the exact production descriptors and M/C/WXN enabled under
emulated A53 EL3. A normal control exits without a fault. Deliberately failing
stack writes, unmapped data writes and execute-never instruction fetches report
the exact expected ESR, ELR and FAR, plus SPSR, SP, SCTLR and TTBR0, without any
stack access in the handler. Fault cases remain terminal. Its output uses
semihosting; it is not installed on the Nano and does not establish physical
UART, cache or device behavior. Complete fixture source and generated inputs
are retained with the postmortem.

Next: integrate a bounded register-only capture around the selected SCTLR
transition, preserving/restoring the original vector on successful return and
parking on faults. Validate the actual linked instructions and injected faults
before producing another new matched boot chain. All CPU/owner/containment and
tape checks, original deadlines and consumed programming reservations remain.
No retry of `59e96369a749fc7d` or any older image is authorized by this result.
