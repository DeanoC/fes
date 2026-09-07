# Expected behavior

The FPGA exposes an eight-by-eight unsigned M18 product plus the 36-bit C
addend on HPS GP. GPO `[7:0]` is left, `[15:8]` is right, and `[31:16]` is
the addend. GPI signature `0xD615`; `[15:0]` are the low 16 bits of the 36-bit
result. `10 * 12 + 5` is `125`.

OSS `chtype`s a blackbox `dsp18_mac` cell to `MISTRAL_MUL18X18`. nextpnr-mistral
places one `M18X18P36` DSP and maps C on BX groups `{8,9,6,7}`. Cascade stays
disabled. Simulation and kit both check `A*B+C`. Quartus comparison is not
implemented.

`make sim EXP=450_dsp_mac` and `make oss EXP=450_dsp_mac`.
