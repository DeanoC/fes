# DE25 fixed-source reconfiguration rearm, software evidence

Implementation: `12b8f6227e9d5c3fa5ba9c880a50b92209e6efc0`, based on
`96ea54bd5b54b1db77668b866f78ce3344479d0d`. This milestone adds secure and CPU
hooks for another transfer of the same compiled FPGA package after an owned
return. No board test or Quartus compile was run for this change.

## Behavior

The experimental private bridge contract is version 1.9. Operation 11 returns
its distinct `0x46575231` rearm tag and accepts only owner and generation. It
requires a successfully started, fully completed prior configuration, no issued
call or pending buffer, and a strictly newer owned all-master bridge closure.
Never-enabled history must be cleared and every bridge held reset, with all
masters contained and no active device lease. The secure provider consumes the
new generation before independently checking its fixed staged image against
compiled digests. No address, digest, size or new owner is accepted from the
caller. Duplicate, stale, busy, incomplete, failed and uncertain rearming cannot
authorize another START. Cold preparation remains one-shot.

The retired opt-in computer scope passes explicit rearm and its armed transfer
calls to this existing gate. It grants no fresh computer opening.
`PrepareReconfiguration` and `NewReconfigurationTransfer` preserve compiled-map,
whole-image/chunk and source-ownership checks through the native CPU hooks.
Preparation is deferred until START; terminal failures retain the region and
cannot replay or release it again.

## Validation

`make generate` updated the two consumers, and `make check` passed after the
source commit. The focused native Go race tests passed. The contained C state
machine passed 103 cases, including three successive warm transfer/control-close
cycles and 27 new rearm success/failure cases. The combined strong Nano provider,
actual owned SiP dispatcher and pinned vendor SHA implementation also passed a
second fixed-source transfer and corrupted-data, stale/future-generation and
changed-cache failures with modeled configuration engine/register/PHY events.
The existing joined actual Avalon RTL fixtures passed alongside these tests.
These are software checks, not observations of warm SDM operation on hardware.

All 12 groups selected by `scripts/test_changed.py` passed on the exact source
commit above. The parent suite selected 1,303 IDs, with two documented skips,
across three upfront whole-module partitions capped at 300 seconds each
(900-second outer limit). The FPGA software suite ran 1,577 selected tests with
75 documented skips, within its 840-second cap. Other software groups were
capped at 300 seconds. Every job had a heartbeat and disk-space guard.

| Selected group | Result |
| --- | --- |
| test_affected.py | PASS (0.4 s) |
| test_test_changed.py | PASS (1.0 s) |
| committed whitespace | PASS (0.2 s) |
| working whitespace | PASS (0.2 s) |
| parent Python regressions | PASS (231.37 s) |
| working-tree generated consumer consistency | PASS (4.2 s) |
| native SD firmware lease and PIO lifecycle | PASS (7.61 s) |
| native runtime firmware and FPGA launch integration | PASS (62.85 s) |
| embedded producer identity and native control integration | PASS (10.21 s) |
| contract schemas and generated fixtures | PASS (3.8 s) |
| embedded firmware and identity/drain RTL integration | PASS (6.01 s) |
| test_*.py | PASS (342.44 s) |

[Check receipts](software-result.json) record the exact commands, elapsed times
and log hashes. [Coverage summary](coverage-summary.json) records the partition
coverage and every skip reason. The skips concern unavailable compiler/oracle
fixtures, an uncached upstream U-Boot input and a macOS-specific check; they do
not qualify those omitted paths. Raw logs, the frozen selection and partition
assignment, focused results and development failures remain under
`out/nano-reconfiguration-20261102/`. The initial C fixture incorrectly resumed
masters during a control-only opening; its assertion failure and corrected
passing run are both retained. An initial `make check` refused the dirty module;
working-tree consistency and the required committed check subsequently passed.

## Evidence boundary and next work

The real board allocator still grants only one boot-pinned region. The native
caller does not select a warm transfer, the secure computer scope does not
reopen after it, and the raw-trace decoder still admits one cold cycle. Repeated
physical loads require completed-transfer-backed memory leases, a fresh
configuration-bound computer opening, a bounded two-load native diagnostic and
multi-cycle trace validation, then a rebuilt matched native/entry/SPL/BL31/FIT
chain. No ordinary Nano programming profile or native runtime admission is
granted. Tenfoot physical launch/return, active DDR, USB, video and native network
acceptance remain separate work.

The [previous native fetch observation](../2026-11-02-de25-native-fetch-hardware/README.md)
used older compiled native/firmware bytes and is not evidence for this change.
TRWU8CBL was untouched during this milestone. Its continuing claim and the
user-approved parked diagnostic state remain subject to a live ownership/UART/USB
check before another hardware action. No factory restore was required.

## Retention and shared compiler coordination

[Independent source retention](source-retention.json) records a complete
265,314,501-byte source bundle, SHA-256
`5c8db9be5f1e49ef318c05047f85d198874a8660777ee77f61761014e8231f1e`.
It has no prerequisite commit, was restored into a separate bare repository
without alternate objects and passed full Git integrity checking. The source
and validation records are retained at
`/home/deano/fes-retained/de25-native-reconfiguration-20261102/`; the final
archive receipt there records its independent file readback.

Every future Quartus job on `fes-build-3090` must hold the existing shared
exclusive flock `/tmp/lander-quartus-3090.lock` for the whole job. Never unlink,
replace or substitute a private lock. This compiler lock is separate from the
GPU leases. This milestone reused the existing FPGA source and ran no Quartus
job. Ownership stays in `lane:deano`; no PR handoff, push or merge occurred.
