# Embedded FES base

**Opt-in experiment.** Set `FES_EXPERIMENTAL_DE25_EMBEDDED=1` when building
native Go, the CPU entry, BL31, SPL/U-Boot or flash images. This experiment
does not change the ordinary Linux DE10/DE25 image, release or board defaults.
Host/model tests require no opt-in and open no physical devices.

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 make embedded EMBEDDED_ARGS="--help"
```

This directory develops the Linux-free DE25-Nano stack: a single-core
bare-metal Go (**TamaGo**) EL1 application running FogCast's actual Tenfoot UI
and software renderer, backed by a contained TF-A (BL31) EL3 firmware layer, an
AArch64 C entry stub, and a dedicated U-Boot/SPL boot chain.

## Navigation

| Area | Guide |
| --- | --- |
| Native SD launcher, keyboard & clean-reboot settings | [nano/launcher.md](nano/launcher.md) |
| Nano cold boot, QSPI flash packaging, recovery & traces | [nano/README.md](nano/README.md) |
| TamaGo Go runtime, QEMU regression runner & model checks | [tamago/README.md](tamago/README.md) |
| Contained TF-A (BL31) firmware, bridges & SDM control | [firmware/README.md](firmware/README.md) |
| Experimental AArch64 EL2→EL1 boot entry stub | [entry/README.md](entry/README.md) |
| Native contained-fabric endpoint & host lifecycle | [tamago/nativefabric/README.md](tamago/nativefabric/README.md) |
| Board driver adaptation notes | [board-adaptation.md](board-adaptation.md) |
| Original feasibility study (historical context) | [de25-embedded-os.md](../../docs/design/de25-embedded-os.md) |

## Execution model

- **Single boot image at EL1:** TamaGo compiles the Go runtime, board drivers
  and Tenfoot launcher into one static AArch64 ELF (no Linux kernel, libc or
  dynamic loader). FPGA core packages stay separate sealed `.fcore` data and
  bitstream artifacts with full identity verification.
- **Cooperative single-core scheduling:** One CPU is active; interrupts stay
  masked. TamaGo has no asynchronous goroutine preemption, so driver waits are
  bounded and CPU-heavy work (including the Tenfoot software renderer) yields
  cooperatively.
- **Component ownership:** FES owns the base image and build orchestration;
  FogCast owns Tenfoot and application services; runtime contracts own FPGA
  programming, reset, input delivery and DMA lifetimes. The private
  `de25_embedded` programming registry and
  [`fes.fabric.embedded-computer`](../../sources/mister-packages/docs/embedded-computer.md)
  register map are independent of the Linux DE25 stack.

## Services and boundaries

| Service | Native implementation | Current boundary / open work |
| --- | --- | --- |
| CPU, RAM & boot | R1 QSPI / HPS-first SPL → guarded `fesboot` (`--native-autoboot`) → contained BL31 → EL1 TamaGo entry | Single-core parked; autonomous POR verified on `TRWU8CBL`; factory rollback round-trip tracked under [#1867](https://github.com/DeanoC/fes/issues/1867) |
| Display | 1280×720 Tenfoot software renderer over 8 MiB two-slot DDR [scanout](tamago/scanout/README.md) (QEMU `ramfb` off-board) | Physical HDMI verified for Tenfoot and ZX81; interactive frame throughput and broader core scanout remain open |
| Input | Polling PIO [DWC2](tamago/dwc2/README.md) USB host (single boot keyboard + USB 2 hub; neutral-report fences, `Ctrl+Alt+Escape` return) | No hotplug recovery, USB 3 hubs or physical gamepad qualification yet; [USB](tamago/usb/README.md) HID gamepad parser is host-tested |
| Storage & FAT32 | Owned PIO [SDHCI](tamago/sdhci/README.md) + [sdcontrol](tamago/sdcontrol/README.md) + [FAT32](tamago/fat/README.md) (`sd-checkpoint` preferences & catalogue installer) | Clean-reboot durability (not power-loss atomic); ordinary read-only default outside explicitly compiled writable card plans |
| Network | Software-tested [XGMAC 2.10](tamago/xgmac/README.md) + `lneto` TCP/IP (QEMU VirtIO NIC off-board) | Disabled in Nano boot profile; hardware RGMII/PHY/SMMU DMA containment remains open |
| FPGA lifecycle | Contained EL3 SDM/SMMU configuration, master stop and bridge closure in [firmware](firmware/README.md) and [nativefabric](tamago/nativefabric/README.md) | Paired MENU ↔ ZX81/ZX80 launch/return, tape load and cold POR passed on `TRWU8CBL`; full formal ownership/reset/DMA qualification stays under [#1867](https://github.com/DeanoC/fes/issues/1867) |

## Evidence and qualification boundary

- **Opt-in publication baseline:** [2026-11-13 opt-in publication record](../../docs/validation/2026-11-13-de25-embedded-opt-in/README.md).
- **Matched SD launcher & cold boot:** [2026-11-19 matched launcher preparation](../../docs/validation/2026-11-19-de25-launcher-matched/README.md), [2026-11-19 SD launcher wiring](../../docs/validation/2026-11-19-de25-sd-launcher/README.md), and [native SD launcher guide](nano/launcher.md).
- **Open system qualification:** [Issue #1867](https://github.com/DeanoC/fes/issues/1867) tracks the complete secure ownership, memory, repeated-failure, DMA-drain, factory-rollback and appliance-readiness audit. Standard DE25 HIL continues to use Kit D25 (`TRWU8J8G`) on the Linux image.
- Historical single-run bring-up and trial logs are retained under `docs/validation/*de25*`. The retired Oct 29 feasibility prototypes (`examples/de25-embedded-os` and `platform/embedded/zephyr`) were removed after the TamaGo base superseded them.
