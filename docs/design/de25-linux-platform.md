# DE25-Nano Linux platform acceptance

This page tracks the managed Linux development-kit contract for the Terasic
DE25-Nano (`de25-j8g`, serial `TRWU8J8G`): booting FES Linux, showing MENU,
installing and launching sealed Quartus Pro 26.3 cores, delivering input/media,
returning cleanly to MENU, and restoring/updating through supported interfaces.

- **Architecture, diagrams & build commands:** [DE25-Nano Linux guide](../de25-linux.md)
- **Kit D25 HIL, sealing (`de25:auto`) & baseline restore:** [DE25 kit HIL](../de25-kit-hil.md)
- **HPS-first QSPI bootstrap:** [DE25 persistent bootstrap](../de25-reboot-bootstrap.md)
- **Kernel DMA owner & board shells:**
  [`image/kernel/de25-dma/`](../../image/kernel/de25-dma/README.md),
  [`sources/misteross/boards/de25nano/`](../../sources/misteross/boards/de25nano/README.md)

## Platform acceptance matrix

| Area | Owner | Acceptance contract | Current standing |
| --- | --- | --- | --- |
| FES Linux baseline | FES `image/de25-nano/` | Managed SD + HPS-first QSPI pass cold startup, software reboots, and watchdog recovery with factory backup retained | **Active on Kit D25 (`TRWU8J8G`)**; independent `current-restore-de25.json` baseline promotion tracks `main` |
| Linux FPGA programming & DMA | `libmister-runtime`; `image/kernel/de25-platform/` & `de25-dma/` | Permanent-owner kernel (ABI 3, built-in manager/region/Stratix10 service, SMMU SID-13), repeatable loads without losing Linux/SSH, bounded drain/failure retention | **Active on Kit D25**; KUnit QEMU suite + physical lifecycle proven; arbitrary injected-failure/stress hardening stays ongoing |
| Board & package identity | `mister-packages`; `misteross` (`boards/de25nano/`) | Quartus Pro 26.3 sealed packages with exact handoff/CPU maps; mismatches rejected before device access | **6 registered recipes** (`fes.menu`, `fes.zx81`, `fes.spectrum`, `fes.atari-st`, `fes.c64`, `fes.pcxt-dos`); `pc386` HPS-window shell & runtime support in progress |
| Production runtime & managed software | `libmister-runtime`; `FogCast` | AArch64 `NativeHardware`, bundle install/activate/rollback, `mister-agent`, target discovery, and runtime-loss recovery | **Active on Kit D25**; SIGKILL recovery, bundle rollback/restore, and `de25:auto` HILQ exact-head software runs verified |
| MENU display & scanout | `misteross` scanout; kernel/runtime | Coherent 8 MiB two-slot 1280×720 BGRX8888 `fes.fabric.de25-scanout` DMA, bounded stop/drain, captured HDMI | **Active on Kit D25**; captured via `/dev/v4l/by-id/fes-ms2109-hil`; `boot_screen` + `core_video` automated checks in `hil-de25-core.sh` |
| Core launch, input, media & persistence | `FogCast`; `libmister-runtime` | Catalog install, Play, keyboard/media delivery, SAVE persistence, Stop back to MENU, and relaunch | **Verified for MENU + ZX81 baseline** (`de25:auto` seals only registered recipes whose design identity or toolchain selection moved, or MENU alone for software/runtime/image-only PRs; `core:de25-all` selects all six); full multi-core hardware acceptance & DE25 pc386 in progress |
| Static expansions / PR personas | `misteross`; `libmister-runtime`; `image/kernel/de25-platform/` | Static base + persona composition before core start | **Deferred** (Deano, 8 Nov 2026; hang tracked in [#1568](https://github.com/DeanoC/fes/issues/1568)); normal kit software keeps expansions disabled |
| Shared kit operations | FES (`scripts/powerboat/de25_hilq.py`) | Claim ledger (`de25-TRWU8J8G-STATUS.md`), exact-head HIL, serial-verified restore, and recovery without manual intervention | **Active on Kit D25 (`de25-j8g`)**; top board `TRWU8CBL` remains separate and excluded |

## Standing policies

- **Static expansion deferral:** Physical partial-persona acceptance is
  deferred from the ordinary DE25 development-kit milestone ([#1568](https://github.com/DeanoC/fes/issues/1568),
  [8 Nov waiver](../validation/2026-11-08-de25-ordinary-dev-kit/README.md)).
  Normal development software keeps expansions disabled.
- **Quartus Pro 26.3 only:** DE25 packages build exclusively with Quartus Pro
  26.3 under the shared [Quartus lease](quartus-hosts.md) on Powerboat or
  `fes-build-3090`.
- **Kit separation:** DE25 Linux hardware changes require exact-head Kit D25
  (`TRWU8J8G`) PASS with verified restore; DE10 kits cannot satisfy a DE25
  gate. Bare-metal TamaGo work (`platform/embedded/`) is separate and cannot
  run on J8G yet.
- **Historical evidence:** Detailed bring-up records, kernel/factory trials,
  watchdog proofs, and the 8 Nov ordinary-dev-kit audit are preserved under
  `docs/validation/*de25*` (see especially
  [2026-11-08 ordinary dev-kit audit](../validation/2026-11-08-de25-ordinary-dev-kit/acceptance-audit.md)).
