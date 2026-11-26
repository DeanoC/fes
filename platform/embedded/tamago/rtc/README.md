# `rtc` — ARM PrimeCell PL031 RTC and Wall-Clock Epoch

`rtc` provides an injectable ARM PrimeCell PL031 real-time clock driver and a
monotonic-anchored UTC wall clock (`Clock`).

## Design

- **`runtime/goos.Nanotime` stays monotonic.** Go timers, deadlines, and WFI
  idle scheduling rely on `CNTPCT_EL0` never jumping backward. Wall-clock UTC
  is kept separate via `board.WallTime()` and `rtc.Clock`.
- **PL031 probing (`OpenPL031` / `OpenMMIO`):** verifies the PrimeCell ID
  (`0xb105f00d`), part number (`0x031`), and ARM designer ID (`0x41`), ensures
  `RTCCR` is enabled, and exposes whole-second reads (`Seconds`, `Now`),
  loads (`Set`), match alarms (`SetAlarm`, `ClearAlarm`, `AlarmPending`), and
  level-sensitive GICv3 alarm SPI dispatch (`EnableIRQ`, `DisableIRQ`,
  `IRQCount`).
- **Epoch-anchored `Clock`:** reads hardware RTC once and advances UTC using
  the monotonic nanosecond counter (`NewClock`, `Now`, `Sync`), avoiding
  repeated MMIO reads while providing sub-second wall-clock progression.
- **Board policy:** QEMU `virt` wires PL031 at `0x09010000` (SPI 2 / INTID 34).
  DE25-Nano has no on-chip PL031 RTC (`board.RTC()` returns `(nil, rtc.ErrNotFound)`)
  and exposes an initially unsynced monotonic-anchored `rtc.Clock`
  (`NewUnsyncedClock`) populated once network SNTP (`board.SyncWallTime`)
  establishes a UTC epoch.
