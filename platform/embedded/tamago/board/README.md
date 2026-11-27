# Selectable native boards (`board/`)

`board/` selects boot hooks, memory maps, and peripheral providers at compile
time (`-tags de25nano` excludes the QEMU BSP; FAT data cannot switch boards).
See [../../ARCHITECTURE.md](../../ARCHITECTURE.md) for the full delegation
graph and GICv3 IRQ map.

## Board matrix

| Resource | QEMU `virt` (default) | DE25-Nano (`-tags de25nano`) |
| --- | --- | --- |
| Entry & memory | Fixed `virt` RAM (`0x40000000` Go, `0x50000000` DMA), PL011 UART | Schema-8 profile RAM, EL1, 16550 UART (`0x10c02000` / `0x10c02100`), architectural counter |
| Interrupts & idle | GICv3 + `WFI` idle (PPI 30, PL011 SPI 33, PL031 SPI 34) | Agilex 5 GIC-600 + `WFI` idle (PPI 30, FPGA2HPS 48..111, USB0 125, SDMMC 128, UART 140/141, WDT 149/150, EMAC 222/239/256) |
| Storage & FAT32 | VirtIO block + `fat` | Owned Cadence SDHCI (`0x10808000`, SRS `+0x200`) via [sdhci](../sdhci/README.md) & [sdcontrol](../sdcontrol/README.md) (1-bit PIO default; opt-in 25 MHz, 4-bit, multi-block, IRQ; ADMA2 gated on SMMU) |
| USB input | xHCI ([usb](../usb/README.md)) + shared `input` HID | Owned [dwc2](../dwc2/README.md) PIO (`0x10b00000`, single keyboard/gamepad or 16-ch `MultiHost` with Multi-TT hotplug) via [usbcontrol](../usbcontrol/README.md) |
| Display | `ramfb` + `gfx.Software` | 8 MiB two-slot DDR menu [scanout](../scanout/README.md) (or boot-pinned 4 MiB surface) |
| Network | VirtIO-net + [network](../network/README.md) (`lneto`, static/DHCP/DNS/SNTP) | [xgmac](../xgmac/README.md) + `network` (disabled/dormant in default Nano SD profile) |
| Watchdog / RTC | PL031 [rtc](../rtc/README.md) | DesignWare APB `L4WD0`/`L4WD1` ([wdt](../wdt/README.md)), optional SNTP wall clock |
| FPGA lifecycle | Software model ([modelbackend](../modelbackend/README.md) + [fabricmodel](../fabricmodel/README.md)) | Owned EL3 SDM/SMMU & bridge channel via [nativefabric](../nativefabric/README.md) & [arm64map](arm64map/README.md) |

## Firmware handoff profile (schema 8)

`build.py` generates an immutable per-build Go overlay from an explicit
schema-8 JSON handoff (direct `go build -tags de25nano` fails without it) and
validates all ELF segments against owned RAM:

- `schema: 8`, `firmware`, `uart_index` (`0` or `1`).
- `ram_start`, `ram_size`, `entry`: 2 MiB-aligned HPS DDR; `entry = ram_start +
  0x20000` (first 64 KiB reserved for early tables/vectors; top 128 KiB
  reserved for initial Go stack).
- `reserved`: non-empty list of `{start, size, owner}` firmware/secure/DMA
  regions disjoint from Go RAM.
- `sd_input_clock_hz` (`200000000` or `0`) and `sd_control`
  (`fes-owned-sd-pio-v1` in `sd-profile.json`, `fes-owned-sd-dma-v1` in `sd-dma-profile.json` required for `--sd-adma2` / `FES_NANO_SD_DMA=1`, or `null`).
- `usb_phy_clock_hz` (`60000000` or `0`), `network`, `display`, and
  `fpga_probe` (each explicit or `null`).
- `handoff`: `el1`, `single_cpu`, `counter_access`, `exclusive_devices`,
  `uart_configured`, `mmu_off_caches_clean` all `true`; `sd_platform_ready:
  false` (granted later via EL3 SD lease); `usb_platform_ready`,
  `ethernet_platform_ready`, and `display_platform_ready` matching their
  sections.

## Build example

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py storage \
  --board de25-nano --profile out/verified-firmware-handoff.json \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --output out/nano-storage-build
```

For full Nano Tenfoot launcher builds (`fpgaload`), see
[../../nano/launcher.md](../../nano/launcher.md).

## Hardware references

- No vendor C BSP code is linked into the Go module (CGO is disabled).
- SD/UART register layouts were checked against Altera's MIT-0 bare-metal
  driver snapshot (`66bdcc347564c5698a8997169bce83a31647d618`).
- Agilex 5 SoC DTS (`socfpga_agilex5.dtsi`) defines USB0 (`0x10b00000`),
  SDHCI (`0x10808000`), and XGMAC0..2 (`0x10810000`..`0x10830000`).
- Live EL1 page tables and device/uncached mappings are verified at runtime by
  [arm64map](arm64map/README.md).
