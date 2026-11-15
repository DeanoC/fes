# DE25 preparation command publication and original-file restoration

The preceding [full-SOF hardware test](../2026-11-15-de25-full-sof-startup/README.md)
booted native code, loaded MENU and released its first source. Warm preparation
then stopped at 101.123 ms of command construction, before any queue slot store
or producer publication. Its enclosing four-second preparation had used
1.723 seconds. That failure remains immutable; it was not retried.

The new opt-in firmware selects preparation publication policy 4. Construction
and both mandatory final snapshots remain within the original four-second
preparation lifetime. Hardware completion retains 100 ms from a conservative,
owned prewrite-guard sample, accepted before the producer write and its final
barrier. The command and enclosing clocks are never restarted. All ownership,
register/table validation and six stores remain required. Retirement rejects a
missing publisher, malformed publication history or late completion before MMIO.
The private command state has no new fields. The failure trace now reports
retained `publication_seen` and `publication_us`; callback-poisoned tests prove
that this observation performs no timer, register or ownership call. Arm-only/fault-only setup keeps its
original full-operation policy. Profiles 22/23/24 reject old 19/20/21 contracts
and incomplete timing/version replacements. Retirement's separate two/one/four
second budgets, source release's one second and overall transfer's twenty
seconds remain unchanged.

The affected software checks pass across the retained runs: 61 tests passed in
the second expanded run; its one failing nested C fixture needed an explicit
return after renaming `main`. The final two queue fixture checks pass after
that repair, giving 62 unique affected tests passing across these runs. The
models cover 36 preparation queue boundary/fault cases, 16 real cold/warm
preparation cases, 10 additional invalid prepared-receipt cases and the existing
42 retirement queue cases. Initial runner/fixture compile failures remain in
the logs. All nine selected units compile with the pinned vendor AArch64
compiler. This is manually enabled object compilation, without a linked BL31,
admitted profile, complete EL3 stack qualification or physical acceptance.
[Model/source/object archive](model-retention.json) SHA-256:
`2d0c33b1211a14bda89b9c7e2a8be0f82a8ade1eb1654229d5c9d1ad6a44555b`.

After a fresh human power cycle, restoration-only procedure
`c34868fb09ff76d083e985960acd8b5073ebe08c4572c1e288e8ce67ee446dbf`
completed in 283.789 seconds. Independent replay verifies the recovery program,
one original FIT write and all 17 before-and-after file readbacks. Exact original
FIT SHA-256 is `d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`.
The [restoration archive](restoration-retention.json) verifies 79 entries:
`bff6358f4f26b15343d9122e1ad44587a4f78ebc1556a1da630470d496303ca1`.
All owned processes and UART/adapter handles are closed; USB permissions and
JTAG auto/15M settings are restored. The top kit remains claimed and parked in
the recovery loader with the original FIT on the spare card. The original
native runtime was not booted. QSPI, factory media and the bottom kit were
untouched. No launch/return/Park or general core-loading acceptance is inferred.

Next integration: freeze a fresh bounded repeated-loading test for the new
matched build below, using this actual restoration as its prerequisite.

The complete affected plan now passes across the recorded runs: 2,749 parent
tests (15 skips), native lifecycle/producer/firmware-RTL integration, and 2,181
FPGA tests (88 skips). The initial native integration attempt correctly rejected
an observer edit during verification. Its failed result is retained; only that
phase and the unstarted phases were rerun. The stable continuation's source
hashes match the frozen commit `82e607abffa26a182c02ffad82d1056f210b380e`.
This is phase-specific evidence, not a single immutable-checkout campaign.

A normal SDK, CPU entry, BL31, SPL/U-Boot, full-SOF composition and complete
native file admission pass on that frozen source. Chain:
`f1f3509d0510744b128700471ef88f81f0d000eaeae30374b231c7fd19a88460`;
boot ID `884dfd9259b18659`. FIT is 8,547,024 bytes, SHA-256
`319f03803044a6e395596b9d756b2de09de49bec3a48e920678800bb2e52d537`.
Full SOF SHA-256:
`743833427aef04dfe91531c337371f571a9c7f80ed23e33db2ca44487f926f98`.
The sealed computer/MENU payloads are unchanged. The private reservation still
has 192 bytes spare and four unchanged 8 KiB stacks. Packaging holds the shared
Quartus Pro lock, with no new synthesis, fitting, card write or device access.
No physical acceptance transfers from the older image to this candidate.

The [matched artifact archive](matched-retention-proof.json) verifies 46726 entries,
SHA-256 `5cb62f0626d29b5bad63f4b1f0299743c0b4c7977a67757b3df18864464502d6`. It retains the frozen source bundle,
normal producer outputs, native admission, compiler/layout checks and complete
affected-run logs, including the rejected source-change attempt.
