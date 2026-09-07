# 300 25 MHz-reference 25/50/100 MHz PLL

`make sim EXP=300_pll_ref25` tests reset/relock and frequency meters on the
25/50/100 MHz outputs of one integer `altera_pll` whose reference parameter is
25 MHz. `make oss EXP=300_pll_ref25` produces compressed
`build/oss/300_pll_ref25/top.rbf`. It requires one PLL, one HPS GP, no memory
or DSP, and passing 25/25/50/100 MHz timing. Quartus comparison is not
implemented.

This experiment packs the checked PIN_V11 25 MHz → 25/50/100 MHz 50% profile
with direct mode and zero phase. nextpnr programs the 300 MHz configuration
(M=24 N=2). The port name `FPGA_CLK1_50` is retained; the experiment SDC period
is 40 ns. Simulation treats the pin as 25 MHz and uses a digital 25 MHz
stand-in for 50/100 MHz. The DE10-Nano onboard oscillator is 50 MHz; analog
kit proof needs an external 25 MHz V11 clock.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD721` |
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

Over `2^20` 25 MHz reference cycles the meters are 4095–4097, 8191–8193 and
16383–16385. Held reset returns count 0. Claim the designated kit with
`scripts/kit.py session` only when the physical V11 clock is 25 MHz. Never take
over another owner.
