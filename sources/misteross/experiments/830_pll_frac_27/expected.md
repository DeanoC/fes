# 830 calculated 27 MHz fractional-N PLL

`make sim EXP=830_pll_frac_27` tests the HPS protocol with a digital toggling
stand-in (not the analog 50-to-27 MHz ratio) and reuses the 090 meter
checks. `make oss EXP=830_pll_frac_27` produces a compressed
`build/oss/830_pll_frac_27/top.rbf`. It requires one `altera_pll`, one HPS GP
interface, no memory or DSP, and passing 50 MHz reference and 27 MHz output
timing. Quartus comparison is not implemented.

The supported hardware profile is PIN_V11 50 MHz → 27 MHz through the
bounded fractional-N calculator (reported VCO 400–500 MHz), direct mode,
zero phase, 50% duty, reset held low. Simulation does not model analog lock
behavior. This test does not characterize jitter or reset/relock.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD827` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [7:0] | Selected result byte |
| GPO [1] | Request toggle |
| GPO [0] | Select high byte when set |

The result counts rising edges of the PLL clock divided by 256 over 2^20
reference cycles (about 20.97 ms). Hardware acceptance is **2212 ±2**
(27 × 4096 / 50 = 2211.84), lock=1, sampled lock loss=0 on three successive
measurements.

Claim the designated kit using the current `scripts/kit.py session`, load the
exact OSS RBF, and run `hardware/probe.sh` on that target while holding the lease.
Never take over another owner.
