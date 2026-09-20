# Expected behavior

The FPGA exposes an eight-by-eight unsigned product with the M9 preadder
enabled and subtract selected: `left * (right - preadd)`. GPO `[7:0]` is
left, `[15:8]` is right, `[23:16]` is the preadd operand, and bit 24 selects
the high product byte. GPI signature `0xD614`.

`10 * (12 - 2)` is `0x0064`, which is distinct from `10 * 12 = 0x0078`.

OSS synthesis keeps a blackbox `dsp9_preadder` cell, then `chtype`s it to
`MISTRAL_MUL9X9` with `PREADDER_EN`/`PREADDER_SUB`. nextpnr-mistral programs
one M9 DSP. Quartus comparison is not implemented.

`make sim EXP=440_dsp_preadder` and `make oss EXP=440_dsp_preadder`.
