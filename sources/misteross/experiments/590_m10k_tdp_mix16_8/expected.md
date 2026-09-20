# Expected behavior

The FPGA exposes a padded mixed-width M10K true dual-port table on HPS GP:
512-by-16 port A and 1024-by-8 port B. Port A uses
the 50 MHz board clock. Port B uses an independently gated 25 MHz PLL output.
GPI signature `0xD423`; `[15:0]` are a 16-bit window of the selected
port.

Canonical 10-bit lane `a` powers up as `((a * 73) ^ (a >> 1) ^ 10'hA6)` in the
low 8 bits. Address 0 is `0xA6`. GPO bits 31/30 write ports B/A, bits 29/28
enable ports B/A, bit 27 selects the output window, bit 26 selects port B,
`[25:16]` are the lane seed, bit 14 gates the port-B clock, and `[9:0]` are the
address. A write returns NEW_DATA on that port. A narrow write preserves the
neighboring lane of a wide word. Writes are ignored until GPO equals
`0x13579BDF`. Cross-port accesses that overlap physical storage and include a
write are undefined.

Yosys maps the tagged `ram_style="m10k_tdp_mixed"` table to one
`MISTRAL_M10K_TDP` with `CFG_MIXED_WIDTH=1`, `CFG_ABITS`/`CFG_DBITS` 9/20
and `CFG_RD_ABITS`/`CFG_RD_DBITS` 10/10. nextpnr-mistral
packs it as one `MISTRAL_M10K` with `CFG_TDP=1`. OSS does not edit those
parameters. DSP and MLAB remain forbidden. Quartus comparison is not
implemented.

`make sim EXP=590_m10k_tdp_mix16_8` and `make oss EXP=590_m10k_tdp_mix16_8`.
