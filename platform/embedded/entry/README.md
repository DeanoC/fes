# Experimental native boot entry

This loader connects a U-Boot `booti` handoff to the Linux-free TamaGo image.
Its header is compatible with U-Boot's arm64 Image path; it contains no Linux
kernel. The QEMU test executes the pinned Terasic U-Boot **source** built for
QEMU, not the Nano U-Boot binary or Agilex 5 CPU. A Nano-targeted output can be
assembled from an explicit firmware profile. Each Nano hardware result applies
to its recorded matched image; the compressed entry needs its own physical boot.

U-Boot's `booti` path performs its normal interrupt/cache/MMU cleanup and the
tested configuration enters the loader at EL2. The loader masks exceptions,
requires EL2 with the MMU and caches disabled, and rejects CPU affinity other than the primary A55/A76 (QEMU: zero).
During decoding and segment copying it installs a private EL2 identity map.
Only the owned Go RAM and page-complete loader are normal cached memory. Go RAM
is execute-never at EL2; the loader remains executable. Only the selected UART
page is Device-nGnRnE; every other address is invalid. Live CPU controls and
read/write translations are checked before decoding. It cleans and invalidates
the owned cache lines, disables translation and both caches, invalidates the
temporary TLB and restores the original EL2 SCTLR/TCR/TTBR0/MAIR before the same
clean EL1 handoff. Invalid payloads perform the same cleanup before parking.
It checks the entire static, little-endian AArch64 ELF, its executable entry,
file and memory ranges, the 64 KiB early-runtime reservation, the initial-stack
reservation at the top of Go RAM, and overlap with
the live loader before writing any segment. It copies initialized bytes, clears
BSS, synchronizes the instruction cache, enables the EL1 physical counter and
FP/SIMD access, and enters the native image at EL1. An exception before TamaGo
installs its own vectors parks in the loader. A malformed image parks instead of
launching. Hashes identify build inputs; they do not authenticate a payload.

The embedded ELF is stored as one deterministic gzip member. Before loading,
the freestanding decoder checks its fixed header, input/output bounds, exact
stream consumption, CRC and decoded size. The existing 32 MiB ELF limit remains.
It decodes into the final 32 MiB of owned Go RAM below the protected 128 KiB
initial stack. Every destination segment must exclude that entire temporary
workspace and the live loader; all checks finish before segment copying. The
workspace is no longer live when Go starts. Go's complete runtime metadata
remains in the decoded ELF. The Nano FIT load limit stays 8 MiB.

The allocation-free [puff inflater](vendor/puff/README.md) uses pinned, unmodified
zlib-contributed source with a private GCC jump shim. It links no C runtime.
The AArch64 loader requires the architectural IEEE CRC32 capability, checks the
CPU feature register before decoding, and computes the full gzip checksum with
aligned eight-byte loads and CRC32X plus bounded byte edges. Unsupported feature
values park before decoding. Host checks use an independent portable CRC oracle.
ELF copies and BSS clearing also use naturally aligned eight-byte accesses where
possible, preserving exact byte extents and every existing plan exclusion.
ARM64 compilation requires strict alignment through the uncached entry/exit. Inflater
static tables are materialized in the stored image; the builder conservatively
checks the sum of C stack frames plus exception headroom against the existing
16 KiB loader stack. EL2 and EL1 faults use the loader's own parking vectors.

The table planner requires 2 MiB-aligned Go RAM and a disjoint, page-complete
loader in the same 1 GiB window. It stores 22 table pages inside the loader;
all table addresses must lie within that image. The linker materializes the
last page's padding. After an initial layout link, the builder runs the same
allocation-free C planner on the host and relinks its expected table CRC as an
absolute symbol. Every layout address must remain unchanged. Runtime checks
that CRC before enabling the map. The CRC is a diagnostic binding, not payload
authentication or an ownership grant.

The temporary regime requires little endian operation, WXN off, a 4 KiB
granule, at least 40-bit physical addressing, bounded cache-line size, and no
VHE or stage-2 translation. Initial stale clean lines are invalidated only in
the owned RAM/image ranges. The exit's register-only assembly performs no
memory or stack stores after cleaning those ranges and checks the restored
controls. Other CPUs, DMA and firmware ownership remain caller obligations.

`FES_BOOT_STEP` records sample the physical CPU counter, frequency, exception
level and cache-control register at loader/decode/copy/handoff boundaries.
Separate `inflate_end` and `checksum_end` samples distinguish the two CPU costs.
Nano startup adds allocation-free checkpoints after the runtime enables its
MMU, installs vectors, enters `main`, verifies its context, opens SD, mounts FAT
and retains the fixed package. The separate `startup.py` decoder retains partial
progress and rejects counter resets, missing or duplicate records and invalid
cache state. These timings include trace output and confer no ownership,
package, drain or boot acceptance.

The checks are local to the entering CPU. They do not prove other CPUs are
parked, the firmware's memory reservations, UART clocks, peripheral and DMA
quiescence, or bridge containment. The caller must establish all of those before
using a Nano candidate. The loader has a separate staging window outside Go's
RAM so that Go's early allocator cannot overwrite its exception vectors before
it installs its own. The caller must keep that window reserved through startup.

The optional `fpgaload --computer-control --computer-reload` selection is carried
from the native SDK receipt into the entry receipt and shared SPL/BL31 chain
identity. Entry checks that this boolean agrees with the native link command;
it cannot be added to an image lacking the sealed computer contract. It selects
two fixed-package load/Run/return cycles with held data, not runtime admission.

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
checks the U-Boot build receipt and image hashes, then boots fourteen independent
QEMU guests: a complete native Go probe, corrupt ELF, loader/decode-workspace
overlap, bad gzip CRC, truncated DEFLATE, wrong decoded size, an extra member,
and entry at the wrong exception level. A target C payload executes the actual
copy/zero/CRC primitives across every byte alignment and word boundary against
independent byte/bit oracles and canaries. A private control masks the CPU's CRC
feature read to exercise real early rejection; this simulates an absent feature.
Further private controls simulate an unsupported cache CPU, corrupt the expected
table CRC, and use the wrong translation regime in the live observer. Rejection
must retain the clean CPU state and stop before native handoff.
Negative ELF cases relink the same
checked entry objects around private malformed payloads, so the actual target
planner must reject them after successful decoding. Each guest has one CPU and
its own output log. The failure cases must stop before the EL1
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

For Nano `bootcontext`, `fpgacontext` and `fpgastatus` payloads, the builder also revalidates the sealed
HPS-first project, descriptor digest and linked descriptor bytes. It passes
this source context to the matched SPL/BL31 cold-chain producer. Missing or
changed context rejects; the descriptor grants no physical handoff authority.
For `fpgacontext` and `fpgastatus` it also rederives the CPU control map from the exact compiler
handoff and checks its linked bytes and digest before passing it into the
cold-chain producer. The diagnostic's secure control permission is limited to
one identity read with continuously contained data paths.
`fpgastatus` instead requests only configuration status and keeps control closed.

Before a physical attempt, derive and review a Nano-specific RAM/firmware and
device handoff from the matched boot chain; verify other CPU and DMA ownership,
counter and UART access, and a controlled load and recovery procedure. Pair the
SPL with its selected FIT and FPGA configuration, then test that exact set. A
successful QEMU run or offline SOF conversion is not that proof.
