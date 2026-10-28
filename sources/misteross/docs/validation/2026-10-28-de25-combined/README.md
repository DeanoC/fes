# DE25 combined HPS/local-display validation — 2026-10-28

Two combined GHRD/display diagnostic SOFs compile from producer
`ec3ab70b608eb53a0b845d4462941f8a832af451` with Quartus Prime Pro
26.3.0 Build 114. These are **host-only diagnostics with failed overall design
closure**, not accepted DE25 boot images or interactive MENU firmware.
All reported constrained STA rows are nonnegative with zero TNS; external IO
timing and retained HPS/debug reset findings remain open. No timing constraints
were relaxed, output delays fabricated or Design Assistant violations waived.

The producer follows merge `8bdefb4ab1fe0becbc1da758af16e6c2f5f0ca69`,
which combines the HPS/probe foundation and standalone display work. The
retained clean checkout is `out/work/de25-combined-ec3ab70b60`; actual compiler
inputs and outputs are in separate `out/de25-combined-menu-ec3ab70b60/` and
`out/de25-combined-idle-ec3ab70b60/` directories. Menu was built first, followed
by idle, with two CPU workers per compile. The FPGA agent's idle launcher
session was 64358 and exited zero; the parent launched menu in session 77544.
Exact tool/idle launcher commands, log hashes, output locations, all 76 STA
summary rows and report/source/handoff bindings are in
[timing-summary.json](timing-summary.json).

| Final result | Menu-pattern | Idle |
| --- | ---: | ---: |
| Full compilation errors / warnings | 0 / 304 | 0 / 303 |
| Constrained STA summary rows | 38 | 38 |
| Setup minimum (ns) | 3.129 | 3.319 |
| Hold minimum (ns) | 0.000 | 0.000 |
| Recovery minimum (ns) | 7.057 | 7.554 |
| Removal minimum (ns) | 0.031 | 0.069 |
| Minimum pulse-width slack (ns) | 0.220 | 0.220 |
| ALMs / registers | 11,369 / 22,291 | 11,152 / 21,832 |
| RAM blocks / logical RAM bits | 134 / 2,123,264 | 131 / 2,098,688 |
| HPS / PLLs / DSP blocks | 1 / 3 / 0 | 1 / 3 / 0 |
| Actual clock / constrained status rows | 41 / 41 | 41 / 41 |
| Illegal / unconstrained internal clocks | 0 / 0 | 0 / 0 |
| Unconstrained inputs / outputs (setup and hold) | 2 / 30 | 2 / 30 |
| Paths from / to unconstrained IO pairs | 9 / 4,651 | 9 / 921 |

The final artifact in each output is `hps/project/output_files/golden_top.sof`:

| Mode | SHA256 |
| --- | --- |
| Menu-pattern | `eab5618183dacf4e154de7b92840344efc38d3527261762edd70f3790e8b3940` |
| Idle | `f479941844f2d9eb5db36a42dbf0a96d07e2f8c9e565b2fba818d579ed26db86` |

Both actual final clock tables preserve all 35 HPS baseline clock names,
types, periods and frequencies from producer
`8f54e4f89e5b12ae18df70c542bc5846d0452ce6`. Six display clocks are added:
the 100,040 ns ADV7513 configuration clock; two 100 ns internal PLL counter
clocks; the 13.468 ns (74.25 MHz) pixel output; and the 20 ns reference/base
and generated reference clocks. Every clock has an explicit `Constrained`
status in each final report. This is measured final coverage, not inherited
acceptance from either earlier standalone build. The 100 MHz GHRD system PLL
still uses `CLOCK2_50`, and the new pixel PLL uses `CLOCK0_50`.

The following Design Assistant failures remain in **both** final signoff
reports, with zero waivers across all 85 reported rule classes:

