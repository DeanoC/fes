# Expected behavior

The FPGA exposes a mixed-width M10K simple dual-port table on HPS GP: 256-by-40
writes and 1024-by-10 reads. Writes use the 50 MHz board clock. Reads use an
independently gated 25 MHz PLL output. GPI signature `0xD41B`; `[15:0]` are a
16-bit window of the sampled 10-bit read word.

Each 10-bit lane `a` powers up as `((a * 73) ^ (a >> 1) ^ 10'hA6)`, so address 0
is `0xA6`. GPO bit 31 is write enable, bit 30 is read enable, bit 29 gates the
read clock, `[27:26]` select the output window, `[25:16]` are the 10-bit write
seed, and `[7:0]` are the wide write address. A write stores
`seed ^ (lane * 10'h093)` into the four 10-bit lanes of that word. Writes are
ignored until GPO equals `0x13579BDF`.

Yosys maps the tagged `ram_style="m10k_mixed"` table to one `MISTRAL_M10K` with
`CFG_MIXED_WIDTH=1`, `CFG_DUAL_CLOCK=1`, write `CFG_DBITS=40`, and read
`CFG_RD_DBITS=10`. OSS does not edit those parameters. Place-and-route uses
router1. DSP and MLAB remain forbidden. Quartus comparison is not implemented.

`make sim EXP=510_m10k_mix40r10` and `make oss EXP=510_m10k_mix40r10`.
