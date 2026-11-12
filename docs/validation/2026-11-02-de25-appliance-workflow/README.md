# DE25 public appliance workflow

The existing release, bootstrap and card commands now select DE25 explicitly
through an actual managed-factory directory. The adapter supplies verified
board-specific inputs to the shared release exporter, AArch64 bootstrap
builder, media lease and two-pass regular-file card constructor/reconstructor.
The default DE10 path and its board-independent release schema remain intact.

Final tested source: `c68b96d12362a15dee715bf492c25a72c35db2f4`.
Implementation starts at `34632838c`; `98a6d6a89` initializes the existing
script-style imports and adds snapshot coverage, while `de56b0c4a` fixes only
the routing fixture's module patch. The complete software baseline is
`de56b0c4a`; `d3aff356f` then makes fresh report comparison read-only.
`c68b96d12` assigns the new adapter to the existing media recipe group, keeping
it out of the generic DE10 cold-build fingerprint, and extends that boundary's
existing check. Focused and affected parent coverage tests the final delta.
Subsequent publication changes docs only.

The explicit DE25 selector requires a sole `factory.json`, authenticates its
Git producer and current kernel policy, and runs the real `verify-factory`
command. Its structural report is now included in the factory receipt's closed
artifact hashes, alongside the actual factory boot report/log. Release
provenance uses those exact receipt/report/log hashes and the selected software
revisions. The current producer records one real image-build pass; an invented
second pass is refused. A saved pass claim cannot skip current verification.
Fresh verification compares without rewriting the original report, preserving
its bytes and file identity on both mismatch and success.

Profile/board mismatches, ambiguous or intermediate/headless receipts and
DE25 arguments on the default board are refused before publication. Both
bootstrap construction and retained source verification select AArch64. Card
preparation snapshots the checked kernel/DTB, package-bound idle and the entire
locked Terasic boot tree into private assembly storage; changed kernel or idle
payloads are rejected. The existing exact reconstruction/publication checks
remain shared. A DE25 media lease requires explicit board selection and keeps
the same global build lock and exclusive per-profile behavior.

## Executed scope

- All 54 final focused cases pass in 1.5 s, following the 53-case report-
  preservation pass in 1.7 s and the 52-case pass in 2.0 s. These include 12 new workflow/receipt/
  snapshot cases and the existing release/bootstrap/publication and DE10
  controls, plus the existing media/cold-build recipe boundary check. Fixture
  payloads are deliberately unqualified. Package/kernel/boot
  authority is mocked only inside those unit cases; they establish routing,
  hash binding and refusal behavior, not factory admission or CPU execution.
- Seven final Python/Make refusal cases pass in 1.1 s against the real
  managed-base image from `dfaffa869`: release, bootstrap, card build, card
  verification, Make release, a foreign profile and a missing board selection.
  The original rootfs/kernel/receipt hashes remain unchanged and no release,
  bootstrap or card output is created. Three Make dry runs check forwarding
  only; they do not build or boot an artifact.
- Source/actual SDK authentication passes in 2.3 s. All 374 canonical MENU,
  59 combined-compiler and 58 runtime-test inputs are unchanged. The real
  managed base/kernel/SDK and its earlier D-Bus boot evidence are reused for
  their original scope. That audit does not build an image or rerun the dated
  full 14-scenario native/AArch64 combined proof. The broader software plan
  separately runs its existing focused RTL and native-client simulations;
  none is timing, FPGA programming or physical acceptance.
- The broad ten-command affected run reaches the last FPGA software suite
  after the preceding nine commands complete. Its original 1,800-second
  timeout is preserved; only the unfinished command continues separately on
  the same frozen source. [checks.json](checks.json) distinguishes that
  aggregate receipt, the continuation and the final six-command report-
  preservation/cache delta. The timeout continuation repeats only the unfinished
  command; later parent checks cover the subsequent source changes.
  The standalone FPGA continuation passes in 866.7 s: 1,759 tests,
  76 skipped, no failures. The original prefix includes 1,336 parent tests
  with 45 skips (330.651 s), followed by successful host/UI, runtime and
  contract commands. Its timeout receipt remains unchanged.
  All six final delta commands pass in 826.6 s at `c68b96d12`, including
  the complete 1,378-case parent selection with 47 skips (820.272 s) and
  generated consistency. The earlier six-command report-only delta at
  `d3aff356f` also passes in 206.1 s (1,295 parent tests, six skips).
- Committed-source/package closure (`make check`) passes at the final source
  in 5.7 s: 40 generated consumers and 42 fixture copies match.
- The initial focused run remains **failed**, 2.1 s: the new adapter imports
  an existing script-style dependency without adding its script directory.
  The 2.4-second continuation also remains **failed**, solely because its
  routing fixture patches the package-style media module while the existing
  entrypoint uses the script-style module. The production routing checks and
  all other cases pass in that continuation. The final fixture correction
  changes no production behavior.

[refusals.json](refusals.json), [source-scope.json](source-scope.json) and
[retention.json](retention.json) retain exact commands, artifact identities,
original failures and independently restored source.
The source restoration/archive check passes in 1.5 s. It verifies all 86
regular proof members in the 365,574-byte archive and the exact restored Git
tree. Final completion records separately verify that all 34 known owned job
process/group identities are absent. The earlier timeout's observed descendants
are also absent. Archive and completion hashes are retained independently.

No complete admitted factory/card is built or exported here. Sealed MENU
still requires output/whole-reset signoff, firmware/native qualification and
an eligible independent FPGA rebuild. Top-level image construction/export,
optional independent two-pass factory builds, persistent data/expansion and
native watchdog/update integration remain work to complete. Native PID1/FAT,
SSH/peripherals, owner ABI3/SID/SMMU/coherent DDR, MENU/catalog ZX81 launch,
HID/BASIC/persistence and recovery require exact-artifact acceptance.

No kit UART, network/SSH, capture, allocator, FPGA, SD, installed software/kernel,
flash or power is accessed. Bottom TRWU8J8G remains claimed at its last
authenticated ABI2 observation; top TRWU8CBL is untouched. No baseline is
restored, promoted or released. Both consolidated PRs remain Deano-owned and
the full normal-kit goal stays open.
