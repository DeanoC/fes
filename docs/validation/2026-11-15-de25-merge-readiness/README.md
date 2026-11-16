# Opt-in DE25 preservation and main integration

PR [#1791](https://github.com/DeanoC/fes/pull/1791) preserves the experimental
Linux-free base under `FES_EXPERIMENTAL_DE25_EMBEDDED=1`. The preservation issue
is [#1790](https://github.com/DeanoC/fes/issues/1790). The complete secure-system
investigation is now tracked separately in
[#1867](https://github.com/DeanoC/fes/issues/1867), which stays open when the
foundation is merged. Production Linux build/image defaults remain unchanged.

## Historical matched source and hardware result

The matched source is `e04af2f8a55263644422a412bd02ba69dea58b1c`. Its native
SDK, CPU entry, BL31, SPL/U-Boot, complete FPGA composition and full file
admission pass. The independent tape memory contract agrees across producers.
The full matched archive `071bd84fa873d6a4f939269bfb32575435a0e788341b3836d8f0350126a99b7a`
independently verifies 46,704 entries. The private firmware reservation retains
192 spare bytes and four 8 KiB stacks; whole-EL3 stack qualification remains open.
The accompanying matched/integration receipts identify this exact source.

Fresh procedure `fe4fca9deec5e735de3b47b89b6376fec7582b621ef9978778aedfda0eeed931`
passes recovery, candidate FIT write and all protected-file readbacks. Native
MENU and warm ZX81 configuration, retirement and continuation pass. The
917-byte tape loads in 1.801 seconds within its six-second limit; CPU fetch
advances by 12,008 cycles and the native session reaches Running. Master-stop
reports success. Return to MENU then rejects secure source release at generation
15. The exact rejecting ownership/timing condition is not established.
No complete repeated-loading/final-Park acceptance is claimed.

Physical archive `bef00aa9f9f11e0b8e5bfef96bd5c6cd1660ffd19bc71009669d78a680711174`
independently verifies 165 entries and closed owned processes/UART/adapter
handles. The candidate FIT remains on TRWU8CBL's spare card; native resources
remain retained and the stopped procedure requires a fresh physical reset
before restoration-only work. QSPI, the factory card and bottom kit are
untouched. Consumed diagnostics are not retried. No new board action is part
of this merge-preparation step.

## Integration checks

The 145 planner/workflow regression tests pass after conflict resolution. The
explicit experimental opt-in check, full Tenfoot package under the race detector,
and native model backend, DE25 board and ARM64 mapping race tests pass. The full
Tenfoot run exposed a test that counted asynchronous UI telemetry as a premature
launch. The test now waits for telemetry and counts only the launch endpoint,
while still verifying failed display handoff aborts the launch. Runtime launch
behavior is unchanged.

## Integration scope and remaining gates

Main `62e202751d40887e605c34f54915a0a4df26b807` is merged normally, preserving
published history. The three conflicts are in test planning: retain both main's
DE25 combined-proof input routing and the experiment's private firmware/native
contract routing, with both sets of regression tests. Main's extracted Tenfoot
session handlers remain selected. The previous tested checkout and artifacts
stay frozen for restoration; merge work uses a separate checkout.

The secure-system investigation must cover the whole boot/load/stop/return/reuse
state machine, trust boundaries, source generations, memory/cache aliases,
DMA/master drain, clocks and reset/recovery semantics. Component tests and one
running core do not close that investigation. This PR remains an explicitly
unfinished experiment, with that limitation visible in the source and PR.

Merge readiness still requires current-head CI, independent review, resolved
review threads and the applicable HIL acceptance or an explicit owner waiver.
The historical diagnostic above is partial evidence and does not satisfy a
passing repeated-loading HIL gate for the newly integrated tree.
