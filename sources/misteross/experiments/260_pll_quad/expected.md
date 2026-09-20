# 260 four-output 25/50/100/75 MHz PLL

`make sim EXP=260_pll_quad` tests reset/relock and frequency meters on all
four outputs of one integer `altera_pll`. `make oss EXP=260_pll_quad` produces
compressed `build/oss/260_pll_quad/top.rbf`. It requires one PLL, one HPS GP,
no memory or DSP, and passing 50/25/50/100/75 MHz timing. Quartus comparison
is not implemented.

This experiment measures the checked PIN_V11 50 MHz → 25/50/100/75 MHz
profile with direct mode, zero phase and 50% duty. nextpnr programs M=12 N=2
with C6=12, C7=6, C5=3 and C8=4. Compatible four-output combinations share one
checked 300/320/400 MHz configuration. GPO bits 4:3 select which meter is
visible. Simulation models digital 25/50 MHz clocks and a lock delay; it does
not reproduce analog 100/75 MHz ratios. Kit measurement does.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD71D` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [4:3] | Meter select: 0=25 MHz, 1=50 MHz, 2=100 MHz, 3=75 MHz |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the meters are 2047–2049, 4095–4097, 8191–8193
and 6143–6145. Held reset returns count 0. Claim the designated kit with
`scripts/kit.py session`, load the exact OSS RBF, and run `hardware/probe.sh`.
Never take over another owner.
