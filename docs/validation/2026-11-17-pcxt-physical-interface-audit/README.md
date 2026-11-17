# PC/XT physical-interface timing audit, 17 November 2026

This first bounded slice of [#835](https://github.com/DeanoC/fes/issues/835)
inventories the existing production DOS artifact's timing coverage. Reported
internal timing passes; external IO/HPS and physical reset qualification remain
open. No RTL, clocks, constraints, compiler pins or production gates change.
No new build, kit access, waveform measurement or hardware acceptance is claimed.

Owner: misteross for the FPGA timing investigation, with FES owning this
integration record. Runtime lifecycle and FogCast coordination are unchanged.

## Source and artifact

- Audited source base: `b79146efc1f05941be64e30e8225887623266cee`.
- Artifact source: `85dbb42815a26ef51c490c79a790f861101b96ff`, produced using
  authenticated Yosys and Quartus Lite 17.0.2 Build 602 for `5CSEBA6U23I7`.
- BUILD_ID: `1d1e4a52fced512afe3c96dc88cc0783`.
- Package: `4e7c0c7ea83702b83987c7ca35c15e00269237a446d8646b51363bf75c9695ec`.
- Archive SHA256: `180abc11f593328b5b8e378ed39a653ebe696516bc66bec9c0aa4a63f1bdec4b`.
- Payload SHA256: `d10aec8b6295b341e688ab12d358f2864cfeca6bffc4417df7bbfdab1e6ae56c`.

All 185 recorded input files match the audited source base byte for byte.
This binds the inventory to those inputs, without changing the artifact's
original revision or claiming a new build. The profile is the production
CPU/board **100 MHz**, 74.25 MHz pixel, phased-HDMI DOS profile; the experimental
CPU100/board50 profile is outside this measurement.

The separate exact-package DOS boot/keyboard evidence is recorded in
[#1869](https://github.com/DeanoC/fes/issues/1869#issuecomment-6948591100).
It does not establish external timing, reset/PLL behavior or ISA containment.

## Measured coverage

[inventory.json](inventory.json) is extracted from the original three seed
reports and independent seed-1 repeat. Each run retains all four native
operating conditions. The extractor compares the raw coverage and unconstrained
port reports against the producer summary, checks both setup and hold port
inventories, validates the package/record identity and hashes every raw input.

| Signoff seed | Worst reported setup | Hold | Recovery | Removal | Inputs without delay | Outputs without delay | No-clock entries |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 0.621 ns | 0.120 ns | 5.647 ns | 0.267 ns | 18 | 73 | 1,234 |
| 2 | 0.347 ns | 0.118 ns | 5.512 ns | 0.422 ns | 18 | 73 | 1,234 |
| 3 | 0.265 ns | 0.116 ns | 5.738 ns | 0.267 ns | 18 | 73 | 1,234 |

These are minima over the reported internal endpoints and corners, not
external-interface margins. Reported minimum-pulse-width checks also pass.
Each run has zero loops, latches, multiple/generated-clock errors, partial
input/output delays, IO min/max inconsistencies, reference-pin errors, PLL
cross-check issues and uncertainty issues. No SDC constraints were ignored.

The missing coverage is stable across all three seeds and the repeat:

| Boundary | Observed coverage | Qualification still owed |
| --- | --- | --- |
| SDRAM | 16 DQ inputs and 39 outputs have no external delay. Outputs include DQ, address/bank, controls/masks and the forwarded clock. | Actual fitted clock/pad arrival, device setup/hold/access and pulse requirements, DQ output-enable/turnaround, board skew/load/flight bounds and capture-to-consumer timing. |
| HDMI video | 28 video/clock outputs have no external delay. | Source-synchronous transmitter setup/hold at the actual forwarded phase, both clock edges, connector/PCB skew and electrical/load bounds. |
| HDMI audio | Four audio outputs have no external delay. Internal audio mailbox and payload gates are present. | Transmitter input setup/hold and audio clock/data relationships at its pins; internal CDC proof is not that budget. |
| HPS/I2C | SCL/SDA are unconstrained in both directions. Each hard-IP instance (`hdmi_i2c`, `fes_computer_runtime_hps_gp`) contributes 617 no-clock entries. | Vendor hard-IP timing/model boundary, open-drain feedback/OE and board pull-up/load/rise-time requirements; GP launch/capture, request synchronization and bundled-data stability. |
| Reset/PLL | Reported internal recovery/removal passes; two-stage release chains exist. | Complete asynchronous assertion/deassertion, lock-loss/relock, minimum pulse and fitted CDC/metastability contract under justified source conditions. |

The 1,234 entries are associated with the two HPS primitive instances. The
producer explicitly inventories and permits that naming boundary; it does not
claim to model its physical timing. Do not label all these entries ordinary
fabric clock defects, or invent clocks/blanket exceptions to remove them.

## Trace through the implementation

- [The board producer](../../../sources/misteross/scripts/quartus_fes_pcxt.py)
  creates only the 50 MHz input clock, derives PLL clocks and uncertainty,
  and adds the scoped video/audio mailbox contracts. The package QSF uses
  [RAM Tester's pin assignments](../../../sources/misteross/cores/fes-ramtest/constraints.qsf)
  plus the PC/XT audio pin assignments. Pin locations and IO standards do not
  provide external delay requirements.
- [The top](../../../sources/misteross/cores/fes-pcxt/rtl/fes_pcxt_top.sv)
  captures SDRAM DQ in fabric registers on the 180-degree memory PLL output.
  [The controller](../../../sources/misteross/cores/fes-ramtest/rtl/sdram_addon_port.v)
  forwards the SDRAM clock through `altddio_out`, high on the falling half-cycle.
  The topology alone gives no routed chip-pin budget.
- The top requests an HDMI clock phase of 8.979 ns. TimeQuest reports a
  13.464 ns pixel period and forwarded rising edge at 8.976 ns. This fitted PLL
  relationship is present, but none of the 28 external video outputs has a
  receiver delay requirement in the captured reports.
- `locked = memory_locked && video_locked` asynchronously asserts `sys_reset`
  and `pixel_reset`; each deasserts through its own two-stage release chain.
  The [mailbox SDC](../../../sources/misteross/cores/fes-pcxt/quartus/mailbox.sdc)
  cuts only the three foreign launches to first data synchronizer stages and
  bounds the held payload. The [audio SDC](../../../sources/misteross/cores/fes-pcxt/quartus/audio.sdc)
  likewise binds its source/stage pairs and held payload. Neither supplies
  a physical PLL-lock/reset source specification.
- [The package admission gate](../../../sources/misteross/scripts/build_fes_pcxt_quartus.py)
  requires the internal matrix, pulse-width and mailbox/audio checks while
  preserving the known external and hard-IP gaps. This audit changes no gate.

Historical [pad/board50 measurements](../../../sources/misteross/docs/validation/2026-10-22-pcxt-pad-pipeline/whole-board-pipeline.md)
include conditional chip-pin and HPS feedback/OE studies. Their different
profiles, routes, declared loads and assumed flight times do not qualify this
100 MHz artifact. The [external SDRAM model](../../../sources/misteross/docs/architecture.md#sdram-external-timing-model)
is useful methodology; its example board bounds cannot be adopted as measurements.

## Next bounded qualification slice

Start with the **production SDRAM channel**, retaining this artifact as the
control. Establish the actual kit/module part and revision, and cite its
datasheet and board/load/flight bounds. Then extract per-corner fitted
forwarded-clock, DQ capture, command/data and OE arrivals from a private copy of
the exact seed database. Join those observations to an explicit min/max budget
for reads, writes, turnaround and clock high/low width. Report unsupported or
unmeasured terms as unknown; a conditional zero-flight calculation is only a
diagnostic. Do not select the board50 profile, lower clocks or reroll seeds to
avoid a missing contract.

HDMI receiver budgets, HPS primitive/CDC boundaries, and physical reset/PLL
qualification follow as separately measured boundaries. A constraint/recipe
repair, if justified, needs its own normal seal and exact-artifact acceptance.
Physical ISA placement/admission remains a separate #835 obligation; this
audit does not evaluate or qualify physical slots.

## Reproduce and retain

On Powerboat, from the FES task checkout:

```sh
python3 docs/validation/2026-11-17-pcxt-physical-interface-audit/audit.py \
  --attempt /home/deano/.codex/worktrees/8ce6/fes/sources/misteross/build/fes-pcxt-quartus/1d1e4a52fced512afe3c96dc88cc0783/attempt-ggmnsyd5 \
  --package /home/deano/fes-hil/k1896-codex-3d873bb6/4e7c0c7ea83702b83987c7ca35c15e00269237a446d8646b51363bf75c9695ec.fcore \
  --module-root sources/misteross --output /tmp/pcxt-physical-inventory.json
```

The command is read-only apart from its output file. The recorded source-base
SHA identifies the checkout used for this audit; rerunning from a later commit
records that commit and refuses changed production input bytes.

The original attempt retains the full fitted databases and reports. A selected
raw-report copy, archive, record, extraction script and hash manifest are
retained at `/home/deano/fes-hil/k835-physical-interface-audit/`. Validation is
host-only inspection of existing exact-artifact evidence. HIL is N/A for this
documentation/report extractor; #835 remains open for physical qualification.
