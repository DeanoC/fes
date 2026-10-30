# DE25 HPS-controlled ZX81 diagnostic

This adds execution, HID keyboard and CRC-checked tape control through the
preserved Terasic GHRD lightweight HPS bridge. The actual ZX81 machine runs
on the existing 100 MHz peripheral clock with 13/200 enables; HDMI retains
its separate exact 74.25 MHz PLL. The new path adds no DMA master. The GHRD's
HPS, LPDDR4, inherited probe/debug masters and boot files remain selected.

## Current post-merge Quartus evidence

The fresh build and full standalone STA below use clean committed source
revision `a4bd6fc2ba3cb480de5a9a8fb4ae23a8025a539e`, the current PR #1232 head
after PR #1260 was squash-merged. This is the successful SOF receipt for the
current RTL and constraints. The older `compiled-5a7382c4b5` receipt remains
historical and is kept only for its source and artifact history.

The build ran on Powerboat with Quartus Prime Pro 26.3. The exact version line
was:

```text
Version 26.3.0 Build 114 10/14/2026 SC Pro Edition
```

The official Terasic rev. B resource ZIP was downloaded and matched the locked
SHA-256 `f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`.
From the repository root, the build command was:

```sh
source /data/altera_pro/26.3/qinit.sh
make -C sources/misteross/boards/de25nano/zx81-hps build \
  QUARTUS_ROOTDIR=/data/altera_pro/26.3 \
  VENDOR_ARCHIVE=/home/deano/tmp/fixjob-fes-1232-20261030-185746/sources/misteross/build/vendor/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  OUTPUT=/home/deano/tmp/fixjob-fes-1232-20261030-185746/sources/misteross/build/diagnostic/de25-zx81-hps-a4bd6fc2ba
```

Quartus full compilation succeeded with 0 errors and 332 warnings. It produced
a 1,765,692-byte `golden_top.sof` with SHA-256
`f6f3a6e2e866a112cb2a8d5de5d6300020a1c256bc2b57054ed1a77b9e460a78`. The SOF
binary is not committed; the [artifact receipt](compiled-a4bd6fc2ba/artifact-receipt.json)
binds its hash to the input manifest and build report.

The full standalone post-fit multicorner STA command was:

```sh
source /data/altera_pro/26.3/qinit.sh
cd sources/misteross/build/diagnostic/de25-zx81-hps-a4bd6fc2ba/hps/project
/data/altera_pro/26.3/quartus/bin/quartus_sta \
  --do_report_timing --multicorner=on --parallel=2 \
  --rev=golden_top golden_top
```

It loaded Quartus's completed `final` database and exited 0 with 0 errors and
1 warning. TimeQuest reported `Timing requirements were met`. Across the 21
setup, hold, recovery and removal summary rows, every reported slack was
nonnegative, every TNS was 0.000 ns, and every failing-endpoint count was zero.
The worst displayed values were setup 1.608 ns, hold 0.000 ns, recovery
7.343 ns, removal 0.009 ns, and minimum pulse width 0.220 ns. The displayed
zero hold slack is rounded to three decimals and does not establish positive
hold margin.

| Clock | Setup slack | Hold slack | Recovery slack | Removal slack |
| --- | --- | --- | --- | --- |
| `adv7513_config` | 16.879 ns (Slow fix4 0C) | 0.070 ns (Fast fix4 0C) | 18.372 ns (Slow fix4 0C) | 0.009 ns (Fast fix4 0C) |
| `altera_reserved_tck` | 12.809 ns (Slow fix4 100C) | 0.096 ns (Fast fix4 0C) | 60.118 ns (Slow fix4a 0C) | 0.157 ns (Fast fix4 0C) |
| `fes_video_inst|video_clock|iopll_0_outclk0` | 3.204 ns (Slow fix4 0C) | 0.006 ns (Fast fix4 0C) | 12.486 ns (Slow fix4 0C) | 0.239 ns (Fast fix4 0C) |
| `fes_video_inst|video_clock|iopll_0_refclk` | 18.297 ns (Slow fix4a 0C) | 0.108 ns (Fast fix4 0C) | 17.264 ns (Slow fix4 0C) | 0.326 ns (Fast fix4 0C) |
| `pll_inst|iopll_0_outclk0` | 1.608 ns (Slow fix4a 0C) | 0.000 ns (Slow fix4a 0C) | 7.343 ns (Slow fix4 0C) | 0.164 ns (Fast fix4 0C) |
| `soc_inst|subsys_hps|emif_hps|emif_io96b_hps|emif_0_lpddr4|emif_0_lpddr4_0_phy_clk_0` | — | 0.376 ns (Fast fix4 0C) | — | — |

The table groups all reported rows by clock; `—` means Quartus emitted no row
for that clock/check pair. [per-clock-timing.csv](compiled-a4bd6fc2ba/per-clock-timing.csv)
and [final-timing-audit.json](compiled-a4bd6fc2ba/final-timing-audit.json) retain
the exact corner, TNS and failing-endpoint fields. The [full STA report](compiled-a4bd6fc2ba/reports/standalone-sta/golden_top.sta.rpt.gz)
contains detailed worst paths. No constrained setup, hold, recovery or removal
failing paths were reported.

