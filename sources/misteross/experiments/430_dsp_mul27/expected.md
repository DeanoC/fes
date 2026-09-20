# Expected behavior

The FPGA exposes a twenty-by-eight unsigned product on HPS GP. GPO `[19:0]` is
the left operand and `[27:20]` is the right operand. After one 50 MHz clock,
GPI `[31:16]` stays `0xD613` and `[15:0]` are the low 16 bits of the 28-bit
product.

Yosys maps the multiply to one `MISTRAL_MUL27X27`. nextpnr-mistral places one
wide DSP BEL in `M27X27` mode. Omitted DSP controls encode low, so a native
multiply is not negated. The logical 54-bit result still has the physical hole
at RESULT bit 36; this experiment only reads bits `[15:0]`. M9, M18, memory,
and PLL stay unused. Quartus comparison is not implemented.

`make sim EXP=430_dsp_mul27` and `make oss EXP=430_dsp_mul27`.
