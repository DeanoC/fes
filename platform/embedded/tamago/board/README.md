# Selectable native boards

`run.py --board qemu` (default) targets QEMU `virt` (VirtIO block/network,
xHCI, `ramfb`, software FPGA model). `--board de25-nano --profile FILE`
cross-builds with `-tags de25nano`, excluding QEMU drivers while sharing the
normalized USB/HID decoders and Tenfoot UI.

| Resource | QEMU (`virt`) | DE25-Nano (`-tags de25nano`) |
| --- | --- | --- |
| Boot / CPU / time | Fixed `virt` RAM, PL011 | Schema-8 profile RAM, EL1, selected 16550 UART (`0x10c02000` / `0x10c02100`), architectural counter |
| Storage / FAT32 | VirtIO block | Owned Cadence SDHCI PIO (`0x10808000`, SRS `+0x200`) via [sdhci](../sdhci/README.md) & [sdcontrol](../sdcontrol/README.md) |
| USB input | xHCI + shared HID decoder | Polling PIO [DWC2](../dwc2/README.md) (`0x10b00000`, boot keyboard + USB 2 hubs) |
| Network | VirtIO + Go TCP/IP | [XGMAC 2.10](../xgmac/README.md) + `lneto` (disabled in active Nano boot profile) |
| Display | `ramfb` + Tenfoot renderer | 8 MiB two-slot owned menu [scanout](../scanout/README.md) (or boot-pinned 4 MiB surface) |
| FPGA lifecycle | Software lifecycle model | Owned EL3 SDM/SMMU & bridge channel via [nativefabric](../nativefabric/README.md) and [arm64map](arm64map/README.md) |

## Firmware handoff (schema 8)

`build.py` generates an immutable per-build profile overlay from an explicit
schema-8 JSON handoff (direct `go build -tags de25nano` fails without it) and
validates all ELF segments against owned RAM:

- `schema: 8`, `firmware`, `uart_index` (`0` or `1`).
- `ram_start`, `ram_size`, `entry`: 2 MiB-aligned HPS DDR; `entry = ram_start +
  0x20000`, keeping the first 64 KiB clear for early tables/vectors and the top
  128 KiB clear for the initial Go stack.
- `reserved`: non-empty list of `{start, size, owner}` firmware/secure/DMA
  regions disjoint from Go RAM.
- `sd_input_clock_hz` (`200000000` or `0`) and `sd_control`
  (`fes-owned-sd-pio-v1` or `null`).
- `usb_phy_clock_hz` (`60000000` or `0`), `network`, `display` and `fpga_probe`
  (each explicit or `null`).
- `handoff`: `el1`, `single_cpu`, `counter_access`, `exclusive_devices`,
  `uart_configured`, `mmu_off_caches_clean` all `true`; `sd_platform_ready:
  false` (granted later via firmware lease); `usb_platform_ready`,
  `ethernet_platform_ready`, `display_platform_ready` matching their sections.

Example Nano cross-build:

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py storage \
  --board de25-nano --profile out/verified-firmware-handoff.json \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --output out/nano-storage-build
```

For full Nano Tenfoot launcher builds (`fpgaload`), see
[../../nano/launcher.md](../../nano/launcher.md).

## Source identities and hardware references

- SD/UART register definitions were verified against Altera's MIT-0 bare-metal
  driver snapshot (`66bdcc347564c5698a8997169bce83a31647d618`); no vendor BSP
  code is imported into the Go module.
- Agilex 5 SoC DTS (`socfpga_agilex5.dtsi`) identifies USB0 as DWC2 at
  `0x10b00000`.
- Live EL1 page-table and mapping checks are enforced by
  [arm64map](arm64map/README.md) across configuration copies, SMCs and FPGA
  MMIO.
