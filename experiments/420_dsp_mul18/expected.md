# Expected behavior

The FPGA exposes a sixteen-by-sixteen unsigned product on HPS GP. GPO `[15:0]`
is the left operand and `[31:16]` is the right operand. After one 50 MHz clock,
GPI `[31:16]` stays `0xD612` and `[15:0]` are the low 16 bits of the 32-bit
product. `0xFFFF * 0xFFFF` is `0xFFFE0001`, so the low half is `0x0001`.

Yosys maps the multiply to one `MISTRAL_MUL18X18`. nextpnr-mistral places one
wide DSP BEL in `M18X18P36` mode. M9, M27, memory, and PLL stay unused.
Quartus comparison is not implemented.

`make sim EXP=420_dsp_mul18` and `make oss EXP=420_dsp_mul18`.
