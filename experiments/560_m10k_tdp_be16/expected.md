# Expected behavior

The FPGA exposes a 512-by-16 M10K true dual-port table with two 8-bit write
bytes on HPS GP, padded into one physical 512-by-20 block. Port A uses the
50 MHz board clock. Port B uses an independently gated 25 MHz PLL output.
GPI signature `0xD420`; `[15:0]` are a 16-bit window of the selected port.

Address `a` powers up as `((a * 73) ^ (a >> 1) ^ 16'hA6)`. Address 0 is
`0xA6`. GPO bits 31/30 write ports B/A, bits 29/28 enable ports B/A, bit 27
selects the output window, bit 26 selects port B, `[25:16]` are the 10-bit
seed, bit 14 gates the port-B clock, bits `[13:12]`/`[11:10]` are the B/A
byte masks, and `[8:0]` are the address. Port A stores `{6'd0, seed} ^
16'h3a00`; port B stores `{6'd0, seed} ^ 16'hbc00`. A write updates only the
masked bytes. The sampled output holds during writes. Writes are ignored
until GPO equals `0x13579BDF`. Same-word cross-port access involving a write
is undefined.

Yosys maps the tagged `ram_style="m10k_tdp_byte"` table to one
`MISTRAL_M10K_TDP` with `CFG_BYTE_ENABLE=1`, `CFG_ABITS=9`, and `CFG_DBITS=20`.
nextpnr-mistral packs it as one `MISTRAL_M10K` with `CFG_TDP=1`. OSS does not
edit those parameters. DSP and MLAB remain forbidden. Quartus comparison is
not implemented.

`make sim EXP=560_m10k_tdp_be16` and `make oss EXP=560_m10k_tdp_be16`.
