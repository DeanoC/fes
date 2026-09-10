# Expected behavior

The FPGA exposes a mixed-width M10K simple dual-port table on HPS GP: 512-by-20
writes with two independently writable 10-bit lanes, and 1024-by-10 reads.
Writes use the 50 MHz board clock. Reads use an independently gated 25 MHz PLL
output. GPI signature `0xD425`; `[9:0]` are the sampled 10-bit read lane.

Lane `a` powers up as `((a * 73) ^ (a >> 1) ^ 10'hA6)`. Address 0 is `0xA6`.
GPO bit 31 is write enable, bit 30 is read enable, bit 29 gates the read clock,
`[28:9]` are 20-bit write data, `[5:4]` are the lane mask, and the 9-bit write
address drops those mask bits. A wide write to address `w` updates 10-bit lanes
`2w` and `2w+1`. Writes are ignored until GPO equals `0x13579BDF`.

Locked Yosys does not infer mixed-width byte enables from RTL arrays, so this
experiment instantiates one `MISTRAL_M10K` with `CFG_MIXED_WIDTH=1`,
`CFG_BYTE_ENABLE=1`, write `CFG_DBITS=20`, and read `CFG_RD_DBITS=10`. nextpnr
maps the two `A1BE` bits onto `BYTEENABLEA`. DSP and MLAB remain forbidden.
Place-and-route uses router1. Quartus comparison is not implemented.

`make sim EXP=680_m10k_mix20be10` and `make oss EXP=680_m10k_mix20be10`.
