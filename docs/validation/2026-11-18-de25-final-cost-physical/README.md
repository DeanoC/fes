# DE25 final-validation cost repair: bounded physical result

The fresh TRWU8CBL launch/return/Park test remains **FAILED**. Initial MENU,
warm ZX81 with tape and both source releases pass. Generation-15 preparation,
arm and the complete next-MENU configuration pass, but final input proof again
rejects guard 31 / reason 7: the original 20-second source clock expires at
**20,142,473 us**. ZX80 and final native Park are not reached.

Independent exact original-FIT restoration succeeds **without power cycling**.
The failed diagnostic is consumed and was not retried. This is an experimental
serial diagnostic, not ordinary Kit D25 HIL or physical display/input acceptance.

## Exact selection and gates

Native source: `e5eef81ec96fc8523c8490994837ec3fe4bca124`, documentation only
above tested implementation `b219e3920b32de39a5421dc7a3b2d479ac5c27c4`.
Host: `7119ffa96b153149499f67e797a7ae18fe2413db`.
Boot ID: `8076d962b9b3aa0c`.
FIT SHA-256: `d65ad3211a09818d891c9f5310cc3679ecf872435dbccd8e9fdd608a1faba4e4`.
Full SOF: `a3eaf449512a153ad4f194344dff4a0c886a8f6e9dfb9574d1c16623e3cb9bfe`.
Both core payloads and Linux defaults are unchanged.

The [off-board repair](../2026-11-18-de25-final-validation-cost/README.md)
removes duplicate USB ordering barriers while preserving each reset/controller
read and every ownership check. Its synthetic pass does not establish physical
deadline acceptance. The [preceding physical rejection](../2026-11-18-de25-guard31-physical/README.md)
remains failed and separately retained.

Fresh one-use procedure:
`9f3b9c95f7b8b80dff4e9aea1590ebd885d3262199911830033f1e1c012ce0da`.
Fresh source/capture admission, five image-selection rejection tests, seven
programmer wrapper tests, 60-second USB stability and independent free-handle
checks pass before dispatch. Forward and restoration use different fresh reset
nonces and independently establish loader/card/NS-SMMU gates before any write.
All three programming roles pass with zero Quartus errors, bounded owned-server
tracing and clean detach. This does not explain the older JTAG rescan failure
or establish ordinary programming reliability. All deadlines remain unchanged.

## Native evidence

Initial MENU input proof/retirement/continuation pass. Release 4→10 completes
in **898,366 us**. Warm ZX81 proof/retirement/continuation pass; the 917-byte
tape takes **1,795,576,635 ns** under its six-second bound,
with verified running CPU-fetch delta **11,701**. Release 10→15 completes in
**885,399 us**. Both releases satisfy the original one-second bound.

Generation-15 preparation and arm pass, followed by all **1,708,032 bytes**
accepted/posted/acknowledged/completed and configured status. There is no
transfer/service error and pending/outstanding are zero. Final input proof
then emits its first rejected clock observation:

```text
FES_NANO_SOURCE_CLOCK reason=7 sampled=1 frequency=400000000 start_us=154151730 last_us=173254993 observed_us=174294203 bound_us=20000000 retained=1 grant=0
```

| Source-clock observation | Microseconds from original start |
| --- | ---: |
| Last accepted sample | 19,103,263 |
| Rejected sample | 20,142,473 |
| Original bound | 20,000,000 |
| Overrun | 142,473 |
| Accepted-to-rejected sample gap | 1,039,210 |

The sample gap includes work between transfer-service and final-proof samples;
it is **not an isolated function-duration measurement**. The preceding run
rejected at 20,576,301 us. The lower elapsed result here is an observation from
two separate trials, not isolated attribution of hardware latency or a pass.
The synthetic coefficients and 19.3% barrier-work reduction remain model/work
evidence. This physical repair is insufficient to meet the source deadline.

INPUT_RESULT is status 4 / proof 0 / source_failed 1 / scope_reason 31.
Generation-15 retirement is not entered; ZX80 and final native Park are not
reached. The firmware INPUT_PARK failure marker grants no frontend quiescence,
ordinary resume or old-source reuse. [Independent wire audit](independent-audit.json).

## Restoration and retention

The initial unprivileged post-run trace audit stopped on root-only trace-file
access. A privileged read-only replay passes all trace checks; device permissions
and the physical verdict are unchanged. That recording failure is retained.

| Role | Seconds | Unchanged bound |
| --- | ---: | ---: |
| Candidate FIT install/readback | 300.040 | 1,200 |
| Native diagnostic, failed | 190.691 | 480; capture 450 |
| Original FIT restoration | 295.422 | 1,200 |

The controller completes in 787.081 seconds within its
3,400-second bound. Independent raw-wire replay verifies all 17 before/after
file checks per transaction, 16 preserved files, unique command/session/reset
identities, 19 NS SMMU reset-register observations per recovery and exactly two
FAT writes: candidate then original. All three owned server traces, cleanup,
closed UART/adapter/programmer handles and exact original USB permissions /
JTAG 15 MHz, auto-adjust 1 are independently verified.

Original FIT `d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
is restored; the kit is parked at its verified recovery prompt, spare installed,
continuing claim held. Current state belongs to the kit ledger. No power cycle,
QSPI/raw-SD/factory/bottom/J8G action, old-native baseline dispatch, SDM/old-DMA
retirement guarantee or normal HIL acceptance is claimed.

[Prepared retention](prepared-retention.json) verifies 84,569 entries and exact
native/host Git source archives. [Native terminal retention](native-terminal-retention.json)
independently preserves the failure before restoration completed.
[Physical retention](physical-retention.json) verifies 288 entries,
SHA-256 `e55b939dba7b6d9eecdb9b0765d6c47d24c85a8bb4210e1e198365d161028532`, under
`/home/deano/fes-retained/de25-final-cost-trial-20261118-r1`.
Raw wire, binaries, ROMs, programmer traces and complete boot artifacts remain
outside Git. No failed diagnostic retry occurred.

Next: profile the remaining validation/transfer work off-board against this
new boundary, then reduce its cost while retaining every ownership/integrity/
freshness check, clock origin and time limit. Any new implementation needs a
matched artifact and a new bounded procedure. Exact-head review, complete
lifecycle/failure coverage, vendor reset/DMA guarantees and ordinary Kit D25
HIL/restore remain open. [#1867](https://github.com/DeanoC/fes/issues/1867) stays
open; [#1952](https://github.com/DeanoC/fes/pull/1952) stays draft, `lane:deano`.
