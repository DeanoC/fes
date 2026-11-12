# DE25 permanent DMA owner candidate

Producer `e89975869589550017d4db39e780f1a48d94e51b`, based on platform
`693e6f94de5cbd448e472c0ab68724a0c87eb268`.

The default-off, built-in `FES_DE25_DMA_PLATFORM` candidate now connects the real
DMA constructor, checked guard transport, post-client-close recovery handles
and an early OF-overlay interlock. Its root-only control device also requires
CAP_SYS_RAWIO. OWNER_ALLOC returns a CLOEXEC grant fd; OWNER_REAP retries the
existing hardware drain/revoke path and drops the owner handle only on success.
Client storage references continue to hold the backend lease after a reap.

Probe requires a permanent base-tree device, the generated control resource,
one SID-13 SMMU binding, a translated DMA/DMA-FQ domain and an exclusive device
group. The root-level FPGA region, direct manager, optional fixed bridge list,
SMMU and their bound parent/transitive supplier drivers must be permanent,
built-in and protected from sysfs unbind. The built-in candidate uses the pinned
driver-core API under SRCU to check actual device links; busy/unbound suppliers
defer, and removable/dynamic suppliers are refused. Ordinary module builds do
not require non-exported driver-core symbols.

A priority-100 notifier precedes the stock FPGA-region notifier. Live allocation
leases refuse PRE_APPLY and PRE_REMOVE before bridge disable or device detach,
including an unsubmitted engine or uncertain storage after the last client
closes. Otherwise configuration becomes unavailable before any transition.
Topology/ancestor/supplier mutations and per-image bridge/manager overrides
are refused. A successful firmware/profile POST_APPLY constructs a fresh guard
and verifies its new nonzero epoch. Unknown configurations are never probed or
adopted; epoch tokens do not wrap. DT compatibility describes hardware, not
artifact authenticity: the supported loader must supply the admitted image and
its matching overlay.

The pinned Terasic Agilex5 base tree has no Linux bridge list. This is preserved:
SDM-managed configuration does not require inventing a bridge node. An explicit
bridge list, when present, remains fixed and all its suppliers are checked.

Validation on this producer:

- All 38 required ARM64 QEMU KUnit cases pass with a clean raw kernel log;
  complete runner time 84.2 seconds. This test kernel enables the new candidate
  and OF overlays, so the built-in provider and private core API compile/link.
- Seven new cases cover early replacement/removal exclusion before the first
  submission, refusal while revoked client storage still exists, uncertain
  post-close drain/reap, unknown profile refusal without MMIO, topology overrides,
  ancestor changes, actual probe refusal, and an actual device link to an
  unbound supplier. Some concerns share a test case.
- All six affected software commands pass in 192.3 seconds. Generated
  consistency, documentation and committed whitespace checks also pass.
- The non-KUnit AArch64 module builds in 43.0 seconds against the private prepared
  6.12.11-fes-de25-platform tree. Image/configuration/symbol-version hashes are
  unchanged; ELF machine and vermagic are verified. This ordinary module has
  the candidate disabled. It was not loaded on a board.
- The existing combined RTL/client proof's 59 compiler and 51 runtime/test input
  hashes still match. No FPGA or runtime producer bytes changed.

The allocation/reap cases use the actual coherent allocator and file lifetime,
with scripted guard and endpoint registers. Notification cases invoke the actual
notifier decision path with test nodes; they do not run an OF overlay end to end.
The dependency case uses a real kernel device link, not physical SMMU devices.
Positive firmware/profile activation, concurrent actual overlays, the control
fd syscall path and physical supplier retention still need integration tests.
No physical stream, firewall, DDR, ARM Device-memory ordering or HDMI acceptance
is implied. The new provider remains disabled in ordinary builds and normal FES
runtime admission stays closed. The current kernel lacks the protected supplier
binding contract; there is no module parameter or DT waiver.

Complete test kernel, exported objects, configuration and matching ordinary
module are retained under
`out/artifacts/de25-permanent-owner/e89975869589550017d4db39e780f1a48d94e51b/`.

Next: integrate the pinned kernel's protected supplier bindings and base-tree
owner, prove actual loader/overlay/control lifecycle, then connect managed
AArch64 MENU/catalog/runtime and complete board load/run/reload/stop/recovery.
The FPGA timing/signoff and physical acceptance gates remain open. No board,
image, boot, SD or flash operation was performed.
