# Command completion and retained final validation

The [format-10 physical trace](../2026-11-13-de25-command-observe/README.md)
reaches consumer 12 on the first poll, then times out in its first final
snapshot. That diagnostic is consumed. Exact original-FIT rollback/readback
passes, but its baseline final Quit/Park panics with USB ownership uncertainty;
physical reset and new restoration-only verification remain pending.

Format 11 makes the command-window endpoint explicit. The original 100 ms
origin and bound still cover initial snapshots, stores, publication, progress,
pointer checks and the accepted progress postguard. Only after those checks
accept the target does the retained retirement caller select a one-second
bound for the two mandatory final snapshots and final guard. That bound uses
the unchanged original command origin; the caller independently enforces its
one-second retirement origin, which is earlier and therefore stricter. The
20-second source origin/bound also remains. No clock is rebased.

This intentionally changes which deadline covers final validation. It removes
no register, table, source, clock or ownership check. The arm/fault setup
callers explicitly retain the full-operation 100 ms policy. Unknown policies
consume the attempt and fail before any callback. The driver records the
accepted completion sample and chosen policy separately; observation fields
never select the deadline or create a completion receipt. The receipt is still
issued only after all final checks. Buffers remain retained on every outcome;
no reuse, ordinary activation or interconnect-drain grant is introduced.

A new sanitizer case also exposes callback substitution between a raw register
read and its following barrier. The driver now captures and compares its
callback table/context around individual reads, stores, publication and
barriers, stopping before using a substituted callback. This applies to both
policies. The original failed fixture expectation and sanitizer crash logs
are retained separately; they are not relabelled as passes.

All 37 [focused tests](checks.json) pass. New cases include 488 policy/boundary
scenarios, 144 callback substitutions and 77 raw-read/retirement scenarios.
They cover both snapshot suppliers and individual-register fallback, invalid
policies, a completion-looking target returned after 100 ms, late final checks,
owner loss, changed callback/context, backward clocks and consumed retry. The
actual C finish record is parsed. The reader binds the new policy/completion
sample to the source and retirement clocks; old contracts and old queue records
cannot admit the new candidate.

The [paired models](model-result.json) use source
`3e6a7a89332632f6477e2d8b74a3508772d0d4a7` as the retained control. All 16
existing cost cases have identical supplier operations and outcomes. A shared
first-final-snapshot probe at 105,704 us reproduces phase 4/stage 7/error -3,
four writes and 954 controller reads in the old controller. The new controller
completes phase 6/stage 10 with all five writes, 1,186 controller reads, 188
counter calls and five source validations: exactly its zero-delay successful
path. A probe beyond the original retirement deadline still fails with -110,
four writes, 932 reads and 150 counter calls in both versions. These are
injected models, not calibrated latency or new physical acceptance.

Six [portable AArch64 units](portable-arm64.json), including both setup profiles,
compile with the pinned compiler and strict firmware flags. The largest
individual frame remains 384 bytes; this is not a whole EL3 stack or complete
matched-chain proof. New matching firmware, fresh admissions and a new bounded
trial remain necessary. The board has not been accessed during these edits.
