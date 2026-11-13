# DE25 explicit Linux DMA profile preparation

Producer `a8c87794095a0cb594774021ffe8d9a4fbe86ec1`, based on
`11ebf46059e947001e8833946e7b819cf25578da`. This prepares the FPGA's actual
stream wiring and a matching custom-kernel build. Neither has been booted or
programmed on a board. No SD, QSPI or target software changes occur.

## FPGA stream path

The shared definition selects nonsecure stream 13. AXI address USER bits 23:8
hold the stream, bit 24 is zero, and the lower vendor attributes remain 0xE0,
producing 25'h0000de0 on both read and write address channels. This follows
[Altera's address USER/SMMU field table](https://docs.altera.com/r/docs/683130/25.3/embedded-peripherals-ip-user-guide/register-description?contentId=ubFvBQAlI8Vprt1qAASvfg).
The current vendor device tree assigns peripheral streams 1 through 10; the
fabric selection does not change those peripherals or disable the SMMU.

`--linux-dma` requires explicit `--hps-first`. The private project derives only
the vendor adapter's two master USER widths and two fixed address USER values.
It enables the underlying HPS component's SMMU port exposure. Generation checks
the exact permitted HPS parameter change, both 25-bit interfaces, the derived
source hash and the constant in the QIP-selected HDL. Unrelated HPS/EMIF and
boot-data changes still fail. [generation.json](generation.json) retains the
actual generated path and hashes. [hps-emif-parameter-review.json](hps-emif-parameter-review.json)
records the expected and actual false-to-true `f2sdram_SMMU` change.

The scope is the entire inherited F2SDRAM path: probe/debug and scanout share
this context. This is not isolated ownership of scanout alone. No device-tree
claim, kernel binding, physical translation or DMA lifetime is admitted by the
new constants or producer flag.

Quartus Pro 26.3 Build 114 compiles the frozen source on the second builder;
conversion produces the complete SOF, HPS JIC candidate and Linux core RBF.
The job exits zero after 537.4 seconds, with a 30-second heartbeat and a
7,200-second cap. Every original report and phase-file hash matches its receipt.
[linux-programming.json](linux-programming.json) binds the exact artifacts;
[build.json](build.json) binds compilation and inputs. The 74
[timing-summary rows](timing-summary.json) have nonnegative reported slack
and zero reported TNS. Hold includes rounded 0.000 ns. Setup/hold constraints
remain incomplete; six signoff rules fail and none are waived. Timing and
hardware acceptance remain unqualified.

## Matching kernel build

The kernel comes from exact Terasic source
`d7d192a9ddd955fe206aa350afed8f82e368a74c`. Its configuration is extracted and
hash-checked from the locked vendor Image. The C compiler is the authenticated
new AArch64 SDK from image producer `0da63d104`; its hash and compiler identity
are in [kernel-build.json](kernel-build.json). The configuration comparison
[kernel-config.diff](kernel-config.diff) contains compiler metadata, inactive
compiler-capability markers and the deliberate release suffix; it removes no
enabled vendor feature. SMMU v3, IOMMU DMA and CMA remain enabled.

The Image builds, then the four FPGA manager/region modules and the existing
closed DMA module build against that same kernel. The first module invocation
used a relative M path incompatible with the separate build directory; it
failed after Image completion. The retained Image is reused, the module stage
uses the actual source path and prepared vmlinux symbols, and it passes.
This changes neither vendor kernel code nor its failed-transfer cleanup.
The candidate release is `6.12.11-fes-de25-platform`, distinct from the running
vendor kernel. [module-identities.json](module-identities.json) verifies all
five relocatable ELF modules are little-endian AArch64 and carry this exact
release, with their dependencies. The Image has the AArch64 Image header.

The rebuilt board DTB exports the actual SMMU symbol for overlay references,
retaining the DE25 model. It adds no fabric DMA device. The DMA module's
production probe still returns EOPNOTSUPP; nothing in these artifacts bypasses
that gate. The separate bounded service-claim patch is not applied: bounded
error return alone does not prove firmware DMA quiescence.

Sixty-five focused cases pass: scanout/stream guards 15, HPS generation 21,
combined preparation 7, ZX81-HPS packaging 17 and bootstrap packaging 5.
`make check` validates 37 consumers and 37 fixture copies; docs and whitespace
checks pass. These do not establish a board boot, actual translation or MENU.

Both artifact sets, receipts, original reports, kernel configuration comparison
and failed/successful kernel build logs are independently rehashed and retained
read-only under
`/home/deano/fes/out/artifacts/de25-linux-dma/a8c87794095a0cb594774021ffe8d9a4fbe86ec1/`.
The complete generated FPGA database remains at
`fes-build-3090:/data/fes-de25-linux-dma-a8c877940/output/`.

Next: provide bounded access across the control clock crossing, establish
whole-master/device/domain and epoch lifetime, bind the real kernel DMA device,
and validate a captured frame plus STOP/drain/reset on the board. Managed
AArch64 software, production runtime, MENU, catalog ZX81 launch/stop/relaunch,
persistence and supported update/recovery remain required for normal-kit parity.
