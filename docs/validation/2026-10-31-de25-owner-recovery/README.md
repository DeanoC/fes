# DE25 DMA owner recovery after client close

Producer `17662d5106afd8aa94ee519f26b69c3b510e231d`, based on platform
`78beaba7e3b6df444ed265c3022741f707b4bb45`.

A private opaque owner handle now retains the allocation and kernel module
independently of the client file. The device owner obtains it while the file is
live, before handing that file to a client. After the last client closes, the
owner can retry the actual revocation path without dereferencing the dead file.
The original nonzero epoch, immutable grant and drained snapshot are still
required. Changed/missing epochs never qualify as drain. Dropping a handle does
not drop the separate exposure pin or revoke a live client. Storage destruction
still precedes backend resource-lease release. This does not change UAPI v2.

Validation:

- All 31 required ARM64 QEMU KUnit cases pass with a clean raw kernel log
  (63.6 seconds for the complete runner). Three new cases exercise actual last
  file close with pending drain, failed replacement allocation, successful
  same-operation recovery and subsequent allocation; missing/changed epochs;
  and owner-handle release while a client remains live.
- All six affected software commands pass in 192.2 seconds. Generated
  consistency, documentation and committed whitespace checks also pass.
- The matching non-KUnit AArch64 module builds in 30.5 seconds against a private
  copy of the prepared 6.12.11-fes-de25-platform kernel. Image, configuration and
  symbol-version hashes are unchanged; ELF machine and vermagic are verified.
- The existing combined RTL/client proof's 59 compiler and 51 runtime/test input
  hashes match unchanged. No FPGA or runtime producer bytes changed.

Complete kernel, objects, configuration and matching module are retained under
`out/artifacts/de25-owner-recovery/17662d5106afd8aa94ee519f26b69c3b510e231d/`.
The tests execute the actual allocator and file lifetime on a kernel test device,
with scripted register responses. They do not qualify physical bridge, DDR,
SMMU, firewall, ARM Device-memory ordering, HDMI or board recovery.

The pinned-kernel source inspection also confirms that stock of-fpga-region
ignores PRE_REMOVE and disables bridges only after overlay nodes are removed.
The overlay core allows an earlier PRE_REMOVE veto. Configuration exclusion
must begin before bridge disable and prevent new grants throughout replacement.
Ordinary platform binding holds the default-domain owner count; a device
reference alone does not preserve that binding. Exact source-file hashes are
retained in kernel-lifecycle-inspection.json. These are source findings, not
implemented interlocks or runtime evidence.

Production probe remains EOPNOTSUPP. The actual permanent device/domain owner,
early configuration/remove interlock, loader/guard connection and managed
AArch64 MENU/catalog/core lifecycle still require implementation and physical
acceptance. No board, boot, SD or flash operation was performed.