| Rule | Severity | Violations | Scope |
| --- | --- | ---: | --- |
| TMC-20012 | High | 30 | 29 HDMI RGB/control/I2C outputs and the retained `LED[7]` finding |
| TMC-20011 | High | 2 | `HDMI_I2C_SDA`, `HDMI_TX_INT` input delays |
| LNT-30023 | Medium | 1 | Legacy JTAG packet-data lane steering mapped onto opposite SCLR banks |
| RDC-50003 | Medium | 1 group | 12 retained reset synchronizer heads in the 100 MHz HPS/debug domain |
| RES-50101 | Low | 2 | Top reset chain to existing same-clock subsystem reset controllers |

The LNT, RDC and RES detail tables are exactly identical to the HPS/probe
baseline. The [earlier source trace](../2026-10-28-de25-ddr-probe/timing-review.md)
identifies LNT's `limiter_pipeline...data1[40]` as the legacy JTAG byte-address
lane selection for two 128-bit payload banks. That datapath context does not
waive the finding or qualify JTAG writes/reset safety. The RDC/RES findings
also retain their prior protocol/reset review boundary.

STA's 30 unconstrained output ports are the 29 HDMI outputs plus
`HDMI_TX_CLK`; Design Assistant additionally reports the already classified
human indicator `LED[7]`, which has a separate narrow port exception. No
LED-style timing exception is applied to the HDMI receiver. The clock pin
`HDMI_TX_CLK` DJ24 still fits as 1.1 V in bank 2A_T, while the pinned manual
lists 3.3 V. All 36 selected display pin locations and IO standards match the
actual fitted pin files, but the vendor documentation discrepancy and missing
external ADV7513/board flight-time budget prevent electrical or HDMI timing
acceptance.

The final menu FIFO is a single fitted simple-dual-port 256 × 96 memory:
24,576 bits in three M20K blocks and zero MLABs, with input registers and
unregistered outputs. The original reader declares 128-bit words; unused
pattern-mode alpha bits are optimized away. Idle adds no display RAM. Both
designs preserve the two registers in each of the three local reset chains.
The compiled SDC messages confirm exactly two top-reset and four display-reset
asynchronous clear pins. Those new exceptions cut clear pins only; they do not
cut stage-D or release fanout. This record includes full STA clock coverage and
preserved-register evidence, not an additional completed-database stage-D path
audit or physical reset acceptance.

Metastability reports find 44 chains, model 24, and exclude 20 in each mode;
the shortest chain has two registers, with zero timing calculation failures.
The reported `1e+09` model years cover only the modeled subset. They are not
whole-board MTBF, physical CDC/reset signoff or warm-reset ownership proof.

All 23 captured functional source blobs match the selected Git revision after
compilation. Copied HPS sources, derived video/core/reset RTL and local SDC
match their receipts and the exact RTL used in the passing idle/menu video
benches. Original HPS/EMIF module parameters and vendor boot software hashes
remain unchanged after both full compiles. All compiled report and generated
handoff/SDC manifest hashes match their build receipts. The 114 actual fabric
simulation input hashes from the earlier ten-case probe/interconnect bench
also match both final projects exactly; that reuses the existing narrow-AXI,
lane-order, error-propagation and drain checks without claiming a new hardware
test. Seven preparation/guard tests pass, and both real-video mode benches
pass with modeled PLL/configuration/I2C boundaries.

Generated boot handoff identity is retained separately: idle matches the
earlier 1,629-byte handoff exactly; menu differs only at byte offsets
1624–1627. This raw comparison does not interpret the footer or qualify a boot
pair. The generated top SOPCINFO and boot handoff hashes for both modes are
in the JSON bindings.

The HPS warm-reset acknowledgement is explicitly held at inactive `1`; its
active-low request is unconsumed. The original JTAG F2SDRAM/F2H and HPS-origin
bridge paths remain present. Probe-local drain cannot acknowledge all HPS soft
logic, and hardware may proceed after a reset timeout. No DMA grant, SMMU
mapping, reset-lifetime, allocator or runtime admission follows from these
builds. There is no DDR scanout, GP endpoint, framebuffer upload/control,
interactive MENU profile, stock-BSP/factory-JIC pairing or board execution.
The next integration step needs those lifetime and boot gates plus real
framebuffer scanout/control; no hardware or harness was touched here.
