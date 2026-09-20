# Expected behavior

The FPGA exposes a 256-by-40 M10K simple dual-port table on HPS GP. Writes
use the 50 MHz board clock. Reads use an independently gated 25 MHz PLL
output. GPI signature `0xD419`; `[15:0]` are a 16-bit window of the sampled
read word. The stored high 20 bits are the complement of the low 20 bits.

Address `a` powers up as `{ ~low, low }` where
`low = ((a * 73) ^ (a >> 1) ^ 20'hA6)`. Address 0 low half is `0xA6`. GPO
bit 31 is write enable, bit 30 is read enable, bit 29 gates the read clock,
`[28:9]` are the low 20 write bits, `[28:27]` select the output window, and
the low bits are the address. Writes are ignored until GPO equals
`0x13579BDF` so the loader SPI probe cannot corrupt INIT.

Yosys maps the table to one `MISTRAL_M10K` with `CFG_DUAL_CLOCK=1`, write
`CLK1`, read `CLK2`, and numeric INIT. nextpnr-mistral packs CLK2 onto
CLKIN.1 and keeps the 40-bit input half on the write clock. OSS does not
edit those parameters. DSP and MLAB remain forbidden. Quartus comparison is
not implemented.

`make sim EXP=490_m10k_sdp40` and `make oss EXP=490_m10k_sdp40`.
