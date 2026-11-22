# Experimental native boot entry

This freestanding AArch64 loader bridges U-Boot's `booti` handoff at EL2 to the
Linux-free TamaGo ELF at EL1 (no Linux kernel or C runtime).

## Boot and handoff contract

- **Entry checks:** Masks exceptions, requires EL2 with MMU and caches off,
  checks primary CPU affinity and architectural IEEE CRC32 support, and parks
  on any violation or exception before TamaGo installs its own vectors.
- **Temporary EL2 identity map:** Installs a 22-page identity map (table CRC
  relinked as an absolute symbol and verified at runtime) mapping only owned Go
  RAM (Normal cached, execute-never at EL2), the page-complete loader
  (executable) and the selected UART page (Device-nGnRnE).
- **Compressed ELF decode:** Embeds the static AArch64 TamaGo ELF as a
  deterministic level-9 gzip member decoded via the allocation-free
  [puff inflater](vendor/puff/README.md) into the final 32 MiB of Go RAM below
  the 128 KiB initial stack. Verifies gzip header, exact stream length, CRC32,
  segment bounds, 64 KiB early-runtime reservation and non-overlap with the
  loader before copying segments and clearing BSS.
- **Clean EL1 switch:** Cleans/invalidates owned cache lines, disables EL2 MMU
  and caches, invalidates temporary TLBs, restores original EL2 control
  registers, enables EL1 physical counter and FP/SIMD access, and jumps to the
  ELF entry at EL1.
- **Boot telemetry:** Emits `FES_BOOT_STEP` counter/EL/SCTLR checkpoints
  decoded by `startup.py`.

## QEMU check

Requires the pinned TamaGo compiler ([../tamago/README.md](../tamago/README.md)),
QEMU AArch64, an AArch64 GNU cross toolchain, and clean Terasic U-Boot at
commit `d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`
([../firmware/README.md](../firmware/README.md)):

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py probe \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /absolute/path/qemu-system-aarch64 --output out/entry-native-probe

python3 platform/embedded/entry/build_qemu_uboot.py \
  --reference-repo out/vendor-uboot \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/entry-uboot

FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/entry/build.py \
  --payload out/entry-native-probe/probe.elf \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/entry-image

python3 platform/embedded/entry/verify.py \
  --image-build out/entry-image --uboot-build out/entry-uboot \
  --qemu /absolute/path/qemu-system-aarch64 --output out/entry-check
```

`verify.py` runs 14 independent QEMU guests covering valid handoff plus corrupt
ELF, overlap, gzip/CRC/DEFLATE, wrong-EL, feature-mask and table-CRC failure
cases. To run the Tenfoot model Play/Back suite through U-Boot + entry, run the
[model Play test](../tamago/README.md#tenfoot-play-and-return-through-the-model),
wrap `play.elf` with `build.py`, and run `verify_tenfoot.py`.

## Nano candidate

`build.py --target de25-nano` binds a Nano cross-build, its schema-8 profile,
staging `--image-address` (2 MiB base + `0x80000`, disjoint from Go RAM and all
firmware/device reservations), and matched HPS-first/SDK receipts into the
16 MiB FIT layout (`nano/fit_layout.py`). See
[../nano/launcher.md](../nano/launcher.md) for the complete matched Nano boot
chain.
