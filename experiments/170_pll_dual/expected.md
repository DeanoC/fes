# 170 dual 25/40 MHz PLL

`make sim EXP=170_pll_dual` tests reset/relock and frequency meters on both
outputs of one `altera_pll`. `make oss EXP=170_pll_dual` produces compressed
`build/oss/170_pll_dual/top.rbf`. It requires one PLL, one HPS GP, no memory or
DSP, and passing 50/25/40 MHz timing. Quartus comparison is not implemented.

This experiment measures the original PIN_V11 50 MHz → 25 MHz and 40 MHz pair
in direct integer mode, zero phase, 50% duty, shared M=16 N=2 (reported 400 MHz)
with C6=16 and C7=10. The compiler also accepts other whole-MHz pairs that share
one checked 300/320/400 MHz feedback configuration. GPO bit3 selects which meter
is visible. Simulation models digital 25/40 MHz clocks and a lock delay, not
analog acquisition.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD714` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [3] | Select 40 MHz meter when set, 25 MHz when clear |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the 25 MHz meter is 2048 ±1 and the 40 MHz meter
is 3276–3277. Held reset returns count 0. Claim the designated kit with
`scripts/kit.py session`, load the exact OSS RBF, and run `hardware/probe.sh`.
Never take over another owner.
