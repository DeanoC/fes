# 120 PLL-clocked DSP product

`make sim EXP=120_pll_dsp` tests HPS peek/poke of an eight-by-eight unsigned
product computed on the proven 50-to-25 MHz PLL output. `make oss EXP=120_pll_dsp`
produces compressed `build/oss/120_pll_dsp/top.rbf`. It requires one `altera_pll`,
one `MISTRAL_MUL9X9`, one HPS GP interface, no memory, and passing 50/25 MHz
timing. Quartus comparison is not implemented.

The PLL profile is the same as `090_pll_clock`: PIN_V11 50 MHz → 25 MHz, direct
mode, integer operation, zero phase, 50% duty, reset held low. Operands cross
from the 50 MHz HPS domain into `clk25`; the product crosses back. Hold GPO
operands until both GPI bytes have been read. Simulation models only the
digital 2:1 ratio and treats lock as the inverse of reset.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xDC10` |
| GPI [13] | Synchronized PLL lock |
| GPI [7:0] | Selected product byte |
| GPO [16] | Select high product byte when set |
| GPO [15:8] | Right operand |
| GPO [7:0] | Left operand |

GPI/GPO are FPGA-manager words `0xFF706014` / `0xFF706010`. Wait until lock=1,
write both operands, wait about 1 ms, then read the low byte and the high byte.
`0x0A * 0x0C` is `0x0078`; `0xFF * 0xFF` is `0xFE01`. This experiment does not
measure frequency, reset/relock, or analog lock time.

Claim the designated kit using the current `scripts/kit.py session`, load the
exact OSS RBF, and run `hardware/probe.sh` on that target while holding the lease.
The probe only reads/writes the GP registers; it neither claims nor programs the
kit. Finish using the session's `stop` command and release it. Never take over
another owner or stop an unrelated game.
