# Ownership subprofile source and matched chain

The retired c85f trial captured a 50.723-microsecond sampled ownership mean
and a scaled 0.8204-second estimate during the unchanged one-second tape timeout.
That identifies the combined callback, not its inner cost. Source/ELF inspection
alone does not establish a code-placement or cache fix. This diagnostic adds four
nested timing groups around the existing before-state, shared-USB binding,
paired reset-register read and after-state checks.

Original ownership predicates and ordering, register addresses/widths/barriers,
state/frame guards and complete tape engine/service/USB/boot validators remain.
The source comparison in `out/nano-ownership-parts-20261108/unchanged-checks.json`
checks these against the retained hardware baseline. Every CSR retains fresh
before/after ownership checks. No permission is cached and no deadline extends.
Only already-selected once-per64 outer ownership calls take the inner samples;
failed prefixes may have fewer inner samples. Four cached
`FES_NANO_TAPE_OWNERSHIP_COST` records print after failure/containment and before
the terminal marker. Sampling overhead consumes the original budget.

The read-only host decoder joins ordered inner samples to a previously checked
outer failed-transfer profile, including owner/generation, clock, count and
interval bounds. It rejects foreign, malformed, duplicated and incomplete records.
Results remain labelled sampled estimates, with no elapsed-time partition,
performance qualification, access grant or normal-return receipt. Strong actual
C-provider tests validate the output and reject altered records. Success/failure,
CPU/USB/reset drift, source integrity, expiry, replay and containment cases pass.
Two early decoder-integration runs had a local test variable collide with the
C profile string; the renamed record passes final focused and parent checks.

All selected software coverage passes: 1,741 parent tests with three skips,
generated-consumer consistency, native runtime/firmware/RTL launch integration,
producer control integration and embedded firmware/identity-drain RTL checks.
The broad FPGA software sweep hit its 300-second wrapper cap near the end with
no observed assertion errors. Its completed module prefix is retained; the five
unfinished modules passed separately under the same per-job cap. The original
partial receipt remains a timeout, rather than being relabelled a complete pass.

Source 5131d8bc6 produces matched boot ID `7abe602398c4db44`. BL31, U-Boot and
composition pass in 44.824, 107.256 and 67.035 seconds under unchanged bounds;
PFG holds the shared Quartus lock. The original OCRAM contract retains 7,360
bytes and final checks cover 2,261 source/artifact digests. The actual BL31 binary
contains the new marker under the explicit initial-tape selection. Exact SDK/entry
from e8fe4c9a6 remain unchanged, at their original paths and receipts, with all
selected source mappings checked current. No Go/native driver, shared wire
contract or FPGA payload changed.

The source/build retention target is
`/home/deano/fes-retained/de25-ownership-parts-source-and-build-20261108-r1`;
its successful receipt requires full archive readback and Git bundle verification.
Build/check trees are `out/nano-ownership-parts-runtime-20261108/` and
`out/nano-ownership-parts-20261108/`. The retained c85f board result remains
consumed one/max one. This image has no board attempts or hardware acceptance
at this evidence level. Top TRWU8CBL remains claimed with its spare installed,
factory restore waived unless needed, and bottom untouched. The next separate
one-use operator must retain its source, check current live admission and stop
at first failure. Tape/core-load/appliance readiness remains open.
