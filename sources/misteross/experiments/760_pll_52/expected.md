# 760 52 MHz PLL clock measurement

`make sim EXP=760_pll_52` tests the HPS protocol with a digital toggling
stand-in (not the analog 50-to-52 MHz ratio) and reuses the 090 meter
checks. `make oss EXP=760_pll_52` produces a compressed
`build/oss/760_pll_52/top.rbf`. It requires one `altera_pll`, one HPS GP
interface, no memory or DSP, and passing 50 MHz reference and 52 MHz output
timing. Quartus comparison is not implemented.

The supported hardware profile is PIN_V11 50 MHz → 52 MHz through the
520 MHz feedback profile (M=52/N=5, C6=10), direct mode, integer operation,
zero phase, 50% duty, reset held low. Simulation does not model analog lock
behavior. This test does not characterize jitter or reset/relock.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD752` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [7:0] | Selected result byte |
| GPO [1] | Request toggle |
| GPO [0] | Select high byte when set |

The result counts rising edges of the PLL clock divided by 256 over 2^20
reference cycles (about 20.97 ms). Hardware acceptance is **4260 ±2**
(52 × 4096 / 50 = 4259.84), lock=1, sampled lock loss=0 on three successive
measurements.

Claim the designated kit using the current `scripts/kit.py session`, load the
exact OSS RBF, and run `hardware/probe.sh` on that target while holding the lease.
Never take over another owner.
