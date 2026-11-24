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
- **Single-core cooperative scheduling with GICv3 IRQs & WFI idle:** One CPU is
  active. TamaGo has no asynchronous goroutine preemption, so driver waits are
  bounded and CPU-heavy work (including the Tenfoot software renderer) yields
  cooperatively. Programs can run purely polled or enable ARM GICv3 Non-Secure
  Group 1 interrupts (`gic.Controller`) with the `WFI` idle governor (EL1
  physical timer PPI 30) and device SPI wakeups; both `cmd/tenfoot` and
  DE25-Nano native Tenfoot (`cmd/fpgaload`) enable GICv3 + `WFI` idle at
  startup.
- **Component ownership:** FES owns the base image and build orchestration;
  FogCast owns Tenfoot and application services; runtime contracts own FPGA
  programming, reset, input delivery and DMA lifetimes. The private
  `de25_embedded` programming registry and
  [`fes.fabric.embedded-computer`](../../sources/mister-packages/docs/embedded-computer.md)
  register map are independent of the Linux DE25 stack.

## Services and boundaries

| Service | Native implementation | Current boundary / open work |
| --- | --- | --- |
| CPU, RAM, timers & boot | R1 QSPI / HPS-first SPL → guarded `fesboot` (`--native-autoboot`) → contained BL31 → EL1 TamaGo entry; [gic](tamago/gic/README.md) GICv3 + `WFI` idle, [dmaops](tamago/dmaops/README.md) uncached DMA pool, [psci](tamago/psci/README.md) power/reset + SMCCC TRNG/RNDR entropy, [rtc](tamago/rtc/README.md) PL031 wall-clock, [wdt](tamago/wdt/README.md) opt-in DesignWare APB watchdog (`L4WD0`/`L4WD1`) + supervisor keepalive in `fpgaload`, [fpgairq](tamago/fpgairq/README.md) FPGA-to-HPS doorbell/VSync dispatcher | Single-core Park and automatic application startup followed volatile JTAG delivery in the [2026-11-11 trial](../../docs/validation/2026-11-11-de25-native-autoboot-hardware/README.md), which did not prove standalone SD power-on. The [2026-11-19 matched launcher](../../docs/validation/2026-11-19-de25-launcher-matched/README.md) is off-board and its SD boot trial remains pending; factory rollback round-trip is tracked under [#1867](https://github.com/DeanoC/fes/issues/1867) |
| Display | 1280×720 Tenfoot software renderer over 8 MiB two-slot DDR [scanout](tamago/scanout/README.md) (QEMU `ramfb` off-board) | The [2026-11-01 Linux lifecycle diagnostic](../../docs/validation/2026-11-01-de25-shared-lifecycle/README.md) captured physical ZX81 HDMI on its named artifacts; HDMI observation for the [2026-11-19 matched native artifact](../../docs/validation/2026-11-19-de25-launcher-matched/README.md) remains pending. Interactive frame throughput and broader core scanout remain open |
| Input | [DWC2](tamago/dwc2/README.md) USB host with GICv3 SPI 125 IRQ wakeup (`Ready()` wired into Tenfoot alongside UART RX SPI; single boot keyboard + USB 2 hub; neutral-report fences, `Ctrl+Alt+Escape` return) | No hotplug recovery, USB 3 hubs or physical gamepad qualification yet; [USB](tamago/usb/README.md) HID gamepad parser is host-tested |
| Storage & FAT32 | Owned [SDHCI](tamago/sdhci/README.md) (with GICv3 SPI 128 IRQ) + [sdcontrol](tamago/sdcontrol/README.md) + [block](tamago/block/README.md) LRU write-through sector cache (lease-invalidating in `sdcontrol`/`fpgaload`) + [FAT32](tamago/fat/README.md) (64 KiB multi-sector coalescing, optional RTC wall-clock timestamps, `sd-checkpoint` preferences & catalogue installer) | Clean-reboot durability (not power-loss atomic); ordinary read-only default outside explicitly compiled writable card plans |
| Network | Software-tested [XGMAC 2.10](tamago/xgmac/README.md) (with GICv3 RX IRQ and `dmaops` pool) + `lneto` TCP/IP, opt-in RFC 2131 DHCPv4 client, pure-Go RFC 1035 DNSv4 A-record resolver, and controlled XGMAC DHCPv4 opener ([network](tamago/network/README.md); QEMU VirtIO NIC off-board) | Disabled in default Nano boot profile; hardware RGMII/PHY/SMMU DMA containment remains open |
| FPGA lifecycle & cores | Contained EL3 SDM/SMMU configuration, master stop and bridge closure in [firmware](firmware/README.md) and [nativefabric](tamago/nativefabric/README.md); ZX81/ZX80 plus opt-in shared-core-shell Spectrum 48K ([record](../../docs/validation/2026-11-22-de25-native-spectrum48.md)) and native C64 D64 (#2127, [record](../../docs/validation/2026-11-22-de25-native-c64/README.md)) | The [2026-11-11 volatile-JTAG diagnostic](../../docs/validation/2026-11-11-de25-native-autoboot-hardware/README.md) completed paired MENU ↔ ZX81/ZX80 launch/return, tape load and Park; it did not establish standalone SD power-on. Full formal ownership/reset/DMA qualification stays under [#1867](https://github.com/DeanoC/fes/issues/1867) |

## Evidence and qualification boundary

- **Automated CI lane:** Dedicated `tamago` CI lane (`scripts/ci_tamago.sh`, `scripts/tamago_ci.py`; #2150, #2152, #2153, #2165) runs host `go vet` and `-race` tests, dual-board bare-metal cross-builds (QEMU `virt` and DE25-Nano), and 11 QEMU regression suites (`probe`, `mapcheck`, `fabric`, `keyboard`, `display`, `packages`, `admission`, `network`, `tenfoot`, `storage`, `play`).
- **Opt-in publication baseline:** [2026-11-13 opt-in publication record](../../docs/validation/2026-11-13-de25-embedded-opt-in/README.md).
- **Matched SD launcher preparation (hardware trial pending):** [2026-11-19 matched launcher record](../../docs/validation/2026-11-19-de25-launcher-matched/README.md), [2026-11-19 SD launcher wiring record](../../docs/validation/2026-11-19-de25-sd-launcher/README.md), and [native SD launcher guide](nano/launcher.md). The matched build and file checks are off-board; SD power-on and physical HDMI observation remain pending.
- **Open system qualification:** [Issue #1867](https://github.com/DeanoC/fes/issues/1867) tracks the complete secure ownership, memory, repeated-failure, DMA-drain, factory-rollback and appliance-readiness audit. Standard DE25 HIL continues to use Kit D25 (`TRWU8J8G`) on the Linux image; bare-metal TamaGo PRs cannot be tested on J8G yet (`AGENTS.md`).
- Historical single-run bring-up and trial logs are retained under `docs/validation/*de25*`. The retired Oct 29 feasibility prototypes (`examples/de25-embedded-os` and `platform/embedded/zephyr`) were removed after the TamaGo base superseded them.
