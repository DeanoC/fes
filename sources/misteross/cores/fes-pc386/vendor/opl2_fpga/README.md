# opl2_fpga import

Pinned from [opl2_fpga_MiSTer](https://github.com/gtaylormb/opl2_fpga_MiSTer/tree/451e5b0b23c961666a60619a057ee5842664a5a9/src),
Greg Taylor's MiSTer port of [opl2_fpga](https://github.com/gtaylormb/opl2_fpga),
the YM3812 cut of opl3_fpga, under the retained LGPL-3.0-or-later
(`COPYING.LESSER`). `source.json` records upstream paths and hashes, and the
upstream hash of every file changed here. The
[pc386 machine design](../../../../docs/pc386-machine-design.md#opl2) chose this
import.

Only the synthesis core is vendored: `channels` and what it instantiates. The
host interface, clock divider, timers, reset synchroniser and LEDs of the
upstream top level are not; [`pc386_opl2.sv`](../../rtl/pc386_opl2.sv) writes
`opl2_reg_wr_t` directly, gives the core its 49,715.9 Hz sample enable from a
rational enable, and owns the status register and both timers.

## Local changes

The reference is Nuked-OPL3 (`765ec962`) in OPL2 mode, built with its
defaults. Upstream drifted from it in several places; each change below makes
the core sample-exact against it, and `python3 scripts/sim_fes_pc386.py --suite opl2`
checks the result.

- `phase_generator.sv`: key-on resets the phase to this sample's increment.
  Nuked-OPL3 resets, then adds; upstream reset to zero, which delayed every
  note by one sample.
- `calc_envelope_shift.sv`: `EG_INC_STEP` was packed with rows and bits
  reversed against its `[rate_lo][eg_timer_lo]` index, so attack, decay and
  release at rates 12-14 took the wrong steps.
- `envelope_generator.sv`: the AM bit is pipelined to stage 2. Upstream read it
  from the asynchronous register memory there, which by then addresses the
  next operator.
- `tremolo.sv`, `vibrato.sv`, `calc_phase_inc.sv`: both LFOs rewritten to
  Nuked-OPL3's counters. Tremolo is a 0-209 triangle stepped every 64 samples
  with a depth shift of 2 or 4; vibrato is eight positions stepped every 1024
  samples, and its offset moves the f-number before the block shift and
  multiplier instead of the phase increment.
- `calc_rhythm_phase.sv`: the noise generator is Nuked-OPL3's 23-bit register,
  stepped once per operator slot (36 per sample, as on the OPL3 it models), and
  the top-cymbal phase is latched only in rhythm mode.
- `operator.sv`: a drum operator is keyed by its drum bit or its channel's
  key-on, as Nuked-OPL3 ORs the two.
- `control_operators.sv`: NTS is latched per channel when that channel's
  f-number registers are written, when Nuked-OPL3 derives the key scale value.
- `channels.sv`: channels 6-8 mix their second operator from the previous
  sample (Nuked-OPL3's default `OPL_QUIRK_CHANNELSAMPLEDELAY`), and the channel
  sum is no longer doubled, so the output has Nuked-OPL3's scale and clipping.
- `opl2_pkg.sv`: `INSTANTIATE_SAMPLE_DAC_CDC = 0`; the sample stays in the
  machine clock domain.

The locked Yosys (`cores/fes-pc386/toolchain.lock`) cannot read five
SystemVerilog forms the import uses. These changes keep the same registers,
values and widths; the opl2 and audio suites pass unchanged on them, and each
form has a DeanoC/yosys issue linked from the
[M4 record](../../../../../../docs/validation/2026-10-28-pc386-m4-diagnostic/summary.md).

- `channels.sv`, `mem_single_bank_reset.sv`: `state` and `self` get their
  power-on values from `initial` statements, not initializers on their
  anonymous enum and packed struct declarations.
- `pipeline_sr.sv`: the power-on value is an `initial` replication, not
  `'{default: POR_VALUE}` on a packed array.
- `calc_envelope_shift.sv`: `EG_INC_STEP` is a plain 16-bit localparam indexed
  by `{rate_lo, eg_timer_lo}`, the bits the typed `[3:0][3:0]` localparam's
  `[rate_lo][eg_timer_lo]` reads.
- `phase_generator.sv`, `calc_rhythm_phase.sv`, `envelope_generator.sv`,
  `control_operators.sv`: `$bits` of a type or a part select is the constant
  it names (3, 4 and 2).
- `control_operators.sv`: the operator alignment assertion is simulation-only
  (`` `ifndef SYNTHESIS ``); Yosys has no concurrent assertions.

## Known differences from a YM3812

These follow the reference, not the chip, and are recorded in the design.

- The waveform-select enable (register 01h bit 5) is not modelled: E0h-F5h
  select a waveform whatever bit 5 holds, as in Nuked-OPL3.
- CSM mode (register 08h bit 7) is ignored, as in Nuked-OPL3.
