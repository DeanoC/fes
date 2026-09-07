# 280 four-output 25 MHz quadrature PLL

`make sim EXP=280_pll_quadrature` tests reset/relock and frequency meters on
four 25 MHz outputs of one integer `altera_pll` at 0°/90°/180°/270°.
`make oss EXP=280_pll_quadrature` produces compressed
`build/oss/280_pll_quadrature/top.rbf`. It requires one PLL, one HPS GP, no
memory or DSP, and passing 50/25/25/25/25 MHz timing. Quartus comparison is not
implemented.

This experiment measures the checked PIN_V11 50 MHz → four 25 MHz 50% outputs
with `phase_shift` `0 ps` / `10000 ps` / `20000 ps` / `30000 ps`. nextpnr
programs M=12 N=2 with C6/C7/C5/C8 all dividing by 12. GPO bits 4:3 select
which meter is visible. Simulation models digital 25 MHz quadrature from 50 MHz
edges; it does not reproduce analog phase. Kit measurement proves four 25 MHz
clocks exist, not analog phase accuracy.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD71F` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [4:3] | Meter select: 0=0°, 1=90°, 2=180°, 3=270° |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles each meter is 2047–2049. Held reset returns count
0. Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
