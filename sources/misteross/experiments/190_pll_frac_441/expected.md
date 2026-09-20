# 190 fractional-N 11.2896 MHz PLL

`make sim EXP=190_pll_frac_441` tests ten reset/relock cycles of the checked
50→11.2896 MHz fractional-N profile. `make oss EXP=190_pll_frac_441` produces
compressed `build/oss/190_pll_frac_441/top.rbf`. It requires one PLL, one HPS GP,
no memory or DSP, and passing 50/11.2896 MHz timing. Quartus comparison is not
implemented.

This experiment measures PIN_V11 50 MHz → 11.2896 MHz (256 × 44.1 kHz) with
`fractional_vco_multiplier("true")`, direct integer-looking ports, zero phase
and 50% duty. nextpnr programs the Quartus-checked M=8 N=1 C6=36 tuple and
32-bit K=551954751. Other fractional-N rates remain rejected except the closed
12.288 MHz single and 12.288/24.576 MHz dual profiles. Simulation models a
digital 11.2896/50 ratio and a lock delay, not analog acquisition or the 32-bit
fraction word.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD716` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the meter is 924–925. Held reset returns count 0.
The window cannot resolve the calculated −0.00246747 ppm quantization. Claim
the designated kit with `scripts/kit.py session`, load the exact OSS RBF, and
run `hardware/probe.sh`. Never take over another owner.
