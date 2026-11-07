# DE25 SDK cache and normal software development commands

Actual SDK/runtime proof: `1fdc544b61d934c31bffd18b38de39c925a94d53`.
Final test-ownership repair: `3adbce4660d58a9368d37d72ecfefda4efd0d7ae`.
Baseline publication: `c4af0b27cdf712ac5667e6aac3839f4436781603`.
This changes FES SDK selection/cache and software build orchestration. Component,
kernel, Buildroot, owner ABI and FPGA sources stay unchanged. The repair changes
only test ownership and regression assertions; SDK/Make/runtime implementation
bytes are identical to the actual proof. Subsequent
publication adds documentation only.

`make kit-sdk BOARD=de25-nano DE25_IMAGE=/absolute/verified-image` authenticates
the selected image and original Git producer before importing its original
AArch64 C++ SDK. Headless, managed-base and managed-factory receipts are supported;
missing or mixed receipts are refused. The cache key binds exact archive bytes,
board and architecture. Identical SDK bytes can share storage while each request
retains its actual image profile/producer and managed kernel/rootfs identities.
Cache hits still require valid original source authority. Corrupt cache entries
are replaced from that authenticated source.

The existing toolchain cache retains the original compressed archive unchanged
as `host.tar`, with its hash/key receipt. The existing software runtime builder
validates and copies it under the cache reader lock, then extracts and relocates
that private copy. No new OS, compiler or runtime builder is introduced.
`BOARD` and `DE25_IMAGE` also supply the ordinary `make kit-software` defaults;
existing explicit SDK/software overrides remain available and DE10 remains the
default. DE10 compiler construction and private extraction keep their existing
recipe. Standalone consumer changes do not invalidate the unrelated generic
cold-image script fingerprint.

## Executed scope

- All 53 focused cases pass in 0.8 s, including ten new cache, source-authority,
  factory-reader, runtime-copy and Make-selection cases. Existing DE10 and
  headless/managed-base controls pass. Cache tests cover byte-preserving import,
  failed import preserving a valid entry, reader locking, corruption repair,
  shared bytes with fresh producer metadata, changed-byte keys and refusal to
  substitute cache authority for a bad source. Managed-factory reader cases use
  the real receipt/parser and archived original Git producer as data, with
  kernel/package/boot authority explicitly mocked for unqualified unit fixtures.
- Actual public Make SDK import and unchanged cache hit, then the existing
  normal C++ runtime builder consuming that cache, pass in 42.5 s. The archive
  SHA-256 is `d5bac20323fe6225b984e5a0f35ac087bfb4379dca2b40245e4eab97bd019bf7`;
  neither cache archive nor receipt is rewritten on the second Make call.
  Original SDK, rootfs, Image, base receipt and Buildroot config hashes stay
  unchanged. The real managed-base producer remains
  `dfaffa8696fb164bf30576a6f7a1d7cd1844dd48`.
- The resulting daemon and canonical 29-member runtime archive pass ELF/base
  library closure and MENU interface inspection. Five AArch64 executables pass
  under QEMU Cortex-A76 using the authenticated SDK loader: computer,
  FPGA-manager, MENU, PR-control and production-hardware models. These exercise
  software models, not physical FPGA, owner DMA, reset, timing or native Linux.
  New version strings bind these binaries to the tested source; unchanged C++
  source does not imply byte-identical binaries to the previous proof.
- Nine actual command/admission refusal cases pass in 1.2 s, plus four Make
  selection/forwarding dry runs. The real extracted intermediate has no factory
  software directory; the first factory-admission gate refuses it. Complete
  public software compilation/publication is not attempted against that known
  incomplete base. The first proof expected a missing manifest filename, while
  admission correctly rejected its absent parent directory first. That failed
  assertion/log is retained; only the proof expectation changes for the retry.
- All 374 canonical MENU, 59 combined-compiler and 58 runtime-test input hashes
  are unchanged. Source/artifact scope verification passes in 0.1 s. No new
  managed image, full 14-scenario native/AArch64 RTL proof or Quartus run is made.
- The original full parent execution runs all 1,399 selected cases in 857.182 s
  (858.6 s job), with 47 skips and one failure: the focused room plan exceeds
  the unchanged below-half-suite requirement after adding SDK tests to its
  mandatory build group. The failed aggregate is preserved. Move the five
  standalone SDK tests into their own toolchain/media module, retaining their
  exact class AST and every other software test. New assertions verify room
  omission and media ownership; shared SDK implementation edits still select
  every group. No threshold or test is removed.
- Final six-command parent delta plus committed-source checks pass in 14.0 s.
  All 87 corrected-selector and affected SDK cases pass in 2.250 s, with real plan
  partitioning and all 1,399 test identities preserved after the explicit
  five-case move. The room plan selects 655 of 1,316 routine cases, preserving
  the strict below-half requirement. Unchanged tests/component evidence are reused through exact
  Git objects; this is not a fresh full-suite pass. Generated closure verifies
  40 consumers and 42 fixture copies. Original committed-source checks also pass
  in 5.6 s. Exact results and explicit content-based reuse are in [checks.json](checks.json).
  The ordinary ten-command affected plan is retained; it is not re-executed as
  a whole. The original preceding aggregate timeout remains separate.

[refusals.json](refusals.json), [source-scope.json](source-scope.json) and
[retention.json](retention.json) retain source/artifact identities, actual
commands and independently restored source. The archive includes the new daemon,
runtime library, CPU logs, proof scripts and original failed assertion.
Independent source restoration and all 62 regular archive members verify in
1.6 s; the proof archive is 1,858,016 bytes. Completion records verify that all
18 known owned job process/group identities are absent and both named private
runtime volumes have been removed.

No admitted factory, complete software update bundle or card is produced. The
SDK intermediate lacks factory software and MENU metadata; SDK/cache success
cannot supply them. HDMI output/whole-reset signoff, firmware qualification and
an eligible independent identical-RBF rebuild remain required. Native
PID1/FAT/root/SSH/peripheral, owner ABI3/SID/SMMU/coherent-DDR/security,
MENU/catalog ZX81/HID/BASIC/persistence, watchdog/update/restart/adoption and
recovery acceptance remain open.

No kit UART, network/SSH, capture, allocator, FPGA, SD, installed software or
kernel, flash or power is accessed. No baseline is restored, promoted or
released. Bottom TRWU8J8G retains its last authenticated ABI2 observation; top
TRWU8CBL is untouched. Only the two approved asynchronous-input exceptions
apply. No additional PCB files, invented delays, relaxed clocks, further timing
exception or SMMU bypass is used. Both consolidated PRs stay ready and
Deano-owned, without merge or handoff. The full normal-kit goal remains active.
