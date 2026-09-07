# 310 100 MHz-reference 25/50/100 MHz PLL

`make sim EXP=310_pll_ref100` tests reset/relock and frequency meters on the
25/50/100 MHz outputs of one integer `altera_pll` whose reference parameter is
100 MHz. `make oss EXP=310_pll_ref100` produces compressed
`build/oss/310_pll_ref100/top.rbf`. It requires one PLL, one HPS GP, no memory
or DSP, and passing 100/25/50/100 MHz timing. Quartus comparison is not
implemented.

This experiment packs the checked PIN_V11 100 MHz → 25/50/100 MHz 50% profile
with direct mode and zero phase. nextpnr programs the 300 MHz configuration
(M=6 N=2). The port name `FPGA_CLK1_50` is retained; the experiment SDC period
is 10 ns. Simulation treats the pin as 100 MHz. The DE10-Nano onboard
oscillator is 50 MHz; analog kit proof needs an external 100 MHz V11 clock.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD722` |
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

Over `2^20` 100 MHz reference cycles the meters are 1023–1025, 2047–2049 and
4095–4097. Held reset returns count 0. Claim the designated kit with
`scripts/kit.py session` only when the physical V11 clock is 100 MHz. Never
take over another owner.
