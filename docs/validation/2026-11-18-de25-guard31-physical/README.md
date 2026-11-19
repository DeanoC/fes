# DE25 final-proof hardware diagnostic: exact source deadline rejection

The fresh bounded TRWU8CBL trial identifies guard 31's rejected member:
**reason 7, expiry of the original enclosing 20-second source clock**.
It completes initial MENU, warm ZX81 with tape and return release, then all
next-MENU configuration DATA. Final input proof rejects before generation-15
retirement. The full repeated launch/return/Park cycle remains failed.

The independent recovery/restoration procedure succeeds **without a power
cycle**, restores the exact original FIT and verifies all protected files.
No failed diagnostic is retried. This is an experimental serial diagnostic,
not normal Kit D25 HIL or physical display/input acceptance.

## Exact selection and programming investigation

Native source: `c45a8d27efa2904f1c0000ef99e415cfa4d77fd1`, documentation only
above the [tested implementation](../2026-11-18-de25-final-input-proof/README.md)
`379c6aa7d40d1283d80f353e36a7ddce43ae4f1c`.
Host: `7119ffa96b153149499f67e797a7ae18fe2413db`.
Boot ID: `1a8c1e6016feca1c`.
FIT SHA-256: `1ca80b57e7ff7a6ebbdb089fb41821706df698bbe92fc8f293f0da377a0b572a`.
Full SOF SHA-256: `27073c9b07896da38a54535646c19dad42a0453a337edf26bffc3f6f281a6e06`.
Both core payloads and Linux defaults are unchanged.

The [preceding attempt](../2026-11-18-de25-guard31-launch-attempt/README.md)
stopped at recovery programming with a rescan/chain-broken error, before any
SD write. It remains failed. A new [read-only isolated-server probe](read-only-jtag-probe.json)
subsequently passes three forced scans at fixed 3 MHz without a reset or
programming operation. Its bounded server `ioctl` trace contains nonblocking
`EAGAIN` polling. This does not establish the earlier fault's physical cause.

The new one-use procedure is
`b78e3dad9d5ab3117dfedfa830d614843dd2fbcbae47bf4ec6256d78c8ff1619`.
It uses fresh recovery nonces, source/capture admission, five image-selection
tests, a new 60-second USB window and independent handle checks. Seven private
wrapper tests ensure errors remain failures, trace attachment/ownership/detach
failures block success and attempts cannot be reused. Programming return code,
error-free log, live scan, ownership and every clock/deadline gate are unchanged.

All three programming roles pass with zero Quartus errors. Each observes only
its owned private JTAG server through the existing bounded tracer, attaches
around programming and detaches before subsequent work. Each trace has an
8 MiB / 120-second cap; the programmer's original 90-second limit remains.
Tracing changes host timing and is diagnostic evidence. There is no claim that
it fixes the rescan fault or qualifies ordinary programming reliability.

## Native result

Initial MENU input proof, retirement and continuation pass. Source release
4→10 completes in **929,241 us**. Warm ZX81 proof/retirement/continuation pass;
917-byte tape delivery takes 1,785,682,762 ns under its six-second bound.
Running CPU fetch delta is **12,014**. Return release 10→15 completes in
**916,510 us**. Both source releases satisfy the unchanged one-second bound.

Generation-15 warm preparation/arm and the full **1,708,032-byte** next-MENU
DATA/ACK accounting and configured status pass. The failure-only clock record
then precedes the rejected INPUT_RESULT:

```text
FES_NANO_SOURCE_CLOCK reason=7 sampled=1 frequency=400000000 start_us=155247680 last_us=174742007 observed_us=175823981 bound_us=20000000 retained=1 grant=0
```

| Clock evidence | Microseconds from original source start |
| --- | ---: |
| Last accepted sample | 19,494,327 |
| Rejected sample | 20,576,301 |
| Existing bound | 20,000,000 |
| Gap between accepted and rejected samples | 1,081,974 |

The gap is **not an isolated function-duration measurement**: the saved
accepted sample belongs to the transfer service; work until the final clock
check lies between these observations. Source inspection places full table
and live owner validation before that final check. A detailed cost profile
must distinguish those operations rather than assigning the whole gap to one
helper or using the model's synthetic barrier coefficient as measured latency.

INPUT_RESULT is `status=4 proof=0 source_failed=1 scope_reason=31`, while
`configured=1`, all byte counts equal the payload, pending/outstanding are zero
and transfer/service errors are zero. Generation-15 retirement is not entered;
ZX80 and final native Park are not reached. The terminal firmware INPUT_PARK
marker grants no frontend quiescence or old-source reuse.

The 256-byte block-barrier optimization preserved the intended checks but was
insufficient to make this physical path meet its deadline. The earlier modeled
pass remains model evidence. This new exact physical reason does not
retroactively identify the rejected member in older, less informative traces.

## Restoration, bounds and independent checks

| Role | Seconds | Unchanged role bound |
| --- | ---: | ---: |
| Candidate FIT transaction | 327.342 | 1,200 |
| Native diagnostic, failed | 193.378 | 480; capture 450 |
| Exact original FIT restoration | 323.593 | 1,200 |

The controller completes in 845.423 seconds under its original 3,400-second
bound. No host watchdog or operation bound is extended. The 20-second native
source rejection remains failed.

[Independent raw-wire audit](independent-audit.json) verifies all 17 before and
after file readbacks per transaction, 16 preserved files, 823+825 accepted
nonce-bound command receipts, distinct reset/session/capture identities,
19 Non-secure SMMU reset-register words per recovery and exactly two FAT writes:
candidate, then original. All three server traces match their owned server
epochs and retained hashes, and their tracers are stopped/detached.
Exact USB permissions and JTAG 15 MHz / auto-adjust 1 are restored; all owned
programmer/server/UART/adapter handles are independently closed.

Original FIT `d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`
is restored. The spare remains installed and the kit is parked at its verified
recovery prompt under the continuing claim. The ledger governs later work.
No QSPI/raw-SD/factory-card/bottom-kit/J8G operation, old-native baseline reboot,
SDM-reset/old-DMA retirement guarantee, native resume or power-loss durability
acceptance is claimed.

## Preservation and next work

[Prepared retention](prepared-retention.json) independently verifies source
archives and 84,627 prepared entries. An initial archive-verifier relative-path
error and its failed archive remain separate from the corrected verified
archive. No hardware dispatch occurred during that recording fix.

The [native terminal record](native-terminal.json) and a separately verified
58-entry failure archive were retained before restoration completed.
[Physical retention](physical-retention.json) verifies 341 entries, archive
SHA-256 `2946054bc16af4bc96c0794fc284e9567e1b59f3b72a3700435114d2b81812fc`,
under `/home/deano/fes-retained/de25-guard31-observed-trial-20261118-r2`.
Raw UART, programmer/syscall traces, admitted controls and complete matched
artifacts remain outside Git.

Next: profile the actual final-proof path and preceding transfer work off-board
against this observed boundary, then reduce cost without removing ownership,
integrity, freshness checks or moving a clock origin/deadline. Any new repair
requires a matched artifact and a fresh one-use bounded full-cycle procedure.
This diagnostic is consumed and cannot be retried. Exact-head review, complete
lifecycle/failure coverage, vendor reset/DMA guarantees and normal Kit D25
HIL/restore remain open. [#1867](https://github.com/DeanoC/fes/issues/1867) stays
open; [#1952](https://github.com/DeanoC/fes/pull/1952) stays draft, `lane:deano`.
