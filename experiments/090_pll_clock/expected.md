# 090 PLL clock measurement

`make sim EXP=090_pll_clock` tests the HPS protocol at the production measurement
window and tests the meter independently with correct, stopped, and doubled
clocks, repeated requests, sampled lock loss, and stable snapshots.
`make oss EXP=090_pll_clock` produces a compressed `build/oss/090_pll_clock/top.rbf`.
It requires one `altera_pll`, one HPS GP interface, no memory or DSP, and passing
50 MHz reference and 25 MHz output timing. Quartus comparison is not implemented
for this experiment.

The supported hardware profile is PIN_V11 50 MHz → 25 MHz, direct mode, integer
operation, zero phase, 50% duty, reset held low. Simulation does not model analog
lock behavior. This test does not characterize jitter or reset/relock.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD711` |
| GPI [15] | Measurement busy |
| GPI [14] | Completed request toggle |
| GPI [13] | Synchronized PLL lock |
| GPI [12] | Lock loss sampled during measurement |
| GPI [7:0] | Selected result byte |
| GPO [1] | Request toggle |
| GPO [0] | Select high byte when set |

GPI/GPO are FPGA-manager words `0xFF706014` / `0xFF706010`. Set request opposite
the current completed toggle, wait for busy=0 and completed=request, then read
both bytes. Only one request may be outstanding. Initial completed=0 is not a
completed measurement. Unrelated GPO bits are ignored and preserved by the probe.

The result counts rising edges of the PLL clock divided by 256 over 2^20
reference cycles (about 20.97 ms). Acceptance is **2048 ±1**, lock=1, sampled
lock loss=0 on three successive measurements. A stopped clock returns zero;
a doubled clock returns approximately 4096.

Claim the designated kit using the current `scripts/kit.py session`, load the
exact OSS RBF, and run `hardware/probe.sh` on that target while holding the lease.
The probe only reads/writes the GP registers; it neither claims nor programs the
kit. Finish using the session's `stop` command and release it. The client handles
the Stop/reboot protocol if runtime recovery requires a reboot. Never take over
another owner or stop an unrelated game. Building alone does not program hardware.