Overall **Design Closure Summary: Fail** remains visible. The same report says
Timing Closure is Pass, Design Assistant Summary is High Severity Violations,
and Unconstrained Paths is Fail. It reports 2 unconstrained inputs (9 input
path pairs) and 30 unconstrained outputs (2,543 output path pairs) for setup
and hold. The Design Assistant reported seven unwaived rule classes:

| Rule | Severity | Violations |
| --- | --- | ---: |
| TMC-20012 — Missing Output Delay Constraint | High | 30 |
| TMC-20011 — Missing Input Delay Constraint | High | 2 |
| CDC-50011 — Combinational Logic Before Synchronizer Chain | High | 1 |
| LNT-30023 — Reset Nets with Polarity Conflict | Medium | 2 |
| RDC-50003 — Multiple Asynchronous Reset Synchronizers in the Same Clock Domain | Medium | 2 |
| RES-50101 — Intra-Clock False Path Reset Synchronizer | Low | 3 |
| CDC-50101 — Intra-Clock False Path Synchronizer | Low | 1 |

There are zero waivers. The complete findings remain in the [Design Assistant
report](compiled-a4bd6fc2ba/reports/standalone-sta/golden_top.tq.drc.signoff.rpt.gz)
and follow-up tracking remains open in [issue #1284](https://github.com/DeanoC/fes/issues/1284).

An initial standalone invocation included `--post_fpp` and Quartus rejected
the incomplete periphery-placement database with Error 16546. That attempt is
retained separately. The successful command above opened the completed `final`
database; see the [successful STA log](compiled-a4bd6fc2ba/reports/standalone-sta.log.gz)
and the [rejected attempt log](compiled-a4bd6fc2ba/reports/sta-post-fpp-rejected.log.gz).

The source-bound generated-design audit also passed. The compiled SDC contains
the current HDMI visibility and readiness synchronizer selectors, reset ingress
checks, and frame-owner metadata constraints; see
[generated-audit.json](compiled-a4bd6fc2ba/generated-audit.json). This is
Quartus diagnostic evidence only. It is not a sealed package, runtime-admitted
profile, or physical DE25 acceptance. No board, kit, image, SD card, QSPI or
harness was touched.

## Functional and software evidence

| Check | Result and boundary |
| --- | --- |
| [Real machine simulation](simulation/README.md) | ROM/editor boot, HID-typed `LOAD ""`, every ordered RAM write from a 910-byte tape, CRC rejection, stopped-pixel progress and HOLD pass; 224 complete 720p frames |
| [Repaired native binding](simulation/repair-binding/README.md) | All 14 generated model files match the passing machine run after private-path normalization and four explicit diagnostic source-location changes; full execution was not repeated |
| [Tape repair](tape-store/README.md) | 155,660 differential cycles and 147,468 defined-byte checks pass; actual isolated Quartus synthesis maps both M20K banks |
| [Fresh software CI](../../../../../docs/validation/2026-10-29-de25-zx81-hps/software/repair/README.md) | All 10 executed jobs pass for a byte-identical checkout tree; two planned simulation jobs skip. Parent: 1,090 cases/2 skips; FPGA software: 1,448 cases/58 skips |
| Focused repair checks | 28 build/client cases and four fresh native endpoint groups pass; 31 generated consumers/37 fixtures match |

The native simulations exercise behavioral memory and directly driven Avalon
inputs, rather than Linux or the generated HPS interconnect. The supplied
generic vendor RAM model reports an AUTO-family fallback; its functional
comparison does not establish Agilex primitive or physical equivalence.
Unwritten tape bytes are not compared because the portable store leaves them
undefined.

## Historical Quartus producer

The earlier successful producer was `5a7382c4b5c17173f48bd074847acc0acc22de93`.
Its 1,792,663-byte SOF had SHA-256
`6f1c6cf7cbcd8f27d9fa7a9fe8cadfa26179af5e6196ba39536bb32e1edf0b77`; it is
not the current-head artifact. Its [artifact receipt](compiled-5a7382c4b5/artifact-receipt.json),
[timing audit](compiled-5a7382c4b5/final-timing-audit.json), and
[raw reports](compiled-5a7382c4b5/reports/golden_top.sta.summary) remain
historical evidence for that producer only.

The [rejected first producer](failed-786e6383b3/README.md) expanded tape RAM
into logic and failed capacity. It produced no SOF. The separate banked repair
preserves the portable native branch verbatim and was part of the historical
`5a7382` producer.

## Open acceptance gates

Physical HDMI/I2C timing, RAM/startup/collisions, CDC/MTBF, the inherited DJ24
voltage discrepancy, firmware pairing and global reset lifetime remain open.
Warm-reset ACK stays inactive. The local transport permanently contains later
resets; that is not a fence for inherited masters. The client supplies
offline regular-file/injected access only. Native bounded MMIO, physical
device binding and production runtime admission remain closed.

Strict deployment classification is `NO_DEPLOY_CHANGE`; this diagnostic
creates no installed image or existing playable-core artifact. The current
SOF and STA cover the merged source revision named above, while Design Closure
and physical acceptance remain open. Any future hardware investigation must
use the exact source-bound artifact and retain these open findings.
