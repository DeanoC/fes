# 350 two independent PLLs on V11

`make sim EXP=350_pll_two` tests reset/relock and frequency meters on two
independent `altera_pll` cells sharing the 50 MHz V11 reference: 25 MHz integer
and 12.288 MHz fractional-N. `make oss EXP=350_pll_two` produces compressed
`build/oss/350_pll_two/top.rbf`. It requires two PLLs, one HPS GP, no memory or
DSP, and passing 50/25/12.288 MHz timing. Quartus comparison is not implemented.

This experiment measures the checked second FPLL site on PIN_V11. Both PLLs use
direct mode, zero phase and 50% duty. GPO bit 3 selects which meter is visible.
Source names avoid the forbidden `audio` token.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD726` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized selected PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [3] | Select 12.288 MHz meter when set, 25 MHz when clear |
| GPO [2] | Active-high reset request for both PLLs |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the 25 MHz meter is 2047–2049 and the 12.288 MHz
meter is 1006–1007. Held reset returns count 0. Claim the designated kit with
`scripts/kit.py session`, load the exact OSS RBF, and run `hardware/probe.sh`.
Never take over another owner.
