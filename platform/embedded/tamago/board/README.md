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
| SD/FAT32 | VirtIO block | Cadence SDHCI host/card initialization, CSD capacity discovery and bounded single-sector PIO reads |
| Serial input | PL011 | UART0 16550-compatible registers, error-byte discard |
| USB input | xHCI plus shared HID decoder | DWC2 PIO for one direct low/full-speed boot keyboard; firmware ULPI/VBUS readiness required |
| Network | VirtIO and existing Go IP/TCP stack | MAC/PHY adapter unavailable; explicit error |
| Display | RAMFB and tenfoot software renderer | FPGA display/ownership adapter unavailable; explicit error |
| FPGA activation | Software lifecycle model | Physical configuration/identity/containment adapter unavailable |

Nano builds exclude VirtIO, xHCI, RAMFB and QEMU's DMA/page-table setup.
The shared USB descriptor/report decoder remains usable in both builds.
A successful cross-build does **not** mean tenfoot can display or launch on
hardware: the missing network/display providers stop startup. `probe`, `storage` and `keyboard` are the
first bring-up programs; `keyboard` reports keys over serial, while `storage` mounts the same FAT32 implementation, reads
`FES/BOOT.JSON`, and prints its hash without writing anything.

## Firmware handoff

No default Nano resource profile is shipped. The delivery board revision,
firmware recipe and actual memory reservations have not been verified.
The build runner requires an exact schema-2 JSON object with these fields:

- `firmware`: identifying string for the firmware producing this handoff.
- `ram_start`, `ram_size`, `entry`: integer physical addresses/length; owned RAM
  must fit HPS DDR, align to 2 MiB, and leave 64 KiB for runtime tables/vectors.
- `reserved`: nonempty array of `{start, size, owner}` firmware reservations;
  none may overlap Go RAM. Record **all** firmware, secure and DMA reservations.
- `sd_input_clock_hz`: actual SDHCI input clock established by firmware, between
  400 kHz and 200 MHz. The driver divides it to at most 400 kHz for both card
  identification and initial read-only operation, using a one-bit bus.
- `usb_phy_clock_hz`: `60000000` for admitted ULPI USB, or `0` to disable USB
  explicitly. Disabling USB allows SD/serial bring-up before PHY/VBUS work.
- `schema`: integer `2`. Obsolete capacity/RCA fields and schema 1 reject.
- `handoff`: object with `el1`, `single_cpu`, `counter_access`,
  `exclusive_devices`, `uart0_configured`, `sd_platform_ready`, and
  `mmu_off_caches_clean`, all explicitly `true`. `usb_platform_ready` must be a
  boolean matching the USB clock declaration: true for 60 MHz, false for 0.

These are firmware obligations, not facts the cross-builder can verify.
Firmware must park other CPUs, release the UART/SD controller and interrupt
ownership, configure DDR/clocks/reset/pinmux/ComboPHY for SD identification at 3.3V with
no outstanding DMA, and enter at EL1 with clean
caches/MMU disabled and physical counter access. TamaGo installs its own vectors
and MMU mappings; no GIC/interrupt driver is claimed. UART0 is at `0x10c02000`
with four-byte register spacing; firmware retains the baud divisor/clock.
UART0 routing to the delivered board's console connector must be confirmed;
this register address alone does not establish the board routing.
The SD controller is at `0x10808000`, with SDHCI SRS registers at `+0x200`.
The SD driver now resets the host, requests bus power off/on, sends CMD0/CMD8,
loops CMD55/ACMD41 with a deadline, obtains CID/RCA/CSD and selects the card.
It accepts only block-addressed SDHC/SDXC with CSD v2. Board wiring must confirm
whether host bus-power control actually switches VDD; the protocol model cannot
establish a physical power cycle. One-bit operation stays at <=400 kHz; no PHY
tuning, high-speed or voltage switching is attempted.

PIO needs no DMA arena or cache-coherency claim. All SD writes are refused.
Read errors attempt bounded command/data reset and permanently retire that
Device. There is no automatic replay into an existing filesystem after possible
removal. A fresh cold Open and FAT mount is required; the board wrapper requires
a reboot before another attempt. Reinsertion, eMMC, SDSC and SDUC are unsupported.

If USB is admitted, firmware must also configure the 60 MHz ULPI PHY and the
board's VBUS supply/Type-C host role with DMA off and all channels released.
See the [DWC2 contract](../dwc2/README.md) for its supported subset and limits.
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

SD/UART register definitions were checked against Altera's MIT-0 bare-metal driver
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
on disposable synthetic FAT32 media and verify its hash. Boot `keyboard` next
to validate ULPI/VBUS, enumeration and actual key reports independently of display.
Writable SD persistence, MAC/PHY and FPGA display/activation follow separately.
Retain QEMU as the integration control; cross-builds and register models establish
no physical Nano acceptance.
