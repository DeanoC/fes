# 110 PLL reset and relock

`make sim EXP=110_pll_reset` tests ten reset/relock cycles with production-length
measurement windows and reuses the meter fault tests. `make oss EXP=110_pll_reset`
produces compressed `build/oss/110_pll_reset/top.rbf` using the authenticated
reset-capable nextpnr pin. Quartus comparison is not implemented.

The PLL remains the fixed PIN_V11 50→25 MHz integer/direct/zero-phase/50%-duty
profile. Exactly one PLL and one HPS GP interface are required, with no memory
or DSP. Both clock domains must meet their 50/25 MHz constraints.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD712` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [11] | Applied reference-domain PLL reset |
| GPI [7:0] | Selected result byte |
| GPO [2] | Active-high PLL reset request |
| GPO [1] | Measurement request toggle |
| GPO [0] | Select high byte when set |

GPI/GPO addresses are FPGA-manager words `0xFF706014` / `0xFF706010`.
Reset passes through two 50 MHz registers before driving the PLL. Hold the reset
request until its echo and lock status agree. The reference clock and meter
remain operational during reset. Do not issue a new measurement while busy.

After claiming the designated kit with the current `scripts/kit.py session`
and loading the exact RBF, run `hardware/probe.sh` on the target under that
lease. It performs ten cycles, each requiring:

1. Reset asserted: echo=1, lock=0, completed measurement=0, sampled lock loss=1.
2. Reset released: echo=0, lock=1, completed measurement=2048 ±1, sampled lock loss=0.

The count measures the output divided by 256 over 2^20 reference cycles. The
probe validates both result bytes against one stable snapshot and preserves
unrelated GPO bits. It never programs hardware or claims ownership. End the
session with `stop` and `release`; current `kit.py` handles development reboot
recovery when required. Do not take over another owner's kit or game.

The simulation models only a digital divider and lock delay. Hardware checks
establish functional reset/relock, not analog acquisition time, jitter,
minimum reset pulse width, or reset recovery/removal timing.
