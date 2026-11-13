# DE25 positive OF-overlay and control-descriptor lifecycle

Producer `e6bdd35973cec91ee3ef93b92862680de3b7988f`, based on platform
`6736800dda12e8285bafc358310b792bb27cb450`.

The isolated kernel suite now loads compiled fixture overlays into the actual
OF tree and registers the real priority-100 owner notifier. A priority-zero
programmer models firmware completion/reset. The actual owner POST_APPLY path
recognizes the known profile, constructs and claims a new guard, and admits a
real coherent allocation through its control ioctl callback. The callback
installs a real CLOEXEC fd in the test task's private descriptor table.

Four new required cases pass:

- Two complete apply/allocate/submit/reap/close/remove cycles have increasing
  allocation generations and fresh guard epochs 1 and 2. Actual overlay removal
  returns EBUSY before later notifiers and node detachment while storage is
  live, including after reap while the client still retains its file. The
  modeled programmer's removal count and live-tree lookup check this boundary.
- Last client close with uncertain drain retains allocation/lease. Actual
  removal remains refused; checked reap after the same operation drains permits
  storage destruction and removal.
- PRE_APPLY programming failure and POST_APPLY wrong-guard-identity failure
  never admit an allocation. The actual changesets are removed, and a subsequent
  known configuration succeeds without adopting the failed guard or its epoch.
- Nonzero ioctl arguments, unknown commands and actual credentials lacking
  CAP_SYS_RAWIO are refused before register effects.

All 42 ARM64 QEMU KUnit cases pass, none skipped, with the unchanged raw-kernel
warning/oops/panic gate (53.9 seconds for the complete runner). Expected OF error
messages for refused transitions remain in the raw log. No leaked-node/refcount
diagnostics were observed. All six affected software commands pass in 194.1
seconds; generated consistency, documentation and whitespace checks also pass.

The non-test board kernel and base DTB were rebuilt from this same producer:
245.9 seconds for the complete runner, 235.8 seconds in the isolated build
container, release 6.12.11-fes-de25-owner. Built-in symbols/configuration and the
compiled DTB pass verification. KUnit hooks are off, and its System.map contains
neither owner test functions nor the compiled fixture-overlay symbols. The
exported Image has the ARM64 magic. This board kernel has not been booted.

KUnit-only glue supplies the modeled raw register transport to each newly
constructed guard and attaches the notifier to the fixture region. It also
enters the actual ioctl callback with the normal private-data layout. It does
not register/open the physical misc device or execute a userspace ioctl syscall.
The FD table, credentials, coherent allocator, grant operations, references,
final close, OF changesets and notifier chain are real kernel code. The
programmer does not call the SDM manager, fetch firmware or load an RBF. Physical
binding is bypassed only by KUnit fixtures; the normal provider contract is
unchanged. No actual supplier graph, SMMU/stream/firewall, physical DDR, bridge
clock/liveness, HDMI or board lifecycle is qualified.

Complete test and board kernels, configurations, symbol maps and receipts are
independently retained beneath
`out/artifacts/de25-owner-positive/e6bdd35973cec91ee3ef93b92862680de3b7988f/`.
The inventory binds every retained artifact. Earlier source-bound kernels and
records remain retained; no prior artifact is relabeled as this producer.

Next: matching candidate boot/rootfs packaging, actual SDM/loader and userspace
control integration, then physical boot/supplier/domain/DMA acceptance and
managed AArch64 MENU/catalog/runtime plus normal ZX81 lifecycle/recovery.
Concurrency and FPGA timing/signoff gates remain open. Normal runtime admission
stays closed. No board, image, boot, SD or flash operation was performed.
