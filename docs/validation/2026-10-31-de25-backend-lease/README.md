# DE25 DMA backend lease

Producer `a92247ffd8b3484464470c046a98ab67e26d901b`, based on consolidated
platform `905817f7365b944d6621b84af1abeaa7c53a7e5b`.

Previously two constructors could both observe an empty engine and allocate
independent grants before either submitted a frame. The private API also relied
on its caller to keep the callback/MMIO/device-domain context alive, without an
enforced acquisition/release boundary.

The constructor now requires an exclusive backend `acquire` before epoch,
identity, snapshot, DMA-mask or allocation operations. A failed acquisition
performs no transport access and releases no other owner's lease. Successful
acquisition is paired with one `release` after storage destruction, including
constructor-error cleanup. Uncertain DMA retains both storage and the lease;
successful drain does not release a still-live allocation's context. Callbacks
are private to the same module, whose existing file/exposure references retain
its code. This is not a userspace lease or a new allocation ABI.

Validation:

- All 22 actual ARM64 QEMU KUnit cases pass, with no warnings/oops/panic in the
  raw log. New cases cover missing hooks and acquisition failure before access,
  a second unconfigured allocation, constructor failure unwind, and retention
  through uncertain drain until storage destruction. The scripted transport
  rejects any access without its acquired lease throughout the suite.
- All six affected software commands pass (188.8 seconds).
- The non-KUnit AArch64 module builds against a private copy of the prepared
  `6.12.11-fes-de25-platform` kernel (8.5 seconds). Image, configuration and
  symbol-version hashes are unchanged; ELF and module vermagic are checked.
- The existing combined RTL/client proof's compiler and runtime input hashes
  still match. No FPGA/runtime source changed and no Quartus rebuild was needed.

Complete test kernel, objects and module are independently retained under
`out/artifacts/de25-backend-lease/a92247ffd8b3484464470c046a98ab67e26d901b/`.

These are kernel software and module-preparation results. The production probe
still returns `EOPNOTSUPP`. A physical provider must actually pin its MMIO and
device/IOMMU resources and prevent detach/reconfiguration for the whole lease;
callbacks alone do not prove those properties. Actual guarded MMIO binding,
managed AArch64 MENU/runtime/catalog integration and exact-artifact board
acceptance remain required. No kit, image, boot configuration, flash or SD
operation was performed for this change.
