# Expected behavior

The FPGA exposes a 512-by-20 M10K simple dual-port table on HPS GP with two
independently writable 10-bit lanes. Writes use the 50 MHz board clock. Reads
use an independently gated 25 MHz PLL output. GPI signature `0xD41A`; `[15:0]`
are a 16-bit window of the sampled read word.

Address `a` powers up as `((a * 73) ^ (a >> 1) ^ 20'hA6)`. Address 0 is
`0xA6`. GPO bit 31 is write enable, bit 30 is read enable, bit 29 gates the
read clock, `[28:9]` are write data, `[28:27]` select the output window,
`[5:4]` are the lane mask, and the write address drops those mask bits.
Writes are ignored until GPO equals `0x13579BDF`.

Yosys maps the table to one `MISTRAL_M10K` with `CFG_BYTE_ENABLE=1`,
`CFG_DUAL_CLOCK=1`, write `CLK1`, read `CLK2`, and two `A1BE` lanes. OSS does
not edit those parameters. DSP and MLAB remain forbidden. Quartus comparison
is not implemented.

`make sim EXP=500_m10k_be20` and `make oss EXP=500_m10k_be20`.
