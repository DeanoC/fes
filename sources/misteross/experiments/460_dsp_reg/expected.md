# Expected behavior

The FPGA exposes an eight-by-eight unsigned product on HPS GP through an M18
DSP with input and output registers enabled. GPO layout matches `060_dsp_mul`.
GPI signature `0xD616`. The DSP pipeline plus the fabric byte register means
the selected product is stable a few 50 MHz clocks after the operands change;
the kit wait is long enough.

OSS `chtype`s `dsp18_reg` to `MISTRAL_MUL18X18` with `INREG_CTRL_AX`,
`INREG_CTRL_AY`, and `OREG_CTRL` enabled. Clock comes from `FPGA_CLK1_50`.
Enable and asynchronous clear are omitted: nextpnr keeps the registers enabled
and unused ACLR low. Simulation checks the two-stage DSP pipeline plus the
fabric byte register. Quartus comparison is not implemented.

`make sim EXP=460_dsp_reg` and `make oss EXP=460_dsp_reg`.
