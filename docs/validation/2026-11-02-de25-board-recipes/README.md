# DE25 board-scoped MENU recipe and package integration

Broad software and actual Quartus authentication source: `6909f1afdd6bcb72943e0f2f247d44740c2212f9`.
Final code: `08c9bd5106b2363febedf2450e5b239ae0f14934`. Its later changes are the DE10 nightly consumer/test,
DE10 support-matrix lookup/test and publication fault-injection test. Component,
registry and shared resolver Git objects are identical between those revisions.
Baseline publication is `744561d1925b2aa7e2e09fd48b82dfdcb91ff1f2`.
Subsequent publication changes documentation only.

The registry now selects one producer per core ID and board. Omitting the
board preserves all 21 DE10 descriptors. Explicit DE25 selection currently
provides `fes.menu` with the existing Quartus Pro producer and refuses absent
cores. The whole registry is validated, with duplicate IDs, selection filenames
and environment names scoped to each board. Ambient environment settings do
not select another board.

The DE25 producer exposes the common clean-source/tool-authentication,
execution-input and canonical-record API. Its normal CLI accepts the common
root/identity/cache inputs and retains explicit Quartus/vendor inputs. Missing
vendor artifacts are refused before source/tool/output work. The shared
resolver authenticates the existing producer; there is no second builder.
Package inspection compares the manifest target with the independently chosen
board, and publication/staging/recovery/artifact export carry that board.
Native timing, source/tool/execution, seal and independent-rebuild gates remain.
General same-board dual-toolchain selection and `FES_OSS_ONLY` are not added.

The raw-registry DE10 nightly and support-matrix validation now filter
explicitly to their DE10 board, so a later same-ID DE25 row cannot replace
their MENU producer. Regression cases cover both row orders.

## Executed checks

- The original current seven-command affected plan is partitioned into
  independent parent and FPGA software jobs on committed source `6909f1afd`.
  The parent run remains failed: 1,405 cases in 888.476 s, two errors and
  47 skips (889.7 s job). The errors expose a fault-injection callback lacking
  the new board keyword and a support-matrix test treating the final DE25
  vendor lock as an OSS lock. The matrix validator also needed its own
  explicit DE10 filter. These are corrected in `08c9bd510`.
- Final parent repair passes in 11.8 s: all 11 publication/matrix cases pass
  in 0.062 s, actual support-matrix validation passes, the five remaining
  current planner/whitespace/consumer checks pass, and `make check` passes
  (40 generated consumers, 42 fixture copies). Exact Git and test AST identity
  account for the original 1,356 passing cases: 19 passing cases in the
  affected suites are rerun, leaving 1,337 unchanged passing cases reused.
  All 21 existing affected cases and both new row-order tests are revalidated.
  The current planned selection has 1,407 cases, including the 47 unchanged
  skips. The count reconciliation is explicit in `checks.json`.
  This is not a new full parent-suite or aggregate pass.
- Full FPGA software verification passes in 961.6 s:
  1,762 cases in 960.651 s, with 76 skips. Extended video
  and media modes are enabled in the broad selection. Its component bytes
  are identical at the final code revision.
- All 15 producer API cases pass in 0.514 s on `3f2686066`; component bytes
  are identical at the broad/final revisions. Four staging discovery cases
  pass in 0.567 s on `6909f1afd`.
- All 12 nightly cases pass in 0.223 s on `1a5a08465`. The actual corrected
  nightly dry-run selects `build_fes_menu_package` and creates no output.
  All 21 default DE10 descriptors equal the previous publication's descriptors.
  Those nightly source/test objects are unchanged at the final code revision.
- Actual common-resolver authentication on `fes-build-3090` passes in 59.3 s
  with installed Pro 26.3.0 build114. It binds four tool identities, device
  A5EB013BB23BE4SCS, seed1, two threads, HPS-first initialization and Linux DMA.
  Read-only local/shared lookup finds zero eligible packages. No synthesis,
  export, FPGA programming or CPU/native acceptance is performed.
- Source scope verifies all 374 canonical files against that actual record.
  Only `scripts/build_de25_menu_package.py` differs from the historical compiled
  candidate; the recipe/canonical identity therefore changes. All 59
  combined-compiler and 58 runtime-test inputs remain unchanged. C++, Go,
  kernel, shared ABI, platform and image objects are unchanged. The historical
  RBF keeps its original source identity and is not requalified. Final Git
  object identity retains this audit at `1a5a08465` without rewriting its source.

Original failures remain preserved: the missing producer-test import,
publication return indentation defect, incorrect staging discovery invocation,
explicitly stopped first parent/FPGA groups and the two-error broad parent run.
The initial invalid bundle invocation and staging-output location/copy hashes
are retained too. Synthetic package publication and manifest/ELF fixtures grant
no package, factory or hardware authority. See [checks.json](checks.json),
[canonical-auth.json](canonical-auth.json) and [source-scope.json](source-scope.json).

Independent source restoration and all 85
regular proof archive members verify in 1.7 s.
The archive is 712,664 bytes. [retention.json](retention.json)
binds source, actual authentication, original/final logs and own completed
process/group absence, including the remote authentication job.

`make dev BOARD=de25-nano` remains refused. The next integration is the
AArch64 source consumers and persistent normal development path through the
existing builders. There is still no admitted DE25 MENU, factory/bundle/card
or new native test. HDMI output/whole reset, disabled-F2H firmware/native
qualification and eligible independent identical-RBF rebuild remain open.
No additional PCB files are available; only the two approved asynchronous-input
findings may be reviewed. No timing values, clocks or exceptions are changed.
Full native factory, owner ABI3/SID/SMMU/coherent-DDR/security, MENU/catalog
ZX81/HID/BASIC/persistence, watchdog/update/restart/adoption and recovery
acceptance remain required for the normal-kit goal.

No kit UART, network/SSH, capture, allocator, FPGA, SD, installed kernel or
software, QSPI or power action occurs. Bottom TRWU8J8G keeps its claim and
last authenticated ABI2 observation; top TRWU8CBL is untouched. No baseline
is restored, promoted or released. Both consolidated PRs remain ready in
`lane:deano`, without merge or handoff. The full goal remains active.
