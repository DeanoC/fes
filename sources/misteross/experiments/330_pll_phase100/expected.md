# 330 four-output 100 MHz quadrature PLL

`make sim EXP=330_pll_phase100` tests reset/relock and frequency meters on four
100 MHz outputs of one integer `altera_pll` at 0°/90°/180°/270°.
`make oss EXP=330_pll_phase100` produces compressed
`build/oss/330_pll_phase100/top.rbf`. It requires one PLL, one HPS GP, no
memory or DSP, and passing 50/100/100/100/100 MHz timing. Quartus comparison is
not implemented.

This experiment measures the checked PIN_V11 50 MHz → four 100 MHz 50% outputs
with `phase_shift` `0 ps` / `2500 ps` / `5000 ps` / `7500 ps`. GPO bits 4:3
select which meter is visible. Simulation models digital 50 MHz stand-ins for
the 100 MHz outputs; 90°/270° are also stand-ins. Kit measurement proves four
100 MHz clocks exist, not analog phase accuracy.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD724` |
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

Over `2^20` reference cycles each kit meter is 8191–8193. Simulation expects
4095–4097 because the 50 MHz-stepped model cannot form 100 MHz. Held reset
returns count 0. Claim the designated kit with `scripts/kit.py session`, load
the exact OSS RBF, and run `hardware/probe.sh`. Never take over another owner.
