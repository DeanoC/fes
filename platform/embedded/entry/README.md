# Experimental native boot entry

This loader connects a U-Boot `booti` handoff to the Linux-free TamaGo image.
Its header is compatible with U-Boot's arm64 Image path; it contains no Linux
kernel. The QEMU test executes the pinned Terasic U-Boot **source** built for
QEMU, not the Nano U-Boot binary or Agilex 5 CPU. A Nano-targeted output can be
assembled from an explicit firmware profile, but no verified profile or
hardware-ready boot image is supplied.

U-Boot's `booti` path performs its normal interrupt/cache/MMU cleanup and the
tested configuration enters the loader at EL2. The loader masks exceptions,
requires EL2 with the MMU and caches disabled, and rejects nonzero CPU affinity.
It checks the entire static, little-endian AArch64 ELF, its executable entry,
file and memory ranges, the 64 KiB early-runtime reservation, the initial-stack
reservation at the top of Go RAM, and overlap with
the live loader before writing any segment. It copies initialized bytes, clears
BSS, synchronizes the instruction cache, enables the EL1 physical counter and
FP/SIMD access, and enters the native image at EL1. An exception before TamaGo
installs its own vectors parks in the loader. A malformed image parks instead of
launching. Hashes identify build inputs; they do not authenticate a payload.

The checks are local to the entering CPU. They do not prove other CPUs are
parked, the firmware's memory reservations, UART clocks, peripheral and DMA
quiescence, or bridge containment. The caller must establish all of those before
using a Nano candidate. The loader has a separate staging window outside Go's
RAM so that Go's early allocator cannot overwrite its exception vectors before
it installs its own. The caller must keep that window reserved through startup.

## QEMU check

Install the pinned TamaGo compiler and its locked Go modules as described in
the [native image guide](../tamago/README.md). Provide QEMU and a GNU AArch64
cross toolchain. U-Boot also needs its ordinary host build dependencies,
including Python setuptools/pyelftools, SWIG, dtc, bison, flex and OpenSSL.
The cross-prefix and QEMU paths below are examples; all outputs are new regular
files under this worktree's `out/`. The QEMU-only U-Boot build disables an unused
host EFI-capsule tool; it does not change the Nano firmware configuration.

```sh
python3 platform/embedded/tamago/run.py probe \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /absolute/path/qemu-system-aarch64 --output out/entry-native-probe

python3 platform/embedded/entry/build_qemu_uboot.py \
  --reference-repo out/vendor-uboot \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/entry-uboot

python3 platform/embedded/entry/build.py \
  --payload out/entry-native-probe/probe.elf \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/entry-image

python3 platform/embedded/entry/verify.py \
  --image-build out/entry-image --uboot-build out/entry-uboot \
  --qemu /absolute/path/qemu-system-aarch64 --output out/entry-check
```

The U-Boot checkout must be clean at pinned commit
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`; see the
[vendor firmware guide](../firmware/README.md) for its source. The verifier
checks the U-Boot build receipt and image hashes, then boots four independent
QEMU guests: a complete native Go probe, a corrupt ELF, a load segment
overlapping the loader, and an entry at the wrong exception level. Each guest
has one CPU and its own output log. The failure cases must stop before the EL1
handoff. The positive case checks timer, goroutine, allocation, garbage
collection and the volatile in-memory filesystem. It does not test SD/FAT32.

To exercise tenfoot's existing software-model launch and return cases through
the same entry, first run the [model Play test](../tamago/README.md#tenfoot-play-and-return-through-the-model)
and build an entry image from its `play.elf`. Then run:

```sh
python3 platform/embedded/entry/build.py \
  --payload out/native-tenfoot-play/play.elf \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/entry-play-image

python3 platform/embedded/entry/verify_tenfoot.py \
  --play-build out/native-tenfoot-play --image-build out/entry-play-image \
  --uboot-build out/entry-uboot --qemu /absolute/path/qemu-system-aarch64 \
  --output out/entry-play-check
```

The verifier boots a private copy of the model's FAT32 image and drives the
same launch, return and failure suite with the emulated keyboard and display.
QEMU U-Boot starts its emulated xHCI keyboard controller before the handoff.
The test checks the exact emulated PCI identity, stops U-Boot's USB controller,
clears its PCI command register and checks the readback before entering Go.
Without that step the native driver rejects U-Boot's still-enabled controller.
This shutdown sequence is QEMU-specific; it does not establish Nano DWC2 or
physical USB/DMA quiescence.

## Nano candidate

`build.py --target de25-nano` requires a native Nano cross-build, the exact
schema-8 handoff profile used for that build, and an explicitly selected
`--image-address` in a separate HPS DDR window at a 2 MiB base plus `0x80000`.
Staging must be disjoint from Go RAM and all declared firmware and device arenas.
It checks the build/profile/ELF and full selected-source hashes, selected UART, executable segments and
staging overlap. It does not establish that the profile's declarations are
true, or that U-Boot, TF-A, the FPGA, other CPUs, and DMA devices have released
the stated resources. The test fixture's profile is synthetic and must not be
used on a board. Output is a diagnostic file only; the builder does not install,
boot, sign, program, or otherwise contact a device.

Before a physical attempt, derive and review a Nano-specific RAM/firmware and
device handoff from the matched boot chain; verify other CPU and DMA ownership,
counter and UART access, and a controlled load and recovery procedure. Pair the
SPL with its selected FIT and FPGA configuration, then test that exact set. A
successful QEMU run or offline SOF conversion is not that proof.
