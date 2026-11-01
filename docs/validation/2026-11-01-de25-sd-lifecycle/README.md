# Native SD PIO quiesce and same-card resume

The actual Go SD driver now fences queued reads, stops a batch after its current
sector, observes controller/card readiness and idle, masks interrupts and stops
the card clock with readback. A quiesced Device refuses reads and flush without
MMIO. Repeated quiesce supplies no fresh success. See [evidence](evidence.json).

Resume reinitializes the controller/card using the original clock declaration
and checks CID[127:8] plus capacity before allowing the retained filesystem to
read again. A new RCA is accepted; reserved R2 bits do not change identity.
Changed identity/geometry, failed reinitialization or uncertain quiesce retires
the Device. Subsequent operations refuse without MMIO.

Race-enabled tests passed for SDHCI and FAT. They execute the real driver with
modeled controller/card events and a temporary FAT32 image. Coverage includes
same-card filesystem reuse, unchanged media bytes, concurrent/queued reads and
partial-batch boundaries, DMA/IRQ/error/clock/idle failures, card replacement,
capacity changes and reset/init failures. The [race output](go-race.log) and
[build summary](build-summary.json) identify the tested sources and commands.

The Nano storage application also cross-built for ARM64 using an explicitly
unqualified synthetic profile. All 1,319 compiler-selected source files matched
their receipt after the build. This is compilation evidence: the profile is
not a trusted firmware grant and was not deployed.

No board operation or shared firmware ABI change occurred. Native SD remains
disabled in the [accepted serial profile](../2026-11-01-de25-native-go-boot/README.md).
Driver quiescence does not establish all-master reset containment. The next
integration step is qualified firmware SD grant/stop binding and Cadence PHY
preparation, followed by a bounded native FAT32 read on the spare card. Core
loading, active FPGA programming and physical launch/return remain incomplete.
