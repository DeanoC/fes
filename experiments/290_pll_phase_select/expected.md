# 290 selectable 0/180/180/0 25 MHz PLL

`make sim EXP=290_pll_phase_select` tests reset/relock and frequency meters on
four 25 MHz outputs of one integer `altera_pll` at 0°/180°/180°/0°.
`make oss EXP=290_pll_phase_select` produces compressed
`build/oss/290_pll_phase_select/top.rbf`. It requires one PLL, one HPS GP, no
memory or DSP, and passing 50/25/25/25/25 MHz timing. Quartus comparison is not
implemented.

This experiment measures a checked PIN_V11 50 MHz four-output 25 MHz 50%
profile with repeated 180° shifts (`0 ps` / `20000 ps` / `20000 ps` / `0 ps`).
Output 0 stays at 0°. GPO bits 4:3 select which meter is visible. Simulation
models digital 25 MHz 0/180 repeats from 50 MHz edges; it does not reproduce
analog phase. Kit measurement proves four 25 MHz clocks exist, not analog
phase accuracy.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD720` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [4:3] | Meter select: 0=0°, 1=180°, 2=180°, 3=0° |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles each meter is 2047–2049. Held reset returns count
0. Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
