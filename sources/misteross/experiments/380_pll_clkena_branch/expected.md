# 380 running and gated PLL clock branches

`make sim EXP=380_pll_clkena_branch` tests reset/relock on an always-running
25 MHz PLL output and a separately gated branch of the same counter.
`make oss EXP=380_pll_clkena_branch` produces compressed
`build/oss/380_pll_clkena_branch/top.rbf`. It requires one PLL, one clock-enable
primitive, one HPS GP, no memory or DSP, and passing 50/25/25 MHz timing.
Quartus comparison is not implemented.

This experiment measures the checked PIN_V11 50→25 MHz profile with a
falling-edge `cyclonev_clkena` that powers up low. The ungated PLL output keeps
running while the gated branch stops. GPO bit 4 selects the meter; GPO bit 3
drives `ena`. Enable setup/hold is not characterized.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD729` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [10] | Clock-enable request echo |
| GPI [7:0] | Selected result byte |
| GPO [4] | Meter select: 0=running, 1=gated |
| GPO [3] | Clock enable (`ena`) |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the running meter is 2047–2049 whether or not the
gate is enabled. The gated meter is 2047–2049 when enabled and 0–2 when
disabled. Held reset returns count 0. Claim the designated kit with
`scripts/kit.py session`, load the exact OSS RBF, and run `hardware/probe.sh`.
Never take over another owner.
