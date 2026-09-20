# 200 dual fractional-N 12.288/24.576 MHz PLL

`make sim EXP=200_pll_frac_dual` tests reset/relock and frequency meters on both
outputs of one fractional `altera_pll`. `make oss EXP=200_pll_frac_dual` produces
compressed `build/oss/200_pll_frac_dual/top.rbf`. It requires one PLL, one HPS GP,
no memory or DSP, and passing 50/12.288/24.576 MHz timing. Quartus comparison is
not implemented.

This experiment measures the checked PIN_V11 50 MHz → 12.288 MHz and 24.576 MHz
pair with `fractional_vco_multiplier("true")`, direct mode, zero phase and 50%
duty. nextpnr programs the Quartus-checked shared M=8 N=1 K=1528394891
(`0x5b18548b`) configuration with C6=34 and C7=17. Swapped outputs and other
fractional pairs remain rejected. The single 12.288 MHz fractional word is a
different configuration and is not reused. GPO bit3 selects which meter is
visible. Simulation models digital 12.288/24.576 MHz clocks and a lock delay,
not analog acquisition or the 32-bit fraction word.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD717` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [3] | Select 24.576 MHz meter when set, 12.288 MHz when clear |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the 12.288 MHz meter is 1006–1007 and the
24.576 MHz meter is 2013–2014. Held reset returns count 0. Claim the designated
kit with `scripts/kit.py session`, load the exact OSS RBF, and run
`hardware/probe.sh`. Never take over another owner.
