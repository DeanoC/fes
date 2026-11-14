# Command completion and retained final validation

The [format-10 physical trace](../2026-11-13-de25-command-observe/README.md)
reaches consumer 12 on the first poll, then times out in its first final
snapshot. That diagnostic is consumed. Exact original-FIT rollback/readback
passes, but its baseline final Quit/Park panics with USB ownership uncertainty;
the subsequent confirmed reset and restoration-only verification now pass
(see below).

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
individual frame remains 384 bytes; this is not a whole EL3 stack proof.

Production source `494ffcf5136123ab5abf0b804faa39a7afd95761` passes the complete
[affected campaign](checks.json): 2,665 parent tests (15 skips), native runtime,
firmware and producer integration, 2,181 FPGA software tests (88 skips), and
consistency for 69 generated consumers/45 fixture copies. Documentation links
pass. The [complete matched chain](matched-chain.json) also passes: selected
SDK, compressed entry, BL31, SPL/U-Boot, locked FPGA composition and full fresh
native file admission. The complete BL31's selected finish frame is 928 bytes;
that is still not a whole EL3 stack bound. No FPGA synthesis, fitting, timing
constraint or seal change is involved.

Chain `fc340ff0efd7803dc970ef7dcc3566fcffe94c43cb65afb7c6c125599f539c47`,
boot ID `d5a984131fbd2286`, FIT
`b080302e115f00cc5316b5bf033e2dc2886bed93e2842437b213e48ea4c926db`
(8,514,256 bytes within 16 MiB). The 1,373-file
[matched retention](matched-retention.json) independently reads back the new
archive. Both complete unchanged sealed project trees remain in the referenced
archive `9587828cdddfd72ab618154a1bb73bb1ccaaa7b3bbcfff8ebfb369991f4247eb`;
both archives are required. Compiled/model evidence remains bound to source
`494ffcf5136123ab5abf0b804faa39a7afd95761`; later evidence publication changes
only documentation. The [post-publication file check](publication-files.json)
confirms current native file/tree membership and all referenced archive hashes.
The subsequent [physical trial](../2026-11-13-de25-command-completion-physical/README.md)
passes retained-source retirement and full original-baseline restoration.
Its hardware evidence is recorded separately from this matched-build proof.

After the actual human confirmation “TRWU8CBL power-cycled; test card stays in”,
new original-baseline-only procedure
`4e8810501870b37bca3245751bd5baa275340b3f0d2ca5f1361e6abb77f62e11`
is consumed once. It selects the unchanged original baseline from source
`d580a60d93ad5dad87f0c311b762d0df2bd2beb5`, not format 11. The
[physical restoration](baseline-restoration.json) independently verifies the
sealed-SPL original-FIT boot, all 51 scripted exchanges, ZX81/ZX80 launch/return
and final Park at generation 5. The saved final trace includes shared USB STOP
at generation 3. Saved-wire/program-witness replay passes; all recorded owned
workers stop, UART is closed/free, JTAG auto/15 MHz and exact USB
identity/inode/permissions are restored. No SD refresh occurs; this boot proof
does not claim fresh file readbacks.

The [restoration archive](baseline-restoration-retention.json) retains and
reads back exact bytes, hashes and modes, with explicit prepared-procedure and
failed-trial dependencies. Current verified kit state is the original native
baseline at final Park, test card installed, host handles closed, top claim
held. QSPI, factory card and bottom kit are untouched. The format-10 failure
remains consumed and failed. A cold-reset restoration does not establish a USB
root-cause repair or general warm recovery. This baseline-restoration proof
alone qualifies no new mapping retirement,
interconnect drain, memory reuse or ordinary activation. A subsequent
[one-attempt format-11 trial](../2026-11-13-de25-command-completion-physical/README.md)
now passes the selected retirement diagnostic and full baseline restoration.
Normal activation, independent interconnect drain and memory reuse remain open.
