# DE25 permanent-owner kernel candidate

This opt-in preparation recipe builds a separate AArch64 kernel and base DTB
from the exact Terasic source, configuration, SDK and builder locked by
[the DMA kernel tests](../de25-dma/linux.lock.toml). It does not assemble an SD
image, replace the normal vendor kernel, deploy anything or enable normal FES
runtime admission.

The profile makes the DMA owner, FPGA manager, FPGA region and bridge framework
built-in. Its source-bound patch disables sysfs bind/unbind on the region,
manager, Stratix10 service and simple-pm-bus ancestor drivers only when
FES_DE25_DMA_PLATFORM is enabled. In the locked source, simple-pm-bus is built
with CONFIG_OF; there is no independent SIMPLE_PM_BUS option. The recipe
requires built-in OF and verifies the linked bus probe symbol. The real board's
SMMU is a child of that bound bus. Leaving the bus unbindable
would allow removal of a protected supplier through its parent.
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
actual SDM firmware loading and userspace control-syscall tests, matching
candidate rootfs and boot integration, physical SID/domain/firewall/DDR acceptance, managed MENU and
normal core lifecycle/recovery. The owner and device-tree description must not
be mistaken for package authentication or a safe probe of unknown fabric.

The separate offline assembler prepares that matching rootfs and boot image:

```sh
python3 scripts/de25_owner_image.py \
  --base /absolute/verified/headless-linux-output \
  --kernel /absolute/verified/kernel-owner-output \
  --output /absolute/new/owner-image-output
```

It authenticates the existing headless image receipt and candidate kernel,
preserves the userspace libraries and tools, removes the old vendor module
tree, and replaces the release marker, FPGA startup check and module manifest.
It prefixes the preserved device-manager rules with an explicit owner-only
mode-600 rule, including chmod for an existing devtmpfs node. The startup check
also verifies that mode and root ownership after mdev has run.
The startup check requires the built-in drivers and owner device. The assembler
replaces only the kernel and DTB in a private copy of the vendor FAT partition;
U-Boot, its boot script, the partition geometry and unrelated boot dependencies
retain their locked bytes. It uses the existing boot parser and image verifier.
No filesystem is mounted and no block device is opened.

Two independent assemblies must have identical image, rootfs, archive and boot
prefix hashes. The pinned unprivileged media-tool container has a 30-minute
limit and a 30-second heartbeat. Outputs include owner-image.json,
boot-contract.json and first-boot-expectation.json for the existing read-only
first-boot collector. Recheck with `python3 scripts/de25_owner_image.py --verify
/absolute/owner-image-output` using the same committed source and e2fsprogs on
PATH. This is an opt-in, unbooted candidate; normal runtime admission stays
closed. The SDK and standard headless image selection are unchanged.

The copied factory-boot artifacts are reference inputs for the shared boot
verifier, not instructions to replace the kit's approved HPS-first QSPI baseline.
Assembling an image does not install it or authorize a new flash operation.

After a separately delivered candidate boots, and before applying any FPGA
overlay, the claimed idle board can run the
[userspace refusal check](owner_unconfigured.c):

```sh
aarch64-buildroot-linux-gnu-gcc -std=c11 -O2 -Wall -Wextra -Werror -static \
  -o owner_unconfigured image/kernel/de25-platform/owner_unconfigured.c
./owner_unconfigured --boot-id <observed-candidate-boot-UUID>
```

Compile with the locked image SDK. The check requires the exact candidate
kernel, board/rootfs and observed boot identity, then holds the existing DE25
computer maintenance lock. It opens the root-only owner character device and
checks its sysfs device number and CLOEXEC flag. Actual ioctl calls must refuse
unknown commands, nonzero arguments and allocation before a known configuration;
a forked child drops all capabilities and must receive EPERM. The parent checks
that allocation is still refused afterward. It never publishes a frame or maps
MMIO. Unexpected allocation is a failure: it reaps only the new grant it just
received and closes that descriptor; EBUSY never triggers a reap. Use only
before configuration and while the whole board is exclusively claimed. The
maintenance lock alone does not serialize arbitrary root FPGA loaders.

Compiling this check, running its host-refusal path or passing KUnit does not
establish a userspace syscall pass. Its pass marker is evidence only when
observed on the identified candidate board boot.
