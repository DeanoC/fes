# 270 mixed-duty 25/50/100 MHz PLL

`make sim EXP=270_pll_multi_duty` tests reset/relock and frequency meters on all
three outputs of one integer `altera_pll` with independent duties.
`make oss EXP=270_pll_multi_duty` produces compressed
`build/oss/270_pll_multi_duty/top.rbf`. It requires one PLL, one HPS GP, no
memory or DSP, and passing 50/25/50/100 MHz timing. Quartus comparison is not
implemented.

This experiment measures the checked PIN_V11 50 MHz → 25/50/100 MHz profile
with direct mode, zero phase and duties 25/50/25. nextpnr programs M=16 N=2
C6=16 C7=8 (400 MHz) because 100 MHz at 25% does not fit 300 MHz. GPO bits 4:3
select which meter is visible. Simulation models digital 25/50 MHz clocks and a
lock
delay; it does not reproduce the analog 100 MHz ratio or pulse width. Kit
measurement proves the frequencies, not pulse width.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD71E` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [4:3] | Meter select: 0=25 MHz, 1=50 MHz, 2=100 MHz |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

Over `2^20` reference cycles the meters are 2047–2049, 4095–4097 and
8191–8193. Held reset returns count 0. Claim the designated kit with
`scripts/kit.py session`, load the exact OSS RBF, and run `hardware/probe.sh`.
Never take over another owner.
