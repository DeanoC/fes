# DE25 permanent-owner kernel candidate

This opt-in preparation recipe builds a separate AArch64 kernel and base DTB
from the exact Terasic source, configuration, SDK and builder locked by
[the DMA kernel tests](../de25-dma/linux.lock.toml). It does not assemble an SD
image, replace the normal vendor kernel, deploy anything or enable normal FES
runtime admission.

The profile makes the DMA owner, FPGA manager, FPGA region and bridge framework
built-in. Its source-bound patch disables sysfs bind/unbind on the region,
manager and Stratix10 service drivers only when FES_DE25_DMA_PLATFORM is enabled.
The pinned SMMUv3 and Agilex5 clock drivers already disable those attributes;
their complete source hashes are verified unchanged. No timing, DMA translation
or hardware qualification check is waived. The service buffer-claim recovery
candidate is not applied: bounded return alone is not a DMA drain proof.

The base-tree owner sits at /dma-owner@20010200, outside any overlay. Its
control resource and SID come from the generated scanout constants; its IOMMU
and FPGA-region phandles refer to the actual pinned board tree. The existing
SDM-managed region has no Linux bridge list and retains that topology. The owner
has no dma-coherent declaration: physical fabric coherence is not established.
The IOMMU remains in strict translated-DMA mode.

Run from committed source with the authenticated C++ SDK/image output:

```sh
python3 scripts/de25_kernel_platform.py \
  --cache /absolute/private/kernel-cache.git \
  --image-output /absolute/retained/linux-image-output \
  --output /absolute/new/kernel-owner-output --jobs 8
```

The output must be new and separate from image inputs. At least 30 GiB must be
free. The named private container reports a heartbeat every 30 seconds and has
a two-hour maximum; only that container is stopped on timeout. The source cache
and committed snapshot are read-only during the build; patches apply only to its
private source copy. Whole-file input/result hashes and the patch hash must
match. The recipe reuses the existing locked image builder and kernel extraction
helpers.

The build verifies required built-ins, release 6.12.11-fes-de25-owner, absence of
KUnit hooks, required linked symbols, and the compiled DTB using the exact pinned
libfdt implementation. Outputs include Image, de25-owner.dtb, kernel.config,
System.map, vmlinux.symvers, config.diff and source/hash/job receipts. FPGA drivers
are built-in; the vendor's old modules must not be reused with this kernel.

This is build preparation. It does not establish that the physical supplier
graph passes admission or that this kernel boots the board. Still required:
positive profile/actual-overlay/control-fd tests, matching candidate rootfs and
boot integration, physical SID/domain/firewall/DDR acceptance, managed MENU and
normal core lifecycle/recovery. The owner and device-tree description must not
be mistaken for package authentication or a safe probe of unknown fabric.
