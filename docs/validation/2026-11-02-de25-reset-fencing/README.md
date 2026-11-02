# DE25 fitted reset coverage and finite release matrix

This record expands the current MENU reset evidence to all twelve reported
reset chains, including nine vendor scalar output registers omitted by the
initial vector-only inventory. It changes no product RTL, kernel, runtime,
producer policy, constraints, RBF or installed state. RDC coordination,
physical metastability, native ownership and recovery remain unqualified.

The published source before this record is
`7246fac729202c70a1adc255be8d2f57054cca35`. The actual FPGA producer remains
`72716153eb7be7b62917ed685d5d7380347a2d7d`, BUILD_ID
`5952e4a9936363114264247efd5584ee`, Linux RBF SHA-256
`e18324235e32c86b643b24dba18deab344628cd9fe0a41bf9d6a3ea76836fde4`.
All 374 canonical source inputs and every non-documentation file remain
unchanged. The preceding exact-input software results are reused; no fresh
full software-suite pass is claimed.

## Fitted reset and timing coverage

Fresh private copies of the exact fitted database are queried with the
installed Quartus Pro 26.3. Original source/RBF/SDC/database bindings remain
unchanged. No timing assignment, exception, clock, toggle rate or waiver is
added. The query includes every actual generic `chain_out` register and checks
for fitted copies, alongside all vector stages. The twelve chains contain
49 registers: 40 vector stages and nine scalar outputs.

The graph contains 4,812 rows. Reverse queries of every structural consumer
collect another 262,870 rows and distinguish asynchronous connections from
synchronous data consumers and external output ports. Empty filtered forward
queries are not interpreted as absence of a reset consumer.

Each of four actual operating corners receives recovery and removal queries
with one worst path per endpoint and capacity above its full structural
consumer count. The 96 raw reports contain 88 nonempty reports and eight
entries with no reported timed path. The nonempty reports contain 10,576
nonviolating paths; worst recovery is 7.353 ns and worst removal is 0.166 ns.
The same 1,322 endpoints appear in each corner/check. An entry saying
`Nothing to report` is not a passing timing check.

The asynchronous connections outside those reports are explicitly retained:

- Four existing outer reset-synchronizer ingress exceptions: the two
  lock-qualifier and two visibility-release registers.
- Forty vendor FIFO reset-synchronizer ingress registers, including their
  scalar outputs, outside these recovery/removal reports.
- Twenty-five HDMI data/DE output ports, which are outside register
  recovery/removal checks and still need their external timing contract.

The lock-qualifier output has two asynchronous and three synchronous
consumers, with no reported recovery/removal path in any corner. This is an
explicit coverage boundary. It is not evidence that the output is unused or
that physical reset coordination passes. [The detailed analysis](analysis.json)
records every chain, report, count, excluded endpoint and input digest.

## Actual generated transport and board/reset model

The exact current candidate passes its existing 72-case generated-project
startup check in 14.3 seconds and a refreshed 32-case board composition in
148.5 seconds. The latter includes eight clock phases and normal STOP,
stopped pixel clock/lock loss/relock, configuration reset and system PLL
loss, plus a deliberately bad lock-dependent bridge reset that is rejected.
These current-candidate runs do not transfer timing or hardware acceptance
from the earlier compiled candidate.

The expanded private model retains the actual board/reset RTL, generated
parent/reset controllers, CDC bridges and Avalon/AXI transport. PLL outputs,
HDMI setup completion, M20K storage and external AXI memory are explicit
models. The normal fixed raster is retained. Elaboration checks all twelve
delay indices, their original depths and asynchronous modes; all other reset
chains keep their original behavior.

Each chain's first stage independently holds reset for zero or one extra
receiving edge. All nine scalar output registers retain their actual logic.
An independent clock-edge monitor checks immediate assertion and exact
release: five edges for the eight FIFO chains, three for the pixel parent
controller and two for each board chain, plus the selected extra edge.
Requesting a delayed release while actually applying no delay is rejected.

All **131,072 cases pass** in 2,193.944 seconds (2,194.0-second job):
eight clock phases, four events and all 4,096 independent delay masks. This
includes 65,536 global-reset cases with accepted external reads pending.
The complete sweep has 65 reachable state classes: 64 receive a second
independent full advance, one is an uncached singleton, and 130,943 cases
restore a checked active state. [The proof summary](proof-summary.json)
binds the exact executable, controls, current baselines and original failures.

