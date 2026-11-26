# Cadence SDHCI driver (`sdhci/`)

`sdhci.Device` discovers SDHC/SDXC cards and performs bounded sector reads and
(opt-in) writes for DE25-Nano (`0x10808000`, SRS `+0x200`).

- **Called by:** [sdcontrol](../sdcontrol/README.md) (`sdcontrol.Session`),
  which wraps `sdhci.Device` with the EL3 SD lease and `PrepareClock` PHY hook.
- **Delegates to:** injected `Read32`/`Write32` MMIO registers, optional
  [gic](../gic/README.md) SPI (`EnableIRQ`), and optional
  [dmaops](../dmaops/README.md) pool (when `UseADMA2` is selected).

## Transport modes

Default configuration uses conservative **1-bit, single-sector, polling PIO**
(read-only unless `Config.AllowWrites` is explicitly enabled):

| Option | Behavior | Notes |
| --- | --- | --- |
| `Config.AllowWrites` | Single-sector `CMD24` + `CMD13` programming-ready wait (1 s bound) | Enforces card CSD permanent/temporary WP and R1 errors; `NoSocketWriteProtect` models microSD sockets without a mechanical WP pin (grants no write permission by itself). |
| `Config.TransferClockHz` (`25000000`) | Switches from `<=400 kHz` ID clock to 25 MHz after verifying card default-speed CSD | Restores disabled ID clock on `Quiesce` and re-applies on `Resume`; failure retires the device with no silent fallback. |
| `Config.TransferClockHz` (`50000000`) | SD High-Speed via `ACMD51` SCR version check (`SD_SPEC >= 1`), `CMD6` mode-0 check and mode-1 Switch Function (`0x00fffff1` / `0x80fffff1`, advertised current must fit `Config.HighSpeedMaxCurrentMA`, default 200 mA), Host Control 1 High-Speed bit 2, and 50 MHz divider (`HighSpeed()`, `TransferClockHz()`) | Restores the identification divider on `Quiesce` and re-establishes High-Speed on `Resume`. |
| `Config.BusWidth4Bit` | Queries SCR (`CMD55`+`ACMD51`), switches via `ACMD6(2)`, sets Host Control 1 bit 1 | Enforces readback across clock changes and `Resume`. |
| `Config.MultiBlock` | `CMD18`/`CMD25` with Block Count and Auto-CMD12 (up to 128 sectors / 64 KiB) | Works in both PIO and ADMA2 modes. |
| `Config.UseCMD23` | Issues pre-defined `CMD23` (`SET_BLOCK_COUNT`) before `CMD18`/`CMD25` without Auto-CMD12 | Requires `Config.MultiBlock` and card `SCR[33]` (`SupportsCMD23`); verified across `Resume`. |
| `Config.UseADMA2` + `DMAPool` | 32-bit/64-bit ADMA2 descriptors + 64 KiB pinned staging buffer from `dmaops.Pool` | Any ADMA fault permanently poisons both buffers and retires the device; physical Nano use awaits EL3 BL31 SMMU stream grant. |
| `(*Device).EnableIRQ(g, irqID)` | Routes status waits onto a GICv3 SPI (e.g. `de25nano.IRQSDMMC` = 128) | Level-triggered handler masks `signalEnable = 0` immediately so idle/quiesce invariants hold. |

## Quiesce, resume, and fail-closed retirement

- **`Identity()` / `CardInfo()` / `Stats()`:** `Identity()` returns immutable `CID[127:8]`
  (excluding R2 CRC/reserved high byte) and discovered sector count for continuity
  checks across reset. `CardInfo()` decodes CID, CSD, and (when queried) SCR
  fields including manufacturer, product name, capacity, write-protect flags, SD
  spec version, and `CMD23` support (`CardInfo.SupportsCMD23` / `CMD23Active()`).
  `Stats()` snapshots cumulative read/write/sector/PIO/ADMA2/CMD23/error/IRQ
  telemetry alongside active bus width, transfer clock, and lifecycle state.
- **`Quiesce()`:** Fences new reads/writes, finishes the current sector
  (returning `ErrQuiesced` on remaining batch work), checks DMA/clock/error
  state, masks IRQs, confirms card-ready and clock-stop readback. `Flush()`
  requires card-ready (not merely an empty FIFO).
- **`Resume()`:** Reruns `PrepareClock` and card discovery at identification
  clock, verifies matching CID and capacity (adopting any new RCA), and
  reapplies the selected speed/bus width.
- **Retirement:** Any uncertain transfer, failed stop, failed reinit, or card
  CID/capacity mismatch permanently retires the `Device` (`ErrRetired` with no
  further MMIO); the caller retains the unresolved hardware lease until reboot.

## References and tests

- Register and PHY sequences were verified against Altera's bare-metal SDMMC
  driver (`66bdcc347564c5698a8997169bce83a31647d618`), Terasic U-Boot
  (`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`, `mmc.c`, `sdhci.c`,
  `phy-cadence-combophy.c`), and Linux v6.12 `sdhci.c`. No vendor C code is
  linked into Go.
- Run host tests:
  ```sh
  GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./sdhci
  ```
