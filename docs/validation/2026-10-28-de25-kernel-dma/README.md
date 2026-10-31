# DE25 DMA ownership kernel validation

The Linux DMA ownership diagnostic passes eight actual kernel cases under
AArch64 QEMU from producer `45c2dfc247eccaae2556798a039a9390eaa15a6c`.
All required cases pass with zero skips and a clean raw kernel warning check.
This is kernel software evidence on a KUnit DMA device. The production DE25
probe always returns `EOPNOTSUPP`; no allocation device, module installation,
runtime admission, physical FPGA transfer or interactive MENU is enabled.

The work starts at `703f5215c23155418b125af87e310e50a30b083f`, then merges the
existing display foundation and adds the
[combined HPS/HDMI diagnostic](../../../sources/misteross/docs/validation/2026-10-28-de25-combined/README.md).
Both combined SOFs compile from the separate producer
`ec3ab70b608eb53a0b845d4462941f8a832af451`; the kernel producer does not relabel
those artifacts. Both retain failed overall design closure and physical gates.

## Tested kernel and source identity

| Input | Identity |
| --- | --- |
| Linux source | Terasic `d7d192a9ddd955fe206aa350afed8f82e368a74c`, 6.12.11 |
| SDK | Buildroot 2021.02.4, AArch64 GCC 9.4.0 |
| SDK SHA-256 | `e4c09c2ebe908a4fa1077b98122a9b561712345435ae1b2fe7d4c563d1ac12c7` |
| QEMU | 7.2.22, `virt`, `max,pauth-impdef=on`, software emulation |
| Builder | `sha256:ea200655b92ab1211bc8b326c6e799815f1d648b2d5e5a6c982f597cb2b9650d` |
| Vendor Image reference | `874f75d81f6452406f0f0ea5e0849ea269e92599fe728eb02b1bdbb56ada1aa8` |
| Extracted vendor IKCONFIG | `3ec2812cffb3c37bf677941283c9b65c3f5620b519cb58a4ded5f230cadbcdf7` |

The runner uses clean committed FES sources, verifies the locked SDK and
vendor Image/configuration, extracts the pinned Git kernel into a private
directory, and integrates only the selected driver plus two Kbuild/Kconfig
entries. The actual compile and QEMU boot run in the authenticated builder
with networking disabled. [inputs.json](inputs.json) binds all ten driver
inputs and the runner; [checks.json](checks.json) binds the test Image, actual
AArch64 driver/test objects, resolved configuration and raw/parsed logs.
Independent review verifies every source against Git and the retained snapshot,
all artifact hashes, and both objects' ELF64 AArch64 identity.

The retained output is
`/home/deano/.codex/worktrees/c134/fes/out/dev/de25-hps-display/out/de25-kunit-45c2dfc247/`.
The original vendor configuration is retained there as `vendor.config` for
comparison. [kunit.config](kunit.config) is the separate test kernel's actual
configuration, including CMA/32 MiB contiguous storage for 8 MiB allocations.
It is not the board's shipping configuration. The SDK's userspace 5.10 headers
are not substituted for kernel headers: Kbuild uses the pinned 6.12.11 tree.
The vendor kernel was built with a different compiler; module loading against
it remains untested. This run compiles the driver into the test kernel.

Reproduce from a clean checkout of the producer:

```sh
python3 scripts/de25_kernel_test.py fetch
python3 scripts/de25_kernel_test.py run \
  --image-output /absolute/de25-nano-linux-cxx \
  --output /absolute/new-kernel-output --jobs 8
```

## Observed cases and limits

[kunit.json](kunit.json) and [kunit-run.log](kunit-run.log) report all eight:

1. Production binding stays closed and rejects malformed/unknown ioctl use.
2. Invalid generation, sequence, slot or reserved fields have no start effect.
3. Kernel-owned descriptors and same-epoch drain control allocation lifetime.
4. A failed start retains backing until stop/drain succeeds.
5. An unreadable original epoch retains exposed storage.
6. Actual coherent DMA mapping, ioctl copies, genuine PFNMAP form, GUP refusal,
   revoke-before-stop ordering and post-revoke access failure.
7. Private, executable and out-of-bounds mappings are rejected.
8. A controlled concurrent mmap/revoke test waits across the pre-VMA-link window
   and revokes the installed mapping before release.

The driver accepts only the DMA mapper's genuine eager `VM_IO|VM_PFNMAP`
mapping with no mapper-installed operations. Unsupported ordinary-page/IOMMU
mapping forms are refused. The source review covers fast/remote GUP and flag
preservation boundaries; the kernel case directly checks ordinary writable
pin refusal. Test-only transport/completion hooks compile out of production.
No userspace address, module parameter or DT property can qualify hardware.

The kernel source review found the empty generic `vm_ops` placeholder, the
mmap callback/link race, and the need to retain backend context/domain resources
alongside allocation/device/module references. [source-review.md](source-review.md)
records the fixes and pinned primary evidence. Allocation ownership is distinct
from translation/firewall permission or physical DMA visibility.

The raw [kernel log](kunit-raw.log) contains no warning, oops, bug or panic.
The runner requires every named case and rejects those diagnostics before
publishing `checks.json`. Earlier attempts remain preserved and identified in
[attempts.json](attempts.json): the first failed Kconfig validation; the second
passed six cases and rejected two mappings because of the empty core operations
placeholder; the third reported eight passes but contained three test-lock
warnings and stopped during object collection. The final candidate corrects the
GUP test entry point, checks the raw log and exports the actual component objects.
Those earlier results are not substituted for the final clean run.

All selected parent, host/UI, runtime, contract and FPGA tooling checks also
pass. The affected run records 1,087 parent tests (two skipped) and 1,419 FPGA
tooling tests (35 skipped). [software-checks.json](software-checks.json) retains
the exact commands, starting checkout, log identity and results. The later
kernel/runner fixes are covered by the final eight-case kernel run and focused
runner/HIL tests; other module executable inputs did not change. `make check`
passes with 26 generated consumers and 37 fixture copies.

## Integration gate

This adds a separate kernel diagnostic ABI and a generated C consumer of the
existing `fes.fabric.de25-ddr-probe` constants. Existing runtime/GP interfaces
and production board admission are unchanged. The driver is not selected by
an image or software bundle; the HIL classification is `NO_DEPLOY_CHANGE`.

Activation still needs an actual fabric device/resource, DMA mask and stream
binding, backing permitted by the physical firewall, compatible firmware/SOF,
and global reset/drain ownership. The warm handshake is active low and
asynchronous to fabric; withholding ACK cannot prevent every timed-out HPS
reset. The combined design therefore drives ACK inactive and claims no global
quiescence. Unknown epochs or failed drain retain allocations.

The next integration joins allocated-buffer scanout and frame-boundary
completion to a qualified device/reset backend. Initial board acceptance still
starts with serial boot, DHCP/SSH and reboot of the separate headless image.
No kit, card, FPGA, QSPI or harness was touched by this work.
