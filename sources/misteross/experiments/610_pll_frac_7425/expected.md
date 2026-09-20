# 610 fractional-N 74.25 MHz PLL

`make sim EXP=610_pll_frac_7425` tests ten reset/relock cycles of the checked
50→74.25 MHz fractional-N profile. Simulation uses a digital 25 MHz stand-in
and a lock delay, not the analog 74.25 MHz ratio. `make oss EXP=610_pll_frac_7425`
produces compressed `build/oss/610_pll_frac_7425/top.rbf`. It requires one PLL,
one HPS GP, no memory or DSP, and passing 50/74.25 MHz timing. Quartus
comparison is not implemented.

This experiment measures PIN_V11 50 MHz → 74.25 MHz with
`fractional_vco_multiplier("true")`, direct mode, zero phase and 50% duty.
nextpnr programs the Quartus-checked M=8 N=1 C6=6 tuple and 32-bit
K=`0xe8f5c239` (calculated 74,249,999.83243954 Hz). Integer mode still
rejects 74.25 MHz. Other references, nearby rates, nonzero phase, other
duties and multi-output combinations remain rejected.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD742` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the hardware meter is 6081–6084 (nominal
6082.56). Held reset returns count 0. The window cannot resolve the
calculated −0.00226 ppm quantization, jitter or HDMI signaling. Claim the
designated kit with `scripts/kit.py session`, load the exact OSS RBF, and
run `hardware/probe.sh`. Never take over another owner.
