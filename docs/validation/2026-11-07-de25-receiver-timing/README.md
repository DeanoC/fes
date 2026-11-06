# DE25 receiver timing in the sealed producer

The actual pixel PLL clock is now forwarded to `HDMI_TX_CLK` in the producer's
source-bound SDC. All 27 RGB/DE/sync outputs receive the ADV7513 Rev B receiver
minima: 1.8 ns setup, 1.3 ns hold. The fitter can therefore repair the previously
observed hold failure. No launch sources are excluded, and there are no new
timing cuts, receiver-register changes, clock phase changes or voltage changes.

Implementation/compiled source: `f1c2429494a17fcd1e60a18625a446d6ee71070b`.
Package: `6009fb08a88d9566bca771b300301b2f7dcb00ca820bcdffa5225f98d205b9c9`.
Functional build ID: `0c305eee00e86b080327f3c218d06d0b`.

The limits are receiver-only requirements at a **zero PCB-skew reference**.
They do not describe a measured board budget. The inherited clock voltage/path,
trace skew and output loading remain unqualified. See the
[receiver datasheet](https://www.analog.com/media/en/technical-documentation/data-sheets/ADV7513.pdf).

## Build and routed checks

Fresh Quartus Pro 26.3 compilation used eight workers on the 3090 under the
normal-user shared lock. All ten phases pass their unchanged 120 s limits;
elapsed time is 545.656 s. The package, 200
source inputs and complete compiler-recorded timing/handoff inventory verify.
The QSF differs from the previous candidate only in the embedded build ID.
Physical pin, voltage, phase and load assignments are byte-for-byte unchanged.

A private copied-netlist query loads only the sealed project's own SDC. It
adds no clocks, output delays or timing exceptions. It checks all 27 pads over
four operating corners (108 pad observations) and setup/hold witnesses from
every one of 42 clocks in each corner (168 inventory rows). Only the pixel
clock launches data/control paths. All pads have nonnegative setup/hold slack;
all worst full-path witnesses latch on `adv7513_video`. Querying leaves the
original QSF, QPF, SOF, timing report and sealed SDC unchanged.

| Receiver-reference minimum, ns | Previous diagnostic | Sealed constraints |
| --- | ---: | ---: |
| Setup | 3.307 | 3.472 |
| Hold | -0.013 | 0.006 |

The new worst setup and hold witnesses both end at data[9], in different
corners and from different actual logic. The reference hold failure is repaired,
but **6 ps reported hold margin cannot qualify unknown board skew/loading**.
This is not physical HDMI acceptance or a paired QoR comparison.

| Reported complete-design minimum, ns | Previous | Receiver constraints |
| --- | ---: | ---: |
| Setup | 1.790 | 1.635 |
| Hold | 0.000 | 0.000 |
| Recovery | 6.557 | 5.806 |
| Removal | 0.065 | 0.037 |
| Minimum pulse width | 0.220 | 0.220 |

Reported constrained TNS is zero; internal hold still rounds to 0.000 ns with
no reported margin. The one added analysis clock is the forwarded pixel clock;
oscillators and PLL parameters are unchanged. System/pixel reset-chain counts
remain four/two. Four signoff classes remain, with zero waivers.

The missing-output-delay table falls from 30 to three findings: `LED[7]`,
`HDMI_I2C_SDA`, `HDMI_I2C_SCL`. The unconstrained-path summary separately lists
two output ports (the I2C lines), so those counts must not be conflated. Its
two unconstrained inputs remain `HDMI_I2C_SDA` and `HDMI_TX_INT`. Multiple reset
chains still have two domain findings and intra-clock reset false path has one.
No LED, I2C or reset waiver is used to clear these gates.

## Software and native admission

The actual SDC executes under Tcl against eleven inventories. Valid inventory
retains all five prior ingress cuts and five metadata bounds, the forwarded
clock, both receiver limits and all 27 pads. Missing/ambiguous clock, target or
pad inventory rejects, along with the existing reset/CDC negative controls.
The fresh actual compile separately establishes real endpoint resolution.

The complete affected suite passes: 1,425 parent cases (two skips), 1,587 FPGA
cases (75 skips), planner/whitespace checks, 48 generated consumers, 37 fixture
copies and actual Avalon/native integration. It includes the pinned writer
induction, broken-capture negative control and full dual-clock video model.
The prior writer/ROM model receipts remain source-identical: all 73 recorded
hashes match. No functional HDL changed in this milestone.

The new real package and retained 8 KiB ZX81 ROM pass the actual shared linker,
resource-map generation and native loader preflight under the race detector.
The single selection causes zero firmware calls. No SD access, native boot,
physical driver grant or core-load acceptance follows from that check. A
matched native boot chain for this package has not yet been built.

## Electrical and hardware boundary

All 31 relevant pin locations still match the manufacturer's Rev B manual.
The manual lists the HDMI clock as 3.3 V, while both the vendor reference and
actual fitter assign 1.1-V on DJ24, bank 2A_T, with reported clock load 0 pF.
The source-bound pin check retains that inherited discrepancy and the vendor
QSF bytes. No translator, valid electrical path, trace delay or load is inferred.

The official [resource index](https://dl2.terasic.com/resources/de25-nano/) and
the inspected Rev B archive do not supply a schematic resolving that path.
This does not establish that no manufacturer schematic exists. A user question
for a schematic/routing document is pending; independent work continues.
The [ADV7513 hardware guide](https://www.analog.com/media/en/technical-documentation/user-guides/adv7513_hardware_user_guide.pdf)
section 6.1.6 does not establish a valid direct 1.1 V connection. The board's
actual electrical path must be verified before using the reference timing as
board signoff. No inherited IO standard is changed speculatively.

No UART opening, JTAG operation, FPGA programming, SD write or flash action
occurred. CPU/FPGA running state still requires requalification after the user's
reported power interruption. Historical recovery inputs verify; no restore was
attempted. The continuing top-kit claim remains held and the bottom kit is
untouched. Factory restoration remains waived by the user.

Next hardware-independent work is HDMI interrupt synchronization, actual I2C
timing and LED classification, plus remaining reset ownership. In parallel,
resolve the electrical clock path and real board skew/load contract. A fresh
matched native chain/operator and bounded ordinary core/ROM/input/return trial
remain necessary. Physical menu/core transitions, media and native networking
remain open toward full DE10-style operation.

Raw evidence: `out/nano-receiver-timing-20261107/`; `result.json` binds exact
receipts. Retention target:
`/home/deano/fes-retained/de25-receiver-timing-20261107-r1`, dependent on the
pixel-mask retention. No push, PR, merge or lane handoff is implied.
