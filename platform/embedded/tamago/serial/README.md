# Embedded Serial Console Driver (`serial/`)

`platform/embedded/tamago/serial` provides low-level UART console RX/TX
buffering, GIC interrupt dispatch, and line-status error telemetry for
bare-metal AArch64 targets:

- **QEMU `virt` (`!de25nano`):** ARM PL011 UART0 at `0x09000000` (SPI 1 / GIC
  INTID 33). Decodes `UARTDR` error bits (overrun, parity, framing, break).
- **DE25-Nano (`de25nano`):** Synopsys DesignWare APB UART (16550-compatible) at
  the validated firmware handoff console address (`IRQ` INTID 140/141). Decodes
  `LSR` error bits `[4:1]` and handles busy-detect interrupt clear (`USR`).

## Telemetry and Ring Buffers

- **`Ring`**: Zero-allocation, lock-free 256-byte SPSC ring buffer (`Push`,
  `Pop`, `Peek`, `Len`, `Cap`, `Dropped`, `Reset`).
- **`ConsoleStats()` / `Stats` / `ErrorCounters`**: Tracks `IRQCount`, `RXBytes`,
  `RXDropped`, `TXBytes`, `TXDropped`, and receive line-status faults (`Overrun`,
  `Parity`, `Framing`, `Break`). `ErrOverrun` is counted while preserving valid
  FIFO characters; parity/framing/break-corrupted bytes are discarded so valid
  bytes behind them remain intact.
- **`Controller`**: Host-testable RX/TX controller over the `serial.UART`
  interface with fast-path direct TX FIFO writes and bounded TX ring draining.

## Host tests

```sh
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./serial
```
