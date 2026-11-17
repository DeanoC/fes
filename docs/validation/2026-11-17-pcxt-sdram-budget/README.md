# Production PC/XT SDRAM reference budget, 17 November 2026

The production recipe's SDRAM interface **does not pass this conditional
single-edge reference budget**. All three existing signoff fits have read and
DQ-write setup misses with the reference delays below. The previously timed
capture-to-controller handoff passes. This narrows [#835](https://github.com/DeanoC/fes/issues/835)
without changing RTL, pin assignments, clocks, constraints, compiler pins or
production gates. No fit, package, kit operation or hardware acceptance occurs.

This follows the coverage audit in [#1939](https://github.com/DeanoC/fes/pull/1939).
A conditional timing result is not completed physical qualification or a claim
that a DOS boot failed. #835 remains open.

## Identity and conditions

Source base: `84318b52dbfadcd51b8cd92065756ea50fd5ef91`. All 185 recorded
production inputs match that checkout. Artifact source remains
`85dbb42815a26ef51c490c79a790f861101b96ff`, BUILD_ID
`1d1e4a52fced512afe3c96dc88cc0783`. The sealed package is
`4e7c0c7ea83702b83987c7ca35c15e00269237a446d8646b51363bf75c9695ec`;
its payload SHA256 is
`d10aec8b6295b341e688ab12d358f2864cfeca6bffc4417df7bbfdab1e6ae56c`.
The sealed bytes match original seed 1 and its independent repeat. Seeds 2/3
are other signoff fits of that recipe, with their own RBF hashes in
[summary.json](summary.json); they do not inherit seed 1's hardware acceptance.

Deano confirmed on 17 November that **Kit A matches the published Etron
module**: two EM63B165TS-6SG devices. This is operator-supplied identity, not a
physical inspection performed by this audit. Kit B's population remains unknown.
The [manufacturer product](https://retroremake.co/products/128mb-sdram-mister-fpga-compatible)
identifies the module's XS-DS v3.0 basis; the retained
[photo/source record](../../../sources/misteross/docs/validation/2026-10-22-pcxt-pad-pipeline/retroremake-sdram-photo-reference.json)
distinguishes the Etron photo from the separate Alliance-populated schematic.
Fitted inverter, load, slew and board-flight bounds remain unverified.

[reference.json](reference.json) binds the primary datasheets and assumptions:

- [Etron revision 2.4](https://etron.com/wp-content/uploads/2022/04/EM63B165TSBM_Rev-2.4.pdf),
  tables 16/17, pages 21/22: -6 CL2 minimum cycle 10 ns, access maximum 6 ns,
  output hold minimum 2.5 ns, input setup/hold 1.5/0.8 ns, clock high/low
  minima 2/2 ns, low-impedance minimum 0 ns and high-impedance maximum 5.4 ns.
  Conditions include 3.3 V ±0.3 V, 0–70 °C, the stated LVTTL test load and
  1 ns transitions. Notes 9/10 require applicable slew compensation. FPGA
  timing corners do not extend the memory's operating range.
- [TI SN74LVC1G04 revision AF](https://www.ti.com/lit/ds/symlink/sn74lvc1g04.pdf),
  section 5.7: the optional inverter reference is 1–4.2 ns at 3.3 V ±0.3 V,
  50 pF, −40–85 °C. This is a conditional second-chip-select case, not proof
  of Kit A's fitted inverter or load.

The 100 MHz nominal cycle is exactly the memory reference's 10 ns CL2 minimum;
there is no nominal cycle-time headroom. Pin-level jitter and pulse-width
bounds are still owed. No board flight/skew, additional margin or measured
loading is substituted for the unknown physical values.

## Budget construction

Each run uses a **private copy** of an existing seed's post-fit database.
[collect.tcl](collect.tcl) reads its original SDCs and derives a generated clock
at `SDRAM_CLK`, inverted from the actual memory PLL output, matching the
controller's DDR forwarded-clock polarity. The retained full-path reports show
that clock's routed DDR/output-buffer path; an ideal pin clock is not substituted.
Clock uncertainty is re-derived with the new pin clock and remains active.

For this zero-PCB-flight/skew reference:

| Constraint relative to the forwarded pin clock | Maximum | Minimum |
| --- | ---: | ---: |
| DQ read input delay | 6.0 ns | 2.5 ns |
| Direct output delay | 1.5 ns | −0.8 ns |
| Optional second-CS inverter output delay | 5.7 ns | +0.2 ns |

For general external clock flight `Fc`, returning data flight `Fr`, and outgoing
data flight `Fw`, the missing physical terms enter as follows:

```text
input_max  = Fc_max + tAC_max + Fr_max
input_min  = Fc_min + tOH_min + Fr_min
output_max = tIS + Fw_max - Fc_min
output_min = Fw_min - Fc_max - tIH
```

Inverter propagation belongs in `Fw`, so its minimum helps hold and its maximum
costs setup. Zero flight is a declared reference, not a measured kit bound or
universally optimistic skew assumption. Native fitter electrical assumptions
are retained; this run does not claim that they match board loading.

The collector queries all four native operating conditions independently:
16 reads, 38 outputs, 16 explicit OE-source paths and one separate second-CS
reference, for setup and hold. That is 568 rows per seed, 1,704 total. All are
present, unique and finite. DQ data paths bind their actual `dq_out` sources;
OE paths bind `dq_oe` separately. The OE rows project that signal into the same
pin window; they **do not establish electrical turnaround/non-overlap**.

The 38 shell outputs remain in the raw inventory. The summary separates CKE
and DQML/DQMH diagnostic mirrors from the connector-reference command/address
signals: the shared controller documents A11/A12 mask multiplexing. This avoids
presenting the mirrors' setup misses as failures of the physical command group;
it does not silently waive their mapping or qualify a fitted connector.

## Results

Each cell is the minimum setup / hold slack across all four FPGA corners,
in nanoseconds. The worst endpoints and all report hashes are in
[summary.json](summary.json).

| Group | Seed 1 | Seed 2 | Seed 3 |
| --- | ---: | ---: | ---: |
| Read to fabric capture | −8.717 / +5.071 | −9.558 / +5.098 | −8.811 / +5.149 |
| DQ write data | −5.582 / +3.860 | −7.745 / +3.951 | −6.521 / +4.136 |
| Connector-reference command/address | +2.238 / +3.138 | +2.201 / +3.151 | +2.243 / +3.136 |
| Shell diagnostic mirrors | −2.077 / +3.699 | −1.669 / +3.699 | −1.473 / +3.714 |
| OE pin-window diagnostic | −5.400 / +5.085 | −5.129 / +5.230 | −6.831 / +5.171 |
| Conditional second-CS inverter | −0.937 / +4.418 | −0.936 / +4.418 | −0.936 / +4.413 |
| Native capture-to-controller handoff | +0.908 / +4.343 | +1.522 / +4.319 | +0.827 / +4.321 |

The last row is independently extracted from each **unchanged original native
endpoint report**: 16 capture launches into the matching controller `dq_fall`
registers, setup/hold at four corners, 128 rows per seed. It is separate from
both the incoming pin timing and the controller's later consumption window.

TimeQuest completes successfully; that means report collection succeeded,
not that the negative slacks pass. Each reference run has two expected
332054 warnings when replacing direct-CS delays with the separate inverter
scenario. No constraints are ignored. Original failed/API-probe traces remain
retained; no failed numerical result is overridden.

## What is still required

The controller holds write data/OE before the column command and selects read
consumption with `wait_count`, `OUT_LATENCY` and `dq_fall`. Consequently these
single-edge pin checks are a reference screen, not a proved transaction timing
contract. The next bounded step is to bind the exact READ/WRITE command,
CAS-2 launch, capture, registered handoff and final consumption edges, and prove
held-data/OE stability and bus release under the actual controller sequence.
No multicycle exception is applied here; any allowance must follow that proof.

The second-CS reference also misses setup and needs its fitted part and
propagation conditions bound. Physical clock high/low width and cycle jitter,
actual flight/skew/load/slew, memory conditions, and read/write turnaround
using `tLZ`/`tHZ` remain open. Internal pulse-width reports are retained but are
not a pin-level SDRAM waveform qualification. A board50 route, a faster seed,
a lower clock or a relaxed constraint cannot discharge these missing terms.
No recipe or RTL repair is selected by this reference alone.

HDMI, HPS/I2C, reset/PLL and physical ISA containment remain the other separate
#835 boundaries. This record grants no new hardware acceptance.

## Reproduce and retain

Use an original production attempt containing `signoff/seed-{1,2,3}` and
`repeat/seed-1`, and the sealed package above. Preserve those inputs. Copy a
seed directory to a private output directory, then run:

```sh
python3 docs/validation/2026-11-17-pcxt-sdram-budget/run_sta.py \
  /absolute/private/seed-1 \
  docs/validation/2026-11-17-pcxt-sdram-budget/collect.tcl
```

Repeat for seeds 2/3. The driver refuses under 30 GiB free, records the argv and
cwd, emits a 15-second heartbeat, caps each STA process at 600 seconds and
terminates only its owned process group. It invokes the existing Quartus Lite
17.0.2 installation on Powerboat; it does not compile, fit, export or deploy.

```sh
python3 docs/validation/2026-11-17-pcxt-sdram-budget/summarize.py \
  --attempt /absolute/original/attempt --package /absolute/sealed.fcore \
  --module-root sources/misteross --work /absolute/private/seeds \
  --quartus-sta /home/deano/intelFPGA_lite/17.0/quartus/bin/quartus_sta \
  --output /tmp/pcxt-sdram-summary.json
```

The summarizer binds the record/package, signoff/repeat payload, tool binary,
all 185 source hashes and unchanged copied QSF/SDCs/RBFs. It validates all row
identities and cardinalities and records conditional misses explicitly.
Exit 0 means evidence validation, not physical PASS.

Full private fits/reports/logs are under the task checkout's
`out/sdram-budget/`. Selected reports, raw rows, native handoff inputs,
datasheets, scripts and a verified hash manifest are retained at
`/home/deano/fes-hil/k835-sdram-budget/`. These are host-only conditional
post-fit measurements. HIL is N/A for the report/extractor; #835 stays open.
