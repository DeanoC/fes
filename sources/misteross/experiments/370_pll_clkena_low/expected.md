# 370 low-startup PLL clock enable

`make sim EXP=370_pll_clkena_low` tests reset/relock, a low-startup gated-off
meter, and an enabled 25 MHz meter on one integer `altera_pll` plus
`cyclonev_clkena`. `make oss EXP=370_pll_clkena_low` produces compressed
`build/oss/370_pll_clkena_low/top.rbf`. It requires one PLL, one clock-enable
primitive, one HPS GP, no memory or DSP, and passing 50/25 MHz timing. Quartus
comparison is not implemented.

This experiment measures the checked PIN_V11 50→25 MHz integer/direct/zero-phase
profile with a falling-edge `cyclonev_clkena` whose enable register powers up
low. GPO bit 3 drives `ena`. Enable setup/hold is not characterized.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD728` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [10] | Clock-enable request echo |
| GPI [7:0] | Selected result byte |
| GPO [3] | Clock enable (`ena`) |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the enabled meter is 2047–2049. Held reset and
low startup both return count 0–2. Claim the designated kit with
`scripts/kit.py session`, load the exact OSS RBF, and run `hardware/probe.sh`.
Never take over another owner.
