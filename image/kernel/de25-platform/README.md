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
The same default-off profile repairs the manager's data-write loop: permanent
submission errors return their original errno, buffer/queue backpressure waits
for a callback, and the complete write phase has a 30-second jiffies deadline.
Every callback wait is capped by both the existing five-second buffer timeout
and that remaining phase budget. An error wins over simultaneous progress bits.
The deadline is manager-owned and persists across Linux scatter/gather fragment
callbacks. Completion polling also counts command and spurious-callback time
against its existing five-second phase budget.

The same flag bounds service buffer-claim inactivity and honors thread stop.
Busy polling yields; failure reports no buffers as returned. The manager marks
uncertainty before sending a request and retains it after any programming-phase
failure. A new caller is refused before commands or buffer-lock reset; failed
calls cannot resume write/complete. Only confirmed completion, all returned
buffers and a joined worker with no final error clear that state. There is no
userspace clear operation. Built-in protected suppliers keep the failed
manager and its buffers alive for that kernel epoch. This is not an SDM abort,
DMA drain or proof that reboot fences a failed transfer. Synchronous secure
calls and shared-service mutex waits still need qualification.
The [actual-function proofs](../de25-fpga/README.md) exercise the selected source
and retain hanging original controls and a retry regression. Normal runtime
admission remains closed.

The pinned SMMUv3 and Agilex5 clock drivers already disable bind attributes;
their complete source hashes are verified unchanged. No timing, DMA translation
or hardware qualification check is waived.

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
mode-600 rule, including chmod for an existing devtmpfs node. The standalone
userspace check verifies that mode and root ownership after mdev has run; it
uses fstat directly and does not require a stat utility in the headless rootfs.
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
PATH. Each new output is an opt-in candidate with no inherited hardware
acceptance; normal runtime admission stays closed. The SDK and standard
headless image selection are unchanged. The
[dated physical record](../../../docs/validation/2026-10-31-de25-owner-binding/README.md)
identifies the kernel/image producers exercised on the board, including the
protected parent bus, persistent permissions and real userspace refusal tests.

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

The matched root manifest now identifies `de25-hps-first-retained-v1` and the
SHA256 of the Image's linked note range. The assembler requires the kernel
receipt's complete supplier-source profile to match this source, then derives
the notes from its checked Image/System.map. The runtime compares them with
`/sys/kernel/notes` before configfs access, so a stale kernel sharing the same
release string cannot inherit this programming contract. This does not enable
normal runtime admission or establish hardware fencing after a failed load.

## Explicit allocated-buffer diagnostic

`owner_scanout.c` is an uninstalled maintenance test for this kernel's owner
ABI 2. It requires root, the exact AArch64 kernel/model/rootfs identity, an
observed boot UUID, the exclusive maintenance lease and a root-owned mode-0600
owner control device whose device number matches sysfs. Compile with the
selected AArch64 C SDK, with `-std=c11 -Wall -Wextra -Werror -O2`.

Before running `owner_scanout --boot-id UUID`, the caller must claim the board
and verify its SD CID, running kernel notes and matching image receipt, then
load and verify the selected HPS-first `--linux-dma` scanout artifact. Its
source-bound timing and generated-interconnect checks must pass first. The
program does not program an FPGA, accept CPU physical/DMA addresses or admit a
production profile. Do not run it against a foreign or uncertain configuration.

It obtains a CLOEXEC grant from the permanent owner and validates every geometry
field. It maps only the current back slot, writes three distinct colored frames
with a white corner marker and contrasting lower-right quadrant, unmaps before
publication, and requires each exact complete-frame acknowledgement before
reusing the previous front slot. Every completion wait has a five-second
monotonic deadline, and the process has a 30-second alarm. The image is held for
two seconds after each acknowledgement for external capture. A final healthy
observation precedes the single owner REAP request that must prove STOP/drain.
A syscall stall still requires kernel/secure-call qualification; the alarm does
not prove physical fencing.

On uncertainty it neither retries a publication nor requests owner REAP, and
never removes an overlay or firmware. Closing its grant is not a drain proof;
the permanent owner retains uncertain backing. A failed REAP is not retried.
The caller must preserve the configuration on failure rather than unload it.

The native syscall model in `tests/fixtures/de25_owner_scanout_model.c` runs
the actual test control flow and checks both-slot reuse, frame byte layout,
metadata/refusal, missing/wrong completion, hardware faults and single-REAP
behavior. It models the kernel ABI, not kernel code, SMMU translation, memory
traffic or physical drain. Run `python3 -m unittest discover -s tests -p
 test_de25_owner_scanout.py -v` from the FES root (without the displayed line
break). Physical allocated-buffer/HDMI acceptance remains pending.

Before closing a failed grant, the diagnostic prints `CACHED_FAILURE_INFO`
from the kernel's INFO operation. This reads cached metadata without issuing
another hardware snapshot, publication or reap. Last-close revocation can
prove STOP/drain and CLEAR, which otherwise destroys the original fault record.
The cached record is evidence about the failed attempt, not a drain proof.

## DMA mapping observability

The owner candidate now builds standard FTRACE, TRACEPOINTS, TRACING and
EVENT_TRACING support. The running bring-up kernel had tracing disabled, which
prevented observing the DMA mapping involved in a physical scanout response
error. The `iommu:map` and `iommu:unmap` trace events provide kernel-derived
IOVA/backing observations without adding addresses to the owner userspace ABI.
Tracing remains inactive unless explicitly enabled through tracefs. This adds
no alternate allocator, publication, physical-address input or DMA bypass.

The first intended qualification is an allocation-only observation, with no
frame submission, followed by the existing guarded owner reap. A trace-enabled
candidate must be booted and its exact kernel notes verified before that test.
The two failed frame tests on the earlier scanout image are retained; tracing
support does not establish a fix or authorize repeated frame submissions.
