# Reusable native configuration leases and computer sessions

Implementation: `6a600b94c8679a5ade45ad4320b56df79a54ee1a`, based on
`34cbd202744b839dc73673ac09e6a8d4e5b3685d`. This is software evidence for the
next native load cycle and a current-source CPU cross-build. No new matched
boot chain, board test or Quartus compile was performed.

## Behavior

The real Nano board allocator now uses a boot-pinned configuration arena with
one active lease at a time. Only checked successful transfer completion makes
that lease reusable. Outstanding or uncertain memory cannot be overwritten;
stale/duplicate completion callbacks cannot release the next lease. Copy or
ordering failure retires the arena, and failed initialization remains one-shot.
The full physical mapping stays pinned until reboot.

The compiled-source transfer constructor binds its completion privately to its
verifier, closed source and cold/rearm phase. `OpenComputer` requires that exact
completed transfer once, and a later opening also requires the preceding native
session's successful Hold, all-master reclosure and secure source check. Generic,
incomplete, failed, foreign and consumed transfers grant no opening. Old computer
clients remain retired. The native `fpgaload` caller now passes its actual
completed transfer rather than an observation supplied by the caller.

The secure computer scope consumes each new completed configuration generation
before readiness/opening and keeps its existing attempted and error state.
A newer bridge generation alone is insufficient. `NewStoredReconfigurationTransfer`
consumes the same privately retained archive after the matching native return;
the unchanged older SD receipt proves retention only, while fresh secure
ownership controls the new transfer. Resuming storage before START rejects;
no filesystem is reopened after containment.

## Completed checks

Focused native race tests execute three successful joined arena/transfer/Run/
Close cycles against injected secure calls and CSRs. They cover retained write/
release failures, lost opening/closure, identity mismatch, unsuccessful-return
exclusion, stale callbacks, competing reservations/completions, malformed
completion bindings and resumed-storage rejection. The strong C provider,
actual owned dispatcher and pinned vendor SHA fixture cover a second transfer,
renewed opening/close, and renewed readiness/open/close failures. That combined
configuration fixture uses the non-reader profile; it does not establish the
matched board inventory's second master-stop cycle.

All ten selected software groups passed on the implementation commit above.
The exact 1,303-ID parent selection ran in three upfront complete-module
partitions capped at 300 seconds each, with a 900-second outer cap. Two parent
checks were skipped. The FPGA software selection ran 1,577 tests with 75 skips
within its 840-second cap. Other software groups were capped at 300 seconds;
jobs had heartbeats and disk guards. `make check` also passed.

| Selected group | Result |
| --- | --- |
| test_affected.py | PASS (0.2 s) |
| test_test_changed.py | PASS (0.8 s) |
| committed whitespace | PASS (0.2 s) |
| working whitespace | PASS (0.2 s) |
| parent Python regressions | PASS (224.22 s) |
| working-tree generated consumer consistency | PASS (3.8 s) |
| native runtime firmware and FPGA launch integration | PASS (53.44 s) |
| embedded producer identity and native control integration | PASS (9.21 s) |
| embedded firmware and identity/drain RTL integration | PASS (5.2 s) |
| test_*.py | PASS (318.44 s) |

[Check receipts](software-result.json) and [coverage summary](coverage-summary.json)
record exact commands, log hashes, partition coverage and skip reasons. Skips
cover unavailable compiler/oracle fixtures, an uncached upstream U-Boot input
and a macOS-specific check; omitted paths are not qualified.

The [native cross-build check](native-sdk-check.json) verified all 1,407 selected
source hashes and the new `fpgaload.elf`, SHA-256
`7cf50215a6d177e77226e733314d5f4e022c695b910726310e5a96758976c259`.
This CPU-only image selects computer control and the held reader memory map,
with active DDR disabled. Its current caller still performs one initial load.
It has not been paired with a new entry/SPL/BL31/FIT chain or booted.
Raw logs, SDK output and the frozen selection remain under
`out/nano-repeat-load-20261102/`.

## Required matched-board gate

Inspection of the actual matched header and new SDK confirms control ABI
`0x10203041`, `FES_NANO_READER_CPU_BASE=0x95000000` and
`FES_NANO_COLD_COMPUTER=1`. The profile is
`de25-embedded-hps-first-ddr-diagnostic-v1`: the separate FPGA readers remain
part of the master-stop inventory even though active DDR is disabled and the
computer interface declares no HPS master.

The current `nano_cold_platform.c` retains `fabric_stop` and `fabric_generation`
for one activation. After a reload, reopening would reobserve the old stop
receipt under a different bridge generation and fail its ownership check.
This is a structural finding from current source and the matched profile, not
an observed hardware failure. The scope check must remain strict. A fresh
master-stop activation must be bound to positive completion of the new fixed
configuration, without clearing failed authority or treating a bare newer
bridge generation as permission. A combined strong-provider fixture with the
actual reader-plus-computer profile is required to cover it.

After that gate, add the bounded two-load native caller and multi-cycle trace
decoder, then build and verify the fresh matched native/entry/SPL/BL31/FIT chain
before a bounded TRWU8CBL trial. Configuration 20-second, native SD 100-second,
prompt 50-second and observer 45-second limits remain unchanged. Ordinary Nano
runtime admission, arbitrary package switching, Tenfoot physical launch/return,
USB/video/network acceptance and active DDR remain open. The full goal remains
active.

## Retention and coordination

[Independent source retention](source-retention.json) records a complete tested
source bundle with no prerequisites, independently restored without alternate
objects and verified by full Git integrity checking. The source and validation
archive are retained at
`/home/deano/fes-retained/de25-native-repeat-load-20261102/`; its archive receipt
records member-by-member readback. Prior physical fetch evidence remains
separately attributed to the older native and FPGA sources.

TRWU8CBL was untouched. Its continuing claim and user-approved parked state need
a live USB/UART/ownership check before any hardware action. The factory card
remains preserved; no factory restore was required. No PR push, handoff or merge
occurred; ownership remains `lane:deano`.

Every future Quartus compile on `fes-build-3090` must hold the existing shared
exclusive lock `/tmp/lander-quartus-3090.lock` throughout the job. Never unlink,
replace or substitute it. No Quartus invocation was needed in this milestone.
