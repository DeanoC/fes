# Expected behavior

The FPGA exposes a 512-by-20 M10K true dual-port table on HPS GP. Port A uses
the 50 MHz board clock. Port B uses an independently gated 25 MHz PLL output.
GPI signature `0xD41E`; `[15:0]` are a 16-bit window of the selected port.

Address `a` powers up as `((a * 73) ^ (a >> 1) ^ 20'hA6)`. Address 0 is
`0xA6`. GPO bit 31 writes port B, bit 30 writes port A, bits 29/28 enable
ports B/A, bit 27 selects the output window, bit 26 selects port B, `[25:16]`
are the 10-bit seed, bit 14 gates the port-B clock, and `[8:0]` are the
address. Port A stores `{10'd0, seed} ^ 20'h93a00`; port B stores
`{10'd0, seed} ^ 20'h2bc00`. Writes are ignored until GPO equals
`0x13579BDF`. Same-word cross-port access involving a write is undefined.

Yosys maps the tagged `ram_style="m10k_tdp"` table to one `MISTRAL_M10K_TDP`
with `CFG_ABITS=9` and `CFG_DBITS=20`. nextpnr-mistral packs it as one
`MISTRAL_M10K` with `CFG_TDP=1`. OSS does not edit those parameters. DSP and
MLAB remain forbidden. Quartus comparison is not implemented.

`make sim EXP=540_m10k_tdp20` and `make oss EXP=540_m10k_tdp20`.
