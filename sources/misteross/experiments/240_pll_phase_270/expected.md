# 240 static 0°/270° 25 MHz PLL

`make sim EXP=240_pll_phase_270` tests ten reset/relock cycles of a dual 25 MHz
PLL with `phase_shift1("30000 ps")`. `make oss EXP=240_pll_phase_270` produces
compressed `build/oss/240_pll_phase_270/top.rbf`. It requires one PLL, one HPS
GP, no memory or DSP, and passing 50/25/25 MHz timing. Quartus comparison is
not implemented.

This experiment programs the Quartus-checked M=12 N=2 C12 pair with C7
`CNT_PRESET=10`. Output 1 lags output 0 by 30 ns. GPO bit0 is launched on the 0°
rising edge and captured on the 270° rising edge. Each output keeps a same-clock
toggle so nextpnr reports both 25 MHz clocks in fmax (`meter.testclk` for 0° and
`phase270`). The 0° meter cannot observe analog phase accuracy. Simulation uses
two digital 25 MHz clocks 30 ns apart, not analog phase. Other shifts and
non-50% duty remain rejected.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD71B` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [9] | Launch FF on the 0° clock |
| GPI [8] | Capture FF on the 270° clock |
| GPI [7:0] | Selected result byte |
| GPO [4] | Select high byte when set |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Launch data |

Over `2^20` reference cycles the 0° meter is 2047–2049. Held reset returns
count 0. Launch and 270° capture both follow GPO [0]. Claim the designated kit
with `scripts/kit.py session`, load the exact OSS RBF, and run
`hardware/probe.sh`. Never take over another owner. This does not measure analog
phase accuracy.
