# DE25 allocated-buffer scanout validation — 2026-10-28

Producer `6da029aa49eeef34698fde5336761580000bd0af` completes the disabled-by-default
DE25 allocated-buffer scanout diagnostic with Quartus Prime Pro 26.3.0 Build
114: **zero compiler errors, 308 warnings**. The new HIGH CDC-50011 finding is
fixed without a waiver. Overall design closure remains **Fail** on five
unwaived rule classes, including external IO delays and reset/debug findings.
This is host-only diagnostic evidence; production DMA/runtime/boot binding and
physical board acceptance remain closed.

The retained clean checkout is `out/work/de25-scanout-registered-reset`, with
private output `out/de25-scanout-registered-reset`. Its final artifact is
`combined/hps/project/output_files/golden_top.sof`, 1,761,978 bytes, SHA256:

`e8c8fc9363ec33be725a5a230285d573096efbf9d1b93d27771f8d9043dae7fe`

[timing-summary.json](timing-summary.json) retains all final STA rows, clock
and coverage tables, failing rule details, topology, fitted RAM/pins,
artifact/report/source hashes and failed candidates. It binds 31 captured Git
blobs to the producer and checks the final copied RTL/SDC, original HPS/EMIF
parameters and vendor boot software. All comparisons pass. Every compiler
report and generated handoff/SDC manifest hash matches its build receipt,
including after the separate read-only path audit.

| Final measure | Result |
| --- | --- |
| Synthesis / Fitter errors | 0 / 0 |
| Full compilation warnings | 308 |
| STA slack rows | 74, all nonnegative |
| Setup / hold minima | 3.280 / 0.000 ns |
| Recovery / removal minima | 7.132 / 0.066 ns |
| Minimum pulse-width slack | 0.220 ns |
| FIFO max-skew / net-delay minima | 7.610 / 7.583 ns |
| Actual clocks / explicitly constrained clock-status rows | 41 / 41 |
| Illegal / unconstrained internal clocks | 0 / 0 |
| Unconstrained input / output ports, setup and hold | 2 / 30 |
| Paths from / to unconstrained IO pairs | 9 / 3,745 |
| ALMs / registers | 13,311 / 25,587 |
| Block RAM bits / RAM blocks | 2,126,900 / 142 |
| HPS / PLLs / DSP blocks | 1 / 3 / 0 |

The 40 setup, hold, recovery, removal and pulse-width rows explicitly report
TNS zero. Another 32 generated FIFO max-skew rows and two net-delay rows
report slack only; their absent TNS fields are retained as null rather than
invented zeroes. All 35 HPS baseline clock names/types/periods/frequencies from
producer `8f54e4f89e5b12ae18df70c542bc5846d0452ce6` are preserved. Six display
clocks are added, including 74.25 MHz pixels and the exact 100,040 ns
ADV7513 configuration clock. This is actual final coverage, not inherited
acceptance from an earlier build.

All five remaining failing rule classes have zero waivers:

| Rule | Severity | Violations | Concrete scope |
| --- | --- | ---: | --- |
| TMC-20012 | High | 30 | 29 HDMI RGB/control/I2C outputs plus retained `LED[7]` finding |
| TMC-20011 | High | 2 | `HDMI_I2C_SDA` and `HDMI_TX_INT` input delays |
| RDC-50003 | Medium | 2 groups | 18 reset heads in 100 MHz and five in the pixel domain |
| LNT-30023 | Medium | 1 | Legacy JTAG packet-data lane steering into two opposite SCLR banks |
| RES-50101 | Low | 3 | Two retained top-reset paths and the first-lock register to existing same-clock reset controllers |

The new CDC-50011 count is **zero**, unwaived. The rejected preceding SOF had
an AND of `top_reset_n` and `first_lock_seen` before the pixel-domain reset
synchronizer. The final common source is the direct Q of the preserved
first-lock register, which already clears asynchronously with top reset.
Initial qualified release and later PLL-loss containment remain intact, with
no live FIFO/accounting reset on STOP, CLEAR or post-startup pixel-lock loss.
The expanded RDC and RES counts are recorded explicitly; they are not declared
identical to the HPS baseline. Multiple vendor FIFO/reset chains and legacy
debug paths still need their physical/protocol qualification.

The LNT driver remains
`soc_inst|mm_interconnect_0|limiter_pipeline|gen_inst[0].core|data1[40]`, with
128 inverted and 128 non-inverted mux-pipeline SCLR sinks. The prior
[source trace](../2026-10-28-de25-ddr-probe/timing-review.md) identifies that
legacy JTAG address bit as payload-lane steering. That context neither waives
the rule nor qualifies JTAG writes or global reset behavior.

