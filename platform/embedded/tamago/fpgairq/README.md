# `fpgairq` — FPGA-to-HPS IRQ Doorbell Dispatcher

`fpgairq` manages exclusive claims on Agilex 5 FPGA-to-HPS GIC-600 SPI lines
(`0..63`, GIC INTIDs `48..111`) and converts hardware interrupts into coalesced
Go wakeup channels compatible with TamaGo's WFI idle governor.

## Highlights

- **Exclusive line ownership (`Dispatcher`):** prevents duplicate claims on the
  same FPGA-to-HPS line (`ErrLineInUse`) and tears down all active registrations
  on `Close`.
- **Safe trigger policy:** supports both `Edge` and `Level` SPI sensing.
  `Level` lines require an explicit `Config.Ack` callback to clear the FPGA
  interrupt source inside the handler so unacknowledged level lines cannot storm.
- **WFI-friendly wakeups (`Doorbell`):**
  - `Ready()` — capacity-1 coalesced notification channel.
  - `Wait(ctx)` / `WaitTimeout(d)` — parks the goroutine so `gic.EnableIdle()`
    can enter `WFI` until the FPGA raises the line.
  - `YieldFunc(maxWait)` — returns a bounded `func()` directly pluggable into
    `scanout.ReaderLimits.Yield`, `scanout.MenuLimits.Yield`, and
    `computer.Limits.Yield`.
- **Telemetry (`Doorbell.Stats`, `Dispatcher.Stats`):** lock-free per-doorbell
  counters (`DoorbellStats`: interrupts, coalesced wakeups, consumed wakeups,
  timeouts, cancellations, trigger mode, active status) and sorted dispatcher
  snapshots.
