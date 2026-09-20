# 320 four-output 50 MHz quadrature PLL

`make sim EXP=320_pll_phase50` tests reset/relock and frequency meters on four
50 MHz outputs of one integer `altera_pll` at 0°/90°/180°/270°.
`make oss EXP=320_pll_phase50` produces compressed
`build/oss/320_pll_phase50/top.rbf`. It requires one PLL, one HPS GP, no
memory or DSP, and passing 50/50/50/50/50 MHz timing. Quartus comparison is
not implemented.

This experiment measures the checked PIN_V11 50 MHz → four 50 MHz 50% outputs
with `phase_shift` `0 ps` / `5000 ps` / `10000 ps` / `15000 ps`. 90° and 270°
use the phase mux as well as the counter preset. GPO bits 4:3 select which
meter is visible. Simulation models digital 50 MHz clocks; 90°/270° are
stand-ins. Kit measurement proves four 50 MHz clocks exist, not analog phase
accuracy.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD723` |
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

Over `2^20` reference cycles each meter is 4095–4097. Held reset returns count
0. Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
