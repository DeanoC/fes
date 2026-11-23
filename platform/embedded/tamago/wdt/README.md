# `wdt` — Synopsys DesignWare APB Watchdog Driver

`wdt` drives the Synopsys DesignWare APB Watchdog Timer (`snps,dw-wdt`) present
on Altera/Intel Agilex 5 HPS (`L4WD0` at `0x10d00200` / SPI 117 / GIC INTID 149
and `L4WD1` at `0x10d00300` / SPI 118 / GIC INTID 150).

## Safety Policy

- **Strictly opt-in.** `Open` / `OpenMMIO` probes `WDT_COMP_TYPE` (`0x44570120`)
  and reads `WDT_COMP_VERSION` without setting `WDT_CR.WDT_EN`. Existing boot
  harnesses never auto-arm the watchdog.
- **Sticky enable.** Once `Start` sets `WDT_CR.WDT_EN`, DesignWare APB watchdog
  hardware remains armed until a peripheral reset from `RSTMGR`.
- **Two response modes:**
  - `ModeReset` (`RMOD=0`): asserts system reset on the first counter expiry.
  - `ModePreTimeoutIRQ` (`RMOD=1`): raises a level-sensitive interrupt on the
    first expiry (`EnableIRQ` clears `WDT_EOI` and invokes a user callback) and
    asserts system reset on the second expiry if the interrupt is not cleared
    or the watchdog is not kicked.
