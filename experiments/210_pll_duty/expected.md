# 210 integer 25% duty 25 MHz PLL

`make sim EXP=210_pll_duty` tests ten reset/relock cycles of a 50→25 MHz PLL
with `duty_cycle0(25)` and a mixed-edge capture path. `make oss EXP=210_pll_duty`
produces compressed `build/oss/210_pll_duty/top.rbf`. It requires one PLL, one
HPS GP, no memory or DSP, and passing 50/25 MHz timing. Quartus comparison is
not implemented.

This experiment programs the Quartus-checked M=12 N=2 C6=12 counters with C high
3 / low 9. Frequency remains 25 MHz; the existing edge meter cannot observe
pulse width. GPO bit0 is launched on the rising output edge and captured on the
falling edge, proving both edges after nextpnr folds the inverted clock into
hardware FF inversion. Simulation uses a digital 25 MHz toggle and a lock delay,
not analog 25% duty. 75% duty is accepted by the compiler but is not a separate
ladder experiment. Fractional-N profiles still require 50% duty.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD718` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [9] | Launch FF (rising output edge of GPO [0]) |
| GPI [8] | Mixed-edge capture of that launch |
| GPI [7:0] | Selected result byte |
| GPO [4] | Select high byte when set |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Launch data for the mixed-edge path |

Over `2^20` reference cycles the meter is 2047–2049. Held reset returns count 0.
Launch and falling-edge capture both follow GPO [0]. That requires Mistral
`b28e30a` (corrected LAB clock invert addresses). This does not measure pulse
width. Claim the designated kit with `scripts/kit.py session`, load the exact
OSS RBF, and run `hardware/probe.sh`. Never take over another owner.
