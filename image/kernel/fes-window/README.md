# fes-window kernel driver

`fes_window.ko` gives user space cached, revocable mappings of the reserved
HPS DDR window (`0x30000000`, 256 MiB; see [HPS DDR](../../../docs/hps-ddr.md)).
The FPGA reads that window through fpga2sdram, past the A9 L1 caches and the
PL310. With a cached mapping, a process draws directly into memory the fabric
scans out and writes back only the lines it changed, instead of copying each
frame through a strongly ordered `/dev/mem` mapping. This is step 1 of the
[kit display engine](https://github.com/DeanoC/fes/issues/949).

## Interface

`fes_window.h` is the user interface.

- `/dev/fes-window` (root, `CAP_SYS_RAWIO`) takes `FES_WINDOW_GRANT`, a
  page-aligned offset and length inside the window, and returns a new grant
  file descriptor. A grant file can be passed to another process.
- A grant file maps shared, readable and writable, never executable. Pages are
  inserted on first touch with the normal cached memory type.
- `FES_WINDOW_CLEAN` writes back a 2D region (rows of `width` bytes, `stride`
  apart) through L1 by address and then the PL310, so fpga2sdram reads see it.
- `FES_WINDOW_REVOKE` removes every process's mapping of the grant, then writes
  back and invalidates its lines, so no late eviction reaches memory a game core
  owns. Later accesses through an old mapping raise SIGBUS; later maps and
  cleans fail with `EACCES`. Closing the last reference also writes the grant
  back.

The module refuses a window that overlaps System RAM and claims it as
`fes-window` in `/proc/iomem`.

## Build and delivery

`make kit-software` builds the module against the pinned kernel
(`image/build/target-image.sources.lock.toml`), with `MiSTer_defconfig` and the
`-MiSTer` local version, so its vermagic matches the kit kernel. It ships at
`share/fes-window/fes_window.ko` in the software release, with `version`, the
digest of these sources that is also the module's `MODULE_VERSION`.

On start, the runtime loads its release's module with the window from the
generated `fes.memory.hps-ddr` contract. It keeps an already loaded copy only
when the version and window match, and otherwise unloads it and loads its own
(`sources/libmister-runtime/src/native/linux/window_driver.cpp`). Nothing maps
through the driver yet.

## Kit evidence

A dev build of this driver on kit A on 2026-10-21 showed the following:
- After revoke, an access raises SIGBUS, and both `mmap` and clean fail with `EACCES`.
- In 1000/1000 runs, DDR held the new data after a clean.
- A full 1280×720 frame costs 3.6 ms to draw cached and 7.4 ms to clean, against 31.2 ms to write through `O_SYNC`.

Details are on the [issue](https://github.com/DeanoC/fes/issues/949#issuecomment-6310766923).
