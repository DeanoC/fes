# DE25 factory compiler/base reuse and runtime refresh

Tested source: `35a29f752c6f0402f341350da56dd9742dc84cf5`.
Baseline publication: `d05041c5d79b0034692abe98029962f5911967d1`.
This changes the existing FES managed-factory cache and its tests. Component,
kernel, owner ABI, SDK, Buildroot package and FPGA implementation bytes stay
unchanged. Subsequent publication adds documentation only.

Previously each immutable output directory selected a different Docker volume,
and the factory's complete input key selected a different Buildroot work tree
after every software revision. The compiler and base packages therefore could
not be retained for routine selected-software changes.

One worktree/cache selection now uses one volume under the existing cache/build
lock. Its base work key requires the managed-factory profile, recipe, pinned
owner compiler and kernel identities. It omits only software producer, artifact
and helper selections; every additional policy field stays in that key. The
complete generation key, package preflight cache, software hashes and immutable
factory receipt retain those software identities. No output is overwritten and
no unqualified package is admitted.

Buildroot local-package stamps do not notice a newly selected prebuilt runtime.
Each actual factory build therefore runs `mister-runtime-dirclean` before its
ordinary Buildroot `all` action. The existing shared post-build recipe replaces
software and MENU/package trees. Factory ELF/library/interface checks,
rootfs verification, actual virtual boot and source-bound receipts still run.
Verification-only and download-only actions do not invoke that runtime refresh.

## Executed evidence

- All 56 focused cases pass in 0.9 s, including three new base-policy and
  refresh-failure cases. Software changes preserve only the compiler/base key;
  they still change the complete generation key. Recipe, SDK, kernel and new
  policy changes select another base. Incomplete/foreign identity is refused.
  A failed native-package refresh prevents build/SDK-export/verification and
  preserves prior diagnostic output.
- The actual pinned Buildroot 2021.02.4 package proof passes in 26.7 s using the
  authenticated owner compiler and existing pinned builder. It installs two
  retained real AArch64 runtime builds. The control selects the second runtime
  without refresh and observes the old first runtime still installed. Running
  the refresh then installs the exact second bytes, with all 59 observed
  compiler/prerequisite stamp identities unchanged. A wrong-hash selection
  fails the real package SHA check before target replacement, preserves the
  installed second runtime and leaves no successful installation stamp.
- This executes six real package Make commands and real SDK relocation/config
  generation. It does not invoke the full OS image build, package admission,
  rootfs/factory verification, virtual boot or CPU tests. The input metadata
  used only to select the base work directory is explicitly a proof fixture;
  it cannot authorize a factory. Both runtime producers and exact hashes are
  retained in [warm-package.json](warm-package.json). No C++ runtime is rebuilt.
- Current source/artifact scope passes in 0.1 s: all 374 canonical MENU,
  59 combined-compiler and 58 runtime-test inputs remain unchanged. The first
  scope guard expected seven Make stages although the executed script has six;
  its failed assertion is preserved. The retry verifies all six named stages
  and the actual bad-checksum error. No application behavior changes for that
  assertion correction.
- All six commands in the current affected plan and committed-source/package
  checks pass in 209.7 s. The complete routine parent selection passes 1,319
  cases with six skips in 197.894 s; generated closure verifies 40 consumers and
  42 fixture copies. The selection runs with both extended
  video and media modes disabled, as selected for these two changed paths.
  [checks.json](checks.json) retains the exact scope, counts and timings.
  Component lanes are not selected because their source inputs are unchanged;
  this is not a new extended FPGA/media, full combined CPU or hardware campaign.

[source-scope.json](source-scope.json) and [retention.json](retention.json) bind
the current code, independently restored source, original guard failure, actual
package logs and executable bytes. Raw pinned SDK and Buildroot inputs are
verified before and after the private proof. Its named container and volume are
removed after successful collection.
Independent source restoration and all 48 regular proof archive members verify
in 1.8 s. The archive is 2,381,539 bytes. Completion records also verify that all
12 known owned job process/group identities are absent.

This is the reuse foundation for normal DE25 development. `make dev
BOARD=de25-nano` is still refused: normal source selection must connect the
explicit DE25 Quartus MENU producer and AArch64 consumers through the existing
recipe/build pipeline. No admitted factory, full software bundle or card is
produced here. HDMI output/whole-reset, firmware/native disabled-F2H qualification
and an eligible independent identical-RBF rebuild remain required for MENU.
Full native factory, owner ABI3/SID/SMMU/coherent-DDR/security, MENU/catalog
ZX81/HID/BASIC/persistence, watchdog/update/restart/adoption and recovery
acceptance remain open.

No kit UART, network/SSH, capture, allocator, FPGA, SD, installed software or
kernel, flash or power is accessed. No claim/baseline is restored, promoted or
released, and no new native health observation is made. Bottom TRWU8J8G retains
its last authenticated ABI2 observation; top TRWU8CBL is untouched. Only the
two approved asynchronous-input findings may be reviewed. There are no
additional PCB files, invented timing, lower clocks, further exceptions or
SMMU bypass. Both consolidated PRs stay ready in `lane:deano`, without merge
or handoff. The full normal-kit goal remains active.
