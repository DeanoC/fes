# Selectable native boards

`run.py --board qemu` is the default and executes the existing QEMU `virt`
backend. Its VirtIO block/network, xHCI and RAMFB drivers talk to emulated
hardware; they are not dummy Go functions. The FPGA lifecycle remains an
explicit software model. `--board de25-nano --profile FILE` instead cross-builds
with `-tags de25nano`; it never boots that image on QEMU or opens a device.
The selection happens before linking and runtime startup, not in `BOOT.JSON`.
All native applications now obtain peripherals through `board`.

| Resource | QEMU | Nano prototype |
| --- | --- | --- |
| Boot/runtime | Fixed virt RAM and PL011 | Profile-owned RAM, EL1, firmware-configured UART0, architectural counter |
| SD/FAT32 | VirtIO block | Cadence SDHCI, bounded single-sector PIO reads of a firmware-selected SDHC/SDXC card |
| Serial input | PL011 | UART0 16550-compatible registers, error-byte discard |
| USB input | xHCI plus shared HID decoder | DWC2 adapter unavailable; explicit error |
| Network | VirtIO and existing Go IP/TCP stack | MAC/PHY adapter unavailable; explicit error |
| Display | RAMFB and tenfoot software renderer | FPGA display/ownership adapter unavailable; explicit error |
| FPGA activation | Software lifecycle model | Physical configuration/identity/containment adapter unavailable |

Nano builds exclude VirtIO, xHCI, RAMFB and QEMU's DMA/page-table setup.
The shared USB descriptor/report decoder remains usable in both builds.
A successful cross-build does **not** mean tenfoot can display or launch on
hardware: the missing providers stop startup. `probe` and `storage` are the
first bring-up programs; `storage` mounts the same FAT32 implementation, reads
`FES/BOOT.JSON`, and prints its hash without writing anything.

## Firmware handoff

No default Nano resource profile is shipped. The delivery board revision,
firmware recipe and actual memory reservations have not been verified.
The build runner requires an exact schema-1 JSON object with these fields:

- `firmware`: identifying string for the firmware producing this handoff.
- `ram_start`, `ram_size`, `entry`: integer physical addresses/length; owned RAM
  must fit HPS DDR, align to 2 MiB, and leave 64 KiB for runtime tables/vectors.
- `reserved`: nonempty array of `{start, size, owner}` firmware reservations;
  none may overlap Go RAM. Record **all** firmware, secure and DMA reservations.
- `sd_sectors`, `sd_rca`: actual sector count and relative card address obtained
  from this boot's SDHC discovery, not values copied from another card/example.
- `schema`: integer `1`.
- `handoff`: object with `el1`, `single_cpu`, `counter_access`,
  `exclusive_devices`, `uart0_configured`, `sdhc_selected`, and
  `mmu_off_caches_clean`, all explicitly `true`.

These are firmware obligations, not facts the cross-builder can verify.
Firmware must park other CPUs, release the UART/SD controller and interrupt
ownership, configure DDR/clocks/reset/pinmux/ComboPHY, select the block-addressed
card into transfer state with no outstanding DMA, and enter at EL1 with clean
caches/MMU disabled and physical counter access. TamaGo installs its own vectors
and MMU mappings; no GIC/interrupt driver is claimed. UART0 is at `0x10c02000`
with four-byte register spacing; firmware retains the baud divisor/clock.
UART0 routing to the delivered board's console connector must be confirmed;
this register address alone does not establish the board routing.
The SD controller is at `0x10808000`, with SDHCI SRS registers at `+0x200`.
PIO needs no DMA arena or cache-coherency claim. All SD writes are refused.
No cold card discovery, removal/reinsert, eMMC or SDSC support is implied.
Runtime random bytes currently seed diagnostic hashes only; qualified entropy
and security-sensitive networking are still outside this board prototype.

`build.py` creates an immutable-per-build profile overlay, validates ELF load
segments against owned RAM and records compiler, selected source, generated
profile and ELF hashes. The original source tree is not modified. Direct
`go build -tags de25nano` intentionally fails without the generated profile.
The profile unit-test fixture is synthetic and is **not** a deployable recipe.

With the pinned compiler already installed, cross-build using an independently
verified firmware handoff file:

```sh
python3 platform/embedded/tamago/run.py storage --board de25-nano \
  --profile out/verified-firmware-handoff.json \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --output out/nano-storage-build
```

For the QEMU storage control, use `--board qemu --qemu PATH --disk IMAGE`
with an existing regular FAT32 image under `out/` that contains `FES/BOOT.JSON`.
The normal `play` and `tenfoot` QEMU tests still build and exercise their actual
application loops, including failure recovery and preference persistence.

## Source identities and next hardware gate

Register definitions were checked against Altera's MIT-0 bare-metal driver
snapshot [66bdcc347564c5698a8997169bce83a31647d618](https://github.com/altera-fpga/baremetal-drivers/tree/66bdcc347564c5698a8997169bce83a31647d618),
particularly `drivers/sdmmc/socfpga_sdmmc_reg.h` (Copyright 2026 Altera),
`drivers/uart/socfpga_uart_reg.h` and `platform/agilex5/include/platform_def.h`.
The Go transport is an exploratory implementation, not an imported vendor BSP.
The vendor source remains outside the delivered Go module.

Agilex 5's published [SoC device tree](https://raw.githubusercontent.com/torvalds/linux/master/arch/arm64/boot/dts/intel/socfpga_agilex5.dtsi)
identifies USB0 as **DWC2** at `0x10b00000`; earlier DWC3 references were wrong.
Current plans are corrected; dated validation receipts retain their original
bytes. Neither this controller fact nor the vendor development-kit examples
qualify Nano pinmux, PHY sequencing or a boot recipe.

On delivery: identify board revision and matching Terasic firmware; establish
actual reservations/entry contract; boot `probe` over serial; then boot `storage`
on disposable synthetic FAT32 media and verify its hash. Only then add cold SD
initialization and writable persistence. Implement and qualify DWC2, MAC/PHY and
FPGA display/activation separately while retaining QEMU as the integration control.