All 36 selected display locations and IO standards match the fitted pin file.
The 30 unconstrained STA outputs are the 29 HDMI data/control/I2C outputs plus
`HDMI_TX_CLK`; Design Assistant also retains the separately classified human
indicator `LED[7]`. No LED-style exception is applied to HDMI. DJ24 still fits
as 1.1 V in bank 2A_T while the pinned manual lists 3.3 V. The missing board
flight-time/receiver budget and this vendor discrepancy prevent external HDMI
timing and electrical acceptance. No output delays were fabricated.

Five new fitted simple-dual-port M20K memories implement the two command and
two response FIFOs plus the reader. The reader is 256 × 96, 24,576 implemented
bits in three M20Ks, with registered inputs and unregistered outputs. The
original 128-bit BGRX reader loses unused alpha bytes during synthesis. The
control FIFOs consume three M20Ks and memory FIFOs five; all five new memories
use zero MLABs. Full logical and implemented widths/modes remain in the JSON.

The compiled SDC confirms four qualifier clear pins, exactly one asynchronous
fault source and exactly one stage-0 D target. A completed-database audit
enumerates all eight dedicated synchronizer registers and both preserved
first-lock/fault source registers. It finds one setup and one hold stage-D
path for each chain in one slow model:

| Chain | Setup / hold slack (ns) |
| --- | --- |
| HPS top reset | 9.569 / 0.111 |
| HDMI configuration reset | 19.595 / 0.099 |
| Pixel-lock qualifier in 100 MHz | 9.568 / 0.111 |
| Sticky-fault CDC in pixel clock | 12.995 / 0.109 |

This audit returns zero errors/warnings and leaves original compiler report
hashes unchanged. The cuts do not remove stage-D timing. It is separate from
the final multi-corner summaries and is not physical reset/MTBF acceptance.
Metastability reports find 84 chains, model 54 and exclude 30; shortest length
is two, with zero timing calculation failures. The reported `1e+09` model
years cover only that modeled subset, not the whole board.

The [transport record](transport/README.md) covers seven passing cases in the
actual generated control/memory clock crossings, endpoint/video/reader and
128-to-256-bit fabric. Its five-frame case checks 1,152,242 narrow AXI beats
and 72,021 bursts, including both slots, data ordering, response errors,
snapshots and drain with a stopped pixel clock. All 175 selected RTL/header
files and 16 QIPs match the final producer after compilation. The SOPCINFO
difference is only its timestamp comment; canonical XML and scanout handoff
data agree. Storage uses explicitly guarded behavioral models for four vendor
M20K primitive configurations, with a direct noncollision storage self-test.
RAM collision/primitive behavior, physical CDC, HPS/MPFE/SMMU/firewall/DDR and
real clock/reset behavior remain unmodeled or unqualified.

An independent board-shell bench binds its RTL/reset/model/bench/runner bytes
to `6da`, exercises startup and brief unlock/relock without resetting live
crossings, and preserves sticky blanking through relock. It uses modeled
PLL/I2C boundaries and does not measure physical pixel-clock progress. The
native endpoint bench passes 12 groups, including four full pixel-checked
frames, and the exact last-visible-pixel submission regression passes. Eight
preparation/mutation guards and existing default reader/video regressions
also pass. Software/affected-core checks are recorded separately by FES.

The failed attempts are retained and are not artifact acceptance:

| Producer | Result and correction |
| --- | --- |
| `cd9e7cf8960f69463424b4918e948aadc4f763d7` | IP generation: four reset-source errors. Added a real top reset bridge while preserving the common external signal. |
| `a54abd623b08fe2ed77f8c9847f036f421ea1c83` | IP generated; initial verifier queried the wrong SOPCINFO parameter level. Independent committed verifier `a0547320b160accf3d6e336e5b8ccf6693db92b3` checked actual referenced IP and preserved all 680 project input hashes. The single continuation then failed on unsupported `get_pins -regexp`; no accepted SOF. |
| `71203a8cf8780466041729a29784729141d21d80` | Completed SOF `72134451ca9a364c6e45269791763af0351e11604ccb837089843eab05acdaf0`, all 74 slack rows nonnegative, but rejected on the real new HIGH CDC-50011 reset AND. Replaced it with registered Q; no waiver. |

Exact synthesized node/filter queries, failed logs, the 680-file unchanged
manifest and the distinct a54 producer/a054 verifier continuation are bound in
the JSON. The final producer runs the enhanced verifier normally and needs no
continuation. The generator's unused automatic address-width metadata warning
is retained; configured/manual width, actual generated 32-bit path and
high-address simulation evidence are recorded instead of ignoring it.

No kit, hardware, harness, SD/QSPI image or runtime deployment was touched.
This lane issues no reads until explicit valid submission and admits no
production DMA profile. SID/firewall/IOMMU backing, firmware/DT/boot pairing,
global reset fencing and hardware acceptance remain gates. Warm-reset ACK is
inactive; local scanout drain cannot acknowledge retained JTAG and HPS-origin
traffic. The shared reader/video fixes also affect rebuilt existing consumers:
their package/HIL acceptance remains a separate merge gate, even though this
new DE25 lane is undeployed.
