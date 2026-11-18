# DE25 final validation: USB containment ordering cost

The production final-proof path repeatedly verifies cold CPU/device ownership
and USB containment. A focused host function profile found 3,888 containment
callbacks, each preserving two clock scans and eight fresh reset/controller
read pairs. The repair shares the reset read's completion barrier with the
following controller observation. Every read, reset predicate, outer ownership
check, callback order, closing barrier and failure stop remains.

This is an off-board repair for [#1867](https://github.com/DeanoC/fes/issues/1867)
in draft [#1952](https://github.com/DeanoC/fes/pull/1952), `lane:deano`.
The [previous physical trial](../2026-11-18-de25-guard31-physical/README.md)
remains failed: guard 31 / reason 7 rejected the original source clock at
20,576,301 us. Its 1,081,974 us accepted-to-rejected sample gap is not an
isolated function profile. No kit action occurs in this checkpoint; hardware
qualification of this new repair remains open.

## Profiling and ordering argument

The host profile compiles the production final proof, transfer/parser,
retirement/continuation, activation, stop/return and source release/rearm paths,
including actual cold/USB platform and board suppliers. Hardware/CPU observations,
MMU platform scope, source hashing and ROM device work remain injected. The
profile uses function events with inlining disabled and wrapped comparisons;
its counts identify work, while its host nanoseconds are not target latency.
The selected window is generation-15 configured ISDONE through final proof and
retirement-entry checks before a retirement attempt. It does not measure a
running core's lifetime.

The earlier meter counted source-table reads and their barriers, but did not
charge the underlying USB/cold ordering callbacks. The expanded work model
also meters those callbacks throughout the original warm source-clock scope,
which starts before the additional source hash. No production timing samples
or logging are added.

The old pair performs:

```text
DSB → reset read → DSB → private bookkeeping/check → DSB → controller read → DSB
```

The new pair performs:

```text
DSB → reset read → DSB → same bookkeeping/check → controller read → DSB
```

The reset helper is unchanged. Its closing full-system barrier completes the
reset observation before the following controller read. Only private diagnostic
bookkeeping and the existing failed/reset predicates intervene; no device
access, platform callback, command or ownership grant is moved across this
boundary. The controller's closing barrier executes before both success and
error publication. This argument relies on the existing single owned CPU and
private-state writer contract; it introduces no cached device or ownership
result. [Arm DSB completion semantics](https://support.arm.com/documentation/102336/0101/Data-Synchronization-Barrier?lang=en).

Each containment sample keeps all 42 reads: 26 clock values and eight separate
reset/controller pairs. Its ordering barriers fall from 36 to 28. Clock scans,
paired full cold samples and USB DMA/IRQ/PHY/AHB-idle predicates remain fresh.
No preparation, transfer, release, retirement, poll or host deadline changes;
no clock origin restarts.

## Controls, work and rejection coverage

Before-fix control `a32309ed1b019472de791b26ffef4486091cdef9` adds tests and an
inactive fixture meter while retaining the physically tested production USB
helper. Fixed implementation is `b219e3920b32de39a5421dc7a3b2d479ac5c27c4`,
above physical evidence head `84eb6814027f0d71e50c7f637ca67eeade908eba`.
The new ordering and source-cost regressions fail before the repair and pass
after it. The preserved control executable, request and results remain separate.

The [independent work audit](work-audit.json) compares completed zero-cost
paths, retaining all 5,120 final table reads, probe read/barrier counts and the
same fixture board-access and hardware-read counts across three configurations.
The fixture's `physical_reads` field is the board access counter, including
writes; it is not a pure MMIO-read count. Actual final-profile callback counts
are separately preserved, including 324 source ownership calls, 1,296 cold
ownership calls, 3,888 coherent USB checks and 31,104 reset/controller pairs.
No failed partial path is compared against completed work as a speedup claim.

| Ordering work in the expanded model | Before | After |
| --- | ---: | ---: |
| USB/cold barriers in completed final proof/retirement entry | 160,704 | 129,600 |
| USB/cold barriers in the selected whole warm source operation | 1,993,624 | 1,608,216 |

These are 31,104 and 385,408 fewer ordering callbacks respectively, about
19.3% of those modeled barrier counts. Every MMIO and ownership observation
remains; no hardware time saving is claimed.

A synthetic 2,000 ns ordering cost plus 16,300,000 us of residual source work
reproduces complete DATA followed by reason-7 expiry in the before-fix control
at 20,286,294 us. Identical costs pass final proof, retirement and continuation
after repair, last accepted source elapsed 19,517,676 us. These coefficients
are counterexamples under the real source-clock origin, not fitted hardware
latency or a prediction that the new physical trial will pass. Already-expired
and higher-cost cases still reject under the unchanged bound. Late owner,
USB-health, callback, generation, mapping, digest and queue corruption reject
before retirement/continuation, with no retry I/O or DATA.

Ordering tests require a separate fresh reset sample before every controller
read, an intervening full barrier and a closing barrier before later I/O. Every
one of the 42 reads is independently failed; boundaries close, exact errors and
addresses are retained and no later hardware read is allowed. Existing cold
sample, reset, DMA/IRQ, PHY, generation and owner faults remain covered.

## Validation and remaining work

All **43** sanitized supplier/retirement tests and all **11 affected scopes**
pass on the fixed implementation: **2,900 parent tests** (21 skips), native
firmware/runtime/RTL/Tenfoot/producer integration and **2,512 FPGA software
tests** (90 skips). Counts overlap. The affected plan compares with main
`e2b8130b5a18b652095a8d5e23124fd0a994e9c0` (#1938).
[Exact commands, limits, log digests and expected before-fix failures](software-results.json).

The [linked AArch64 ordering/layout audit](target-proof.json) confirms the
reset helper is opcode-identical, retains its two barriers, and the direct
controller read retains a closing barrier before error publication. The
containment helper frame remains **64 bytes**; all four static table-ownership
call sites remain. BSS ends at `0x5ff80`, retaining the **128-byte OCRAM margin**
and the same `0x50000..0x58000` stack reservation.

A fresh SDK, CPU entry, BL31, SPL/U-Boot and full-SOF composition passes
[matched source and checked native file admission](matched-chain.json).
Boot ID: `8076d962b9b3aa0c`; FIT: 8,571,584 bytes, SHA-256
`d65ad3211a09818d891c9f5310cc3679ecf872435dbccd8e9fdd608a1faba4e4`; full SOF:
`a3eaf449512a153ad4f194344dff4a0c886a8f6e9dfb9574d1c16623e3cb9bfe`.
Both core payloads are unchanged. Composition used the shared Quartus Pro
lock; no synthesis/fitting or kit programming is claimed by this composition.
**The new image is unprogrammed.** [Stage results](build-stages.json).

[Retention receipts](retention.json) independently bind before/fixed Git source
archives, executable/request controls, profile outputs and the complete boot
chain. Raw binaries, ROMs, assembly and full logs remain outside Git under
`/home/deano/fes-retained/de25-final-validation-20261118-r1`. Initial profiler
harness/capture setup failures remain separate from the qualified results.

The next hardware trial needs fresh file admission and a new one-use bounded
procedure. It must check complete repeated launch/return/Park and preserve the
first clock rejection if it stops. Historical failed procedures remain consumed.
Exact-head review, complete hardware/failure coverage, vendor reset/DMA guarantees
and ordinary Kit D25 HIL/restore remain open, with no waiver. Linux defaults and
both FPGA core payloads are unchanged. This issue remains open.
