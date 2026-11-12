# DE25 permanent-owner kernel and base-tree build

Producer `2c8df6e7ee61e8ad03e828522908c4da667867f4`, based on platform
`0fb039a3dd94657d52e6c6073058d8d49031ac3d`.

The new tracked recipe builds the actual pinned Terasic AArch64 board kernel
with the permanent DMA owner and FPGA manager/region/bridge framework built-in.
A source-bound conditional patch protects region, manager and service drivers
from sysfs unbind for this candidate. The SMMUv3 and Agilex5 clock drivers were
already protected and are verified unchanged. The base DTB adds the permanent
SID-13 owner and generated control resource outside overlays, referring to the
actual SMMU and root-level FPGA region. No unproven dma-coherent property is
added, and the vendor's SDM-managed empty Linux bridge topology is preserved.

The normal image/kernel selection is unchanged. The candidate is a separately
built, unbooted kernel; normal FES runtime admission is still closed.

Validation:

- Full Image and DE25 board DTB build passes in 324.7 seconds for the complete
  runner (315.3 seconds in the isolated build container). Release is
  6.12.11-fes-de25-owner, with the owner/suppliers built-in and KUnit hooks off.
- Required built-in configuration and linked owner/allocator/FPGA-driver symbols
  are verified. The exported Image has the ARM64 magic. The compiled DTB passes
  the pinned libfdt checker for owner resource, stream ID, enabled SMMU, region,
  manager, absence of invented bridge/coherence properties and owner symbol.
- The exact build verifier refuses the previous compiled base DTB lacking the
  permanent owner node. This is a host DTB check, not board evidence.
- Four focused recipe cases pass: exact patch input/result authentication;
  refusal before mutation on changed input or patch; refusal of a wrong result
  hash; and rejecting modular owner/enabled test hooks while accepting omitted
  disabled symbols. All six affected software commands pass in 356.4 seconds,
  including the selected media/image checks. Generated consistency,
  documentation and whitespace checks pass.
- The DMA owner/guard/allocator sources are unchanged from the earlier
  38-case KUnit producer. This build does not run those tests inside the new
  board kernel; its KUnit hooks are disabled.

Initial producer bd60a8266 stopped before kernel compilation because the
configuration verifier demanded an explicit not-set line for a test symbol that
Kconfig omits when KUnit is disabled. The corrected verifier accepts absence
while refusing assigned values; the preserved configuration confirms KUnit was
off. Its first affected run stopped on whitespace inside patch context lines.
The patch now uses zero context, with complete before/result file hashes and
patch hash still mandatory. Original failed logs/configuration/job receipts
remain retained, rather than rewriting them as passing attempts.

The complete Image, base DTB, configuration, System.map, symbol versions,
verifier and receipts are independently retained at
`out/artifacts/de25-owner-kernel/2c8df6e7ee61e8ad03e828522908c4da667867f4/`.
The inventory binds every retained file by hash. No old vendor modules were
reused: the required FPGA/owner drivers are built-in. A matching rootfs/boot
package remains necessary; this recipe does not assemble or install one.

No board boot, module load, firmware/profile activation, actual overlay/control
fd lifecycle, physical supplier/domain/SID/firewall/DDR visibility or HDMI
acceptance is implied. Positive loader/control integration, matching boot/rootfs,
physical acceptance, managed AArch64 MENU/catalog/runtime and normal ZX81
load/run/reload/stop/recovery remain required. FPGA timing/signoff gates remain
open. No board, image, boot, SD or flash operation was performed.