In every case the guard starts at zero, receives a nonzero epoch, and the
real transport must accept external reads whose responses are withheld.
Normal STOP drains while retaining the allocation identity. Pixel-lock loss
with a stopped clock leaves live FIFO/grant accounting and the guard epoch
intact, latches a fault, and drains after the clock returns. Global resets
assert all twelve chains without pixel edges and clear the guard epoch while
the external accepted-read queue remains pending. The model never returns
stale responses, submits a replacement grant or treats reset-empty FIFOs as
proof that old memory ownership ended.

## Checked complete-state acceleration

The original full sweep is preserved as deliberately stopped, with 149
complete successful cases and terminal status `failed/-15`; it is not a
full pass. Profiling shows about 1.66 million memory cycles per case waiting
for the real video frame, approximately 3.96 seconds. Merely increasing the
run limit is not used as the repair.

The private accelerated executable uses Verilator save/restore, one thread
and recorded C++ optimization. Every phase/event/mask still executes its own
cold reset/release and event checks. Board readiness is awaited with the
original 4,096-cycle limit and 64 settling cycles, rather than advancing a
fixed 4,096 cycles after it is already ready.

Before reusing a frame wait, the harness compares the complete serialized
DUT bytes and every one of its 36 host members, including clocks, counters,
pending queues, memory identity and backpressure state. Only the test-only
delay-mask input is normalized, without clock edges, after all twelve delay
selectors have been consumed and every reset source/output is inactive.
Independent release monitors must be saturated. No RTL register, raster
state, epoch, generation or host member is omitted from comparison.

The first member of each equivalence class runs the real raster and obtains
accepted, unanswered reads. A second equivalent member independently runs
that full path and must match the entire resulting DUT/host state before
later members may restore it. Classes with a single member run uncached.
No reset may assert during the shared advance. Each restored state is checked
again before applying its own mask and actual event. The 16-case probe passes
with 13 restored cases; corrupting a saved host counter is correctly refused.
The wrong-delay negative control also remains effective in this executable.
The complete sweep uses that exact probe-tested binary, with no rebuild.

## Reset epochs and remaining acceptance

The fresh fitted inventory includes 33 parent-controller, 12 peripheral-reset,
136 guard and 71 allocation-generation registers. All 33 fitted guard epoch
registers, including a fitted copy, have the peripheral reset output as their
asynchronous source; that output is reset from the synchronized system root.
The 71 fitted generation registers have the pixel parent reset output as their
asynchronous source, itself driven by `first_lock_seen`. The lock qualifier
enters that sticky register's synchronous data path. Static reachability does
not prove the sticky behavior; the RTL/model tests supply the finite behavior
evidence.

This agrees with the kernel's existing refusal to reinterpret an unreadable,
zero or changed epoch as old DMA drain. No fresh integrated RTL/kernel or
native test is performed. The model establishes neither physical DDR/SMMU
isolation nor bounded recovery after a global reset or interrupted secure call.

The canonical candidate still reports TMC-20012:30, TMC-20011:2 and RDC-50003:2,
with zero waived findings. Only the two previously approved HDMI input
findings have their fitted review. No reset or output exception is accepted.
The user has no additional PCB or trace-delay files. A bounded check of the
[official public resource index](https://dl2.terasic.com/resources/de25-nano/)
finds the existing rev-A/rev-B 1.0.0 packages and Linux console image, with no
separate PCB files advertised there; it does not establish that private vendor
material is unavailable. HDMI voltage/loading/path
timing, reset coordination, an eligible independent identical-RBF rebuild,
admitted factory/card and full native MENU/catalog/ZX81 acceptance remain open.
No kit, installed image, card, flash, UART, network or capture is accessed.
The consolidated PRs remain ready and owned by `lane:deano`.

## Retained attempts

The archive retains the vector-only first query, the initial elaboration
failure caused by omitted `.sv`/`.vh` inputs, and the private-query guard
refusal caused by four changed existing report-cache files. The original
candidate database is unchanged; no new cache files explain that refusal.
The initial local analysis incorrectly required a filtered forward data edge;
the corrected analysis uses the actual reverse endpoint query. Its failed
source and refusal record remain retained. None is counted as a passing test.

[Retention](retention.json) binds the complete 136,731,157-byte archive
(SHA-256 `169e2f889145fc42a25d58550331ffaa869e539fe0b43d0a9e9a2a0182764093`).
Independent local restoration verifies all 1,423 files in 5.724 seconds,
reproduces the actual analysis byte for byte, audits every matrix case and
verifies original fitted bindings plus all 374 canonical source inputs.
The complete original fitted project remains retained under its prior archive;
only duplicate private query-project copies are excluded. All 29 remote and
two local completed phase process/group identities are absent. No baseline
restoration, promotion or release is performed.
