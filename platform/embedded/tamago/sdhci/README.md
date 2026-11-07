# Read-only SD PIO lifecycle

This driver discovers SDHC/SDXC cards, reads sectors through PIO and supplies
the shared FAT32 implementation. All writes reject. The Nano adapter requires
exclusive controller ownership and prepared SoC clocks, reset, pinmux and
ComboPHY before `Open` accesses registers. The
[serial Go hardware diagnostic](../../../../docs/validation/2026-11-01-de25-native-go-boot/README.md)
keeps native SD disabled; this driver has model/race and ARM64 cross-build
evidence for its original scope. Later [native launch/return tests](../../../../docs/validation/2026-11-07-de25-fat-launch-trial/README.md)
exercise the real conservative PIO/SD/FAT path through repeated package loads,
containment and resume. Those results do not qualify the optional faster mode
below or general concurrent FPGA/SD access.

`Config.TransferClockHz=25000000` is a separate, default-off driver prototype.
Discovery and PHY preparation remain at the original <=400 kHz, one-bit,
3.3V setting. After selecting the card, the prototype requires its default-speed
CSD declaration, stops card/internal clocks, checks the new divider and stable
state, restarts the card clock and verifies readiness. Reads reject a changed
mode. Quiescence restores the exact original disabled-clock setting required by
the existing secure stop callback. Resume rediscovers and checks the original
card before selecting the faster rate again. Every transition is bounded and
failure retires the device; it never retries at the slower rate.

This option currently has model/race evidence only and is not selected by the
Nano board adapter or build recipe. It grants no new firmware/PHY, DMA, voltage
or bus-width capability. The [Altera clock description](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/clocks?contentId=vITZ5tUjusRdH5RE7r9jwg)
documents the qualified-input divider relationship; the card's CSD field follows
the pinned Terasic U-Boot `drivers/mmc/mmc.c` default-speed interpretation.

`Identity` returns a copy of CID[127:8] and discovered sector count. SDHCI's
R2 response omits the CID CRC/end byte and reserves its high register byte;
that reserved byte is excluded. Identity and geometry remain immutable for the
Device. This identity tracks continuity across reset.

`Quiesce` fences new reads before waiting for current I/O. A batch finishes its
current sector and returns `ErrQuiesced` before issuing another; already
completed sectors may have been copied, so the caller must honor the error.
The operation checks DMA/clock/error state, masks IRQ signals, queries card
readiness, masks status, independently observes idle around card-clock stop
and checks readback. Queued reads and `Flush` reject without MMIO while
quiesced. A repeated call returns `ErrQuiesced`, supplying no fresh stop proof.

The platform then performs its own trusted master-stop/reset/bridge transition.
Driver quiescence covers this PIO controller; it is insufficient to claim all
HPS masters contained or authorize FPGA programming. Firmware ownership,
functional/OCP reset readback and other producers remain separate obligations.

Before `Resume`, the caller retains or reestablishes exclusive controller
ownership and valid SoC clock/ComboPHY/reset preparation. Resume reruns card
discovery using the original input clock and compares CID and capacity before
reopening access. A newly assigned RCA is adopted. The retained FAT volume is
reusable only within unchanged exclusive-media ownership; media/session changes
require a fresh mount. This API grants no firmware/reset authority.

An uncertain stop, failed reinitialization, different CID or changed capacity
permanently retires the Device. Later reads, flush, quiesce and resume return
`ErrRetired` without MMIO. The caller retains the unresolved hardware lease;
new construction needs fresh qualified ownership and filesystem mounting.

The actual driver runs against a modeled card/controller, including a real
temporary FAT32 image. Tests cover sector-boundary interruption and queued
reads, DMA/IRQ/clock/drain failures, media changes, reset/reinitialization
errors, RCA changes and preserved read-only media. The
[pinned Altera register/low-level source](https://github.com/altera-fpga/baremetal-drivers/blob/66bdcc347564c5698a8997169bce83a31647d618/drivers/sdmmc/socfpga_sdmmc_ll.c)
and [Terasic ComboPHY source](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/drivers/phy/cadence/phy-cadence-combophy.c)
describe separate controller and PHY setup. The [SD lease session](../sdcontrol/README.md) now connects the actual driver
lifecycle and the `PrepareClock` boundary to an injected firmware service.
`PrepareClock` runs after host reset/internal-clock stability and before card
clocks or commands, and reruns on resume. Native Nano platform callbacks,
profile selection and physical PHY/read qualification remain pending.
