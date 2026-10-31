# DE25 guarded kernel register transport

Producer `0cb5468434a5a30f35ede28857a9da4f4ad608a8`, based on platform
`523ebe6cc5c3b91021892287d247a2c7871a48d7`.

The private kernel transport now implements endpoint `readl`/`writel` accesses
through the system-clock proxy. Every endpoint transaction checks guard ID,
version, timeout, idle state, faults and the nonzero epoch before and after the
access. Explicit barriers order completion before checking the result. A proxy
timeout's dummy zero cannot become successful endpoint evidence. Reset or a
response fault rejects the result; a poisoned software context never rearms in
place. Endpoint calls cannot address the guard's epoch/configuration registers.

The kernel owner must claim a fresh guard once, verifying the epoch write. An
existing epoch is neither overwritten nor adopted. The owner must retain its
resource/reconfiguration lease across all transport calls. Initialization
requires an already mapped, live system-clock window; this code cannot make a
dead/unconfigured HPS bridge or guard clock safe to probe.

Validation on the pinned AArch64 test kernel:

- All 28 required KUnit cases pass, with a clean raw kernel log (63.3 seconds
  for the complete runner). Six new cases cover real accessor/bounds behavior,
  wrong/foreign/ignored identity or epoch setup, dummy-zero timeout, post-access
  reset/metadata/response errors, pre-existing busy/unknown state, and a guarded
  transport connected through exclusive ownership to the actual DMA allocator.
- All six affected software commands pass (184.2 seconds).
- The non-KUnit module builds against a private copy of the prepared
  `6.12.11-fes-de25-platform` kernel (27.5 seconds). Kernel Image/configuration/
  symbol-version hashes are unchanged; AArch64 ELF and vermagic are verified.
- The existing combined RTL/client proof's 59 compiler and 51 runtime/test
  input hashes still match. No FPGA/runtime producer bytes changed.

The real accessor case uses kernel test RAM. Raw-register scripts model the
fault interleavings. These do not qualify ARM Device-memory ordering, physical
bridge liveness, SMMU translation or FPGA behavior. Existing RTL and generated
interconnect tests cover their own hardware model boundaries. Complete kernel,
objects and module are retained under
`out/artifacts/de25-guard-transport/0cb5468434a5a30f35ede28857a9da4f4ad608a8/`.

Production probe remains closed with `EOPNOTSUPP`. A real device/IOMMU owner,
supported reconfiguration/detach exclusion and normal managed MENU/catalog/core
lifecycle integration still need implementation and exact-artifact board
acceptance. No board, image, boot, flash or SD operation was performed.
