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
AArch64 EL2→EL1 entry stub, and a dedicated U-Boot/SPL boot chain.

## Start here

| Area | Guide |
| --- | --- |
| **System architecture, boot chain & delegation diagrams** | **[ARCHITECTURE.md](ARCHITECTURE.md)** |
| TamaGo Go runtime, QEMU regression suites & model checks | [tamago/README.md](tamago/README.md) |
| Board selection (`qemu` vs `de25-nano`) & schema-8 handoff | [tamago/board/README.md](tamago/board/README.md) |
| Native SD launcher build, keyboard & clean-reboot settings | [nano/launcher.md](nano/launcher.md) |
| Host boot-chain tooling, QSPI packaging, recovery & JTAG gate | [nano/README.md](nano/README.md) |
| Contained TF-A (BL31) EL3 firmware, bridges & SDM control | [firmware/README.md](firmware/README.md) |
| Freestanding AArch64 EL2→EL1 boot entry stub | [entry/README.md](entry/README.md) |
| Native contained-fabric orchestrator (`nativefabric`) | [tamago/nativefabric/README.md](tamago/nativefabric/README.md) |
| Original feasibility study (historical context) | [de25-embedded-os.md](../../docs/design/de25-embedded-os.md) |

## Current services and boundaries

See [ARCHITECTURE.md](ARCHITECTURE.md) for how these components are wired and
delegate to each other.

| Service | Current implementation | Active boundary / open work |
| --- | --- | --- |
| Boot, CPU & core services | HPS-first SPL → guarded U-Boot `fesboot` (`--native-autoboot`) → `entry` EL2→EL1 → TamaGo EL1 ↔ contained BL31 EL3; [gic](tamago/gic/README.md) GICv3 + `WFI` idle, [dmaops](tamago/dmaops/README.md) uncached DMA pool, [psci](tamago/psci/README.md) reset/entropy, [rtc](tamago/rtc/README.md) PL031, [wdt](tamago/wdt/README.md) opt-in `L4WD0`/`L4WD1` supervisor with liveness probes in `fpgaload`, [fpgairq](tamago/fpgairq/README.md) FPGA-to-HPS doorbells | A matched R1 image cold-booted twice to Tenfoot ([#2026](https://github.com/DeanoC/fes/pull/2026)). Factory cold boot after rollback and native reinstall remain open under [#1867](https://github.com/DeanoC/fes/issues/1867) |
| Display | 1280×720 Tenfoot software renderer over 8 MiB two-slot DDR [scanout](tamago/scanout/README.md) (QEMU `ramfb` off-board), plus bare-metal [i2c](tamago/i2c/README.md) (DesignWare APB I2C) and [adv7513](tamago/adv7513/README.md) (HDMI TX / EDID 1.4 with `adv7513.hpp` register parity) driver packages | Physical HDMI observation on the matched native launcher artifact and board wiring of `i2c`/`adv7513` remain open |
| Input | [DWC2](tamago/dwc2/README.md) PIO USB2 host (single boot keyboard/gamepad or 16-ch `MultiHost` with Multi-TT hotplug) via [usbcontrol](tamago/usbcontrol/README.md) with GICv3 SPI 125 wakeup, `input/usbhub` + `input/controllers` (DualSense, DS4, XInput, Xbox GIP, Switch Pro, generic HID; Settings › Controllers family glyphs & per-pad remapping), [bt](tamago/bt/README.md) Classic Bluetooth (HCI/L2CAP/SSP/HIDP over USB dongles such as RTL8761BU), low-latency gameplay fast path, neutral-report fences & `Ctrl+Alt+Escape` return | Physical gamepad, Bluetooth dongle, and USB 3 hub qualification remain open |
| Storage & FAT32 | Owned [SDHCI](tamago/sdhci/README.md) (default 1-bit PIO; opt-in 25 MHz, 4-bit, multi-block, ADMA2 & GICv3 SPI 128 IRQ) + [sdcontrol](tamago/sdcontrol/README.md) + [block](tamago/block/README.md) LRU sector cache + [FAT32](tamago/fat/README.md) (64 KiB coalescing, CoW `ReplaceFile`, `RemoveFile`, optional RTC/SNTP timestamps) + `storagepolicy` (incl. swappable SD appliance mode) + [packagestore](tamago/packagestore/README.md) + crash-safe A/B [ota](tamago/ota/README.md) (`FES/OTA/*`) | Closed-bridge mutual exclusion (SD paused while a core runs); read-only default outside explicitly compiled writable card plans; physical ADMA2 awaits EL3 SMMU stream grant |
| Network | [network](tamago/network/README.md) (`lneto` TCP/IP, `HTTPServer`/`HTTPClient`, RFC 2131 DHCPv4 + T1/T2 renewal, RFC 1035 DNSv4, SNTP) + [mdns](tamago/mdns/README.md) (RFC 6762/6763 `_fogcast._tcp`) + [agentapi](tamago/agentapi/README.md) (`FogCast/targethttp` + `/v1/ota*` wired in QEMU `cmd/tenfoot`) over QEMU VirtIO-net or [XGMAC 2.10](tamago/xgmac/README.md) (1000BASE-T + opt-in 100BASE-TX, link-flap recovery, GICv3 RX IRQ, `dmaops`) | Disabled/dormant in default Nano SD boot profile; physical RGMII/PHY/SMMU MAC lease qualification remains open |
| FPGA lifecycle & cores | Contained EL3 SDM/SMMU config, master stop & bridge closure in [firmware](firmware/README.md) and [nativefabric](tamago/nativefabric/README.md); ZX81/ZX80 plus opt-in shared-core-shell Spectrum 48K and native C64 (including opt-in live D64 selector) | Full formal ownership, repeated/injected failure, DMA-drain and appliance-readiness audit tracked in [#1867](https://github.com/DeanoC/fes/issues/1867) |

## Verification and CI

- **Automated `tamago` CI lane** (`scripts/ci_tamago.sh`, `scripts/tamago_ci.py`):
  runs host `go vet` and `-race` tests, dual-board bare-metal cross-builds
  (QEMU `virt` and DE25-Nano), and 12 QEMU regression runs (`probe`,
  `mapcheck`, `fabric`, `keyboard`, `display`, `packages`, `admission`,
  `network`, `network-dhcp`, `tenfoot`, `storage`, `play`).
- **Kit boundary:** Standard DE25 HIL runs on Kit D25 (`TRWU8J8G`) using the
  Linux DE25 image; bare-metal TamaGo PRs cannot be tested on J8G yet
  (`AGENTS.md`). Historical DE25 bring-up and trial records are preserved under
  `docs/validation/*de25*`.
