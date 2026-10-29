# DE25 HPS-controlled ZX81 diagnostic

This adds execution, HID keyboard and CRC-checked tape control through the
preserved Terasic GHRD lightweight HPS bridge. The actual ZX81 machine runs
on the existing 100 MHz peripheral clock with 13/200 enables; HDMI retains
its separate exact 74.25 MHz PLL. The new path adds no DMA master. The GHRD's
HPS, LPDDR4, inherited probe/debug masters and boot files remain selected.

Base: `1db4ba71e973aa01fc521fe42ed0a901061ad8b0`.
Compiled producer: `5a7382c4b5c17173f48bd074847acc0acc22de93`.
Later evidence commits do not change the producer's 41 selected input files.
The new transport has two generated consumers, plus one existing-computer-ABI
header consumer. The computer ABI and shared production RTL are unchanged.

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
The actual compiled RAM mapping is checked separately below. Unwritten tape
bytes are not compared because the portable store leaves them undefined.

## Compiled artifact

Quartus Pro 26.3.0 Build 114 completes with zero errors and 330 warnings for
`A5EB013BB23BE4SCS`, seed 1 and two CPU workers. The retained diagnostic SOF is
`out/de25-zx81-hps-5a7382c4b5/hps/project/output_files/golden_top.sof` in the
task worktree: 1,792,663 bytes, SHA-256
`6f1c6cf7cbcd8f27d9fa7a9fe8cadfa26179af5e6196ba39536bb32e1edf0b77`.
It is not a sealed package or an admitted runtime profile.

[Artifact/source receipt](compiled-5a7382c4b5/artifact-receipt.json),
[generated-design audit](compiled-5a7382c4b5/generated-README.md),
[final timing audit](compiled-5a7382c4b5/final-timing-audit.json) and
[raw reports](compiled-5a7382c4b5/reports/golden_top.sta.summary) bind this
producer and artifact. Forty prepared top/board/source/SDC/init/software files
remain byte-identical. The complete QSF comparison permits only removal of
the duplicate older tool-version declaration, four exact generated IP
entries and the unchanged FAMILY line ending; clocks and constraints are not relaxed. The source fingerprint
`a6b159e617d400d212240b31944cf377` is embedded as BUILD_ID.

All 38 worst-corner STA summary rows have displayed nonnegative slack and
zero TNS. These are aggregate worst-corner rows across four models.

| Analysis | Minimum displayed slack, ns |
| --- | ---: |
| Setup | 1.630 |
| Hold | 0.000 |
| Recovery | 7.426 |
| Removal | 0.122 |
| Minimum pulse width | 0.220 |

Hold is a rounded displayed zero, with no positive-margin claim. All 41
clocks are constrained; all 35 baseline clock name/type/period/frequency
tuples remain unchanged. Exact PLL counters establish 100 and 74.25 MHz.

Fitter logic utilization is 13,895 ALMs, with 15,022 placed and 23,466 logic
registers. It uses 279 of 358 M20Ks and 2,688,512 block-memory bits. Tape uses
eight physical M20Ks, four per bank, with 131,072 bits and old-data reads.
There are three PLLs and no DSP blocks. This includes the inherited GHRD.

The [rejected first producer](failed-786e6383b3/README.md) expanded tape RAM
into logic and failed capacity. Its reports, source identity and unsuccessful
mapping remain recorded; it produced no SOF. The separate banked repair
preserves the portable native branch verbatim and is part of producer `5a7382`.

## Open acceptance gates

Overall Design Closure remains **FAIL**. Five unwaived classes remain:
missing output delays (30), missing input delays (2), reset polarity (1),
multiple reset synchronizers (1), and intra-clock reset false-path advisories
(2). The reset groups are retained from the baseline. There are zero waivers.
STA reports two unconstrained inputs/nine input path pairs and 30 outputs/
3,927 output path pairs for setup and hold. MTBF models 24 of 40 found chains
and excludes 16; it does not qualify the whole design or board.

Physical HDMI/I2C timing, RAM/startup/collisions, CDC, the inherited DJ24
voltage discrepancy, firmware pairing and global reset lifetime remain open.
Warm-reset ACK stays inactive. The local transport permanently contains
later resets; that is not a fence for inherited masters. The client supplies
offline regular-file/injected access only. Native bounded MMIO, physical
device binding and production runtime admission remain closed.

Strict deployment classification is `NO_DEPLOY_CHANGE`; this change creates
no installed image or existing playable-core artifact. The next integration
step is review, followed by first-board serial/DHCP/SSH/reboot acceptance of
the separate headless Linux image, then a controlled SRAM diagnostic test of
this exact SOF, its native register access, HID input, tape and HDMI.
No board, kit, image, SD card, QSPI or harness was touched.
