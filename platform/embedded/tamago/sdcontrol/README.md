# Firmware-owned SD session (`sdcontrol/`)

`sdcontrol.Session` joins [sdhci.Device](../sdhci/README.md) to the EL3
[embedded-sd lease contract](../../../../sources/mister-packages/docs/embedded-sd.md)
(`0xc2000302`), enforcing closed-bridge mutual exclusion between SD I/O and
FPGA reconfiguration.

- **Called by:** [board/de25nano](../board/README.md), `cmd/fpgaload`,
  [packagestore](../packagestore/README.md), `cmd/sdfile`, and `cmd/sdwrite`.
- **Delegates to:** [sdhci](../sdhci/README.md) (MMIO + optional GICv3 IRQ),
  [block.Cache](../block/README.md) (lease-invalidating LRU write-through cache
  wired via `EnableCache`), and EL3 `nano_sd.c` via SMC `0xc2000302`.

## Lease lifecycle

1. **`Open(cfg)`:** Queries EL3 containment, acquires a fresh SD lease while
   FPGA bridges are closed, wires `PrepareClock` to EL3 ComboPHY setup, and
   opens `sdhci.Device`.
2. **`Quiesce()`:** Fences new reads/writes, finishes the active sector, stops
   `sdhci.Device` (restoring the disabled ID clock), invalidates cached blocks,
   and invokes EL3 SD stop so firmware verifies drain and reset containment.
   `LastReceipt` shares the same boot owner/generation as bridge control.
3. **`Resume()`:** Acquires a fresh closed-bridge SD lease, reruns controller
   and card discovery, and verifies matching CID and capacity before reopening
   the retained FAT volume.
4. **`ResumeAfterReturn(owner, generation)`:** Explicitly crosses a verified
   FPGA core return—requiring a newer same-owner closed receipt and EL3
   confirmation of producer-stop + control-only bridge closure with data held
   before rediscovering the same card.
5. **Fail-closed retirement:** Any uncertain SMC/transport reply, driver error,
   firmware rejection, or card CID/capacity mismatch retires the session
   (`LastReceipt` retained with `valid=false`) with no retry, automatic stop,
   or foreign-owner adoption.

## Read-only vs writable access and transport options

- **Read-only by default:** Zero `Config.WriteCard` selects read-only access
  (package metadata can never enable writes).
- **Compiled writable card identity (`WriteCard`):** Trusted callers supply an
  expected card CID and sector count (e.g. from `--writable-storage-plan`).
  Card discovery must match both fields before writes are enabled; mismatch
  retires immediately without writing.
- **Opt-in transport options:** `sdcontrol.Config` forwards
  `TransferClockHz` (`25000000`), `BusWidth4Bit`, `MultiBlock`, and
  `UseADMA2` + `DMAPool` to `sdhci.Device` (`UseADMA2` without `DMAPool`
  rejects before firmware or MMIO access). `de25nano.OpenStorageWithOptions`
  and `OpenWritableStorageWithOptions` expose `TransferClockHz`,
  `BusWidth4Bit`, and `MultiBlock` while keeping `UseADMA2` disabled at the
  board layer pending EL3 SMMU stream authorization. `BusWidth()` and
  `ADMA2Active()` report the negotiated transport lock-free.
- **Diagnostics:** `storage --sd-scratch-inspect` inspects card geometry
  read-only; `sdwrite --sd-write-plan` (`RunScratch`) tests bounded 8-sector
  raw writes on an unpartitioned spare-card tail; `sdfile`
  ([fatcheck](../fatcheck/README.md)) exercises FAT file allocation/updates.

## Verification

Tests compile the real C EL3 SD dispatcher (`platform/embedded/firmware/`) with
the host C compiler alongside `sdcontrol`, `sdhci`, `fat`, and `fatcheck`
(pass `-count=1` so Go's test cache does not mask C source edits):

```sh
GOWORK=off go -C platform/embedded/tamago test -race -count=1 -timeout 90s \
  ./sdcontrol ./sdhci ./fat ./fatcheck
```
