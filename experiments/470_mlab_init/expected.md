# Expected behavior

The FPGA exposes a 32-by-8 writeable MLAB table on HPS GP with preserved
power-up contents. GPO layout matches `040_mlab_ram`: bits `[4:0]` are the
index, `[15:8]` are write data, and bit 16 is write enable. GPI signature
`0xD417`; `[7:0]` are the registered read byte.

Address `a` powers up as `((a * 73) ^ (a >> 1) ^ 8'hA6)`. Address 0 is
`0xA6`. After a completed write to an even index, that index holds the new
byte and odd indexes keep their initial values.

Yosys still emits eight `MISTRAL_MLAB` cells without INIT, so the power-up
loop is simulation-only. OSS writes the 32-bit lane INIT parameters onto those
cells after synthesis. nextpnr-mistral encodes them into the MLAB LUT_MASK.
Quartus comparison is not implemented.

`make sim EXP=470_mlab_init` and `make oss EXP=470_mlab_init`.
