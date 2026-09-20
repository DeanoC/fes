# 730 TDP M10K unused-clock TCLK fold

`make sim EXP=730_m10k_tdp_tclk` instantiates `MISTRAL_M10K_TDP` with
`CLK2` and `B1EN` tied low and checks A-port INIT and write/read.
`make oss EXP=730_m10k_tdp_tclk` requires nextpnr to fold that constant
clock off `CLKIN[1]` so routing does not use the TCLK sink. Quartus
comparison is not implemented.

The live A port uses the 50 MHz board clock. GPI signature `0xD429`.
Address `a` powers up as `((a * 73) ^ (a >> 1) ^ 20'hA6)`. GPO bit 31 is
write enable, bit 30 is A-port enable, `[28:9]` are write data, `[28:27]`
select the output window, and `[8:0]` are the A-port address. Writes are
ignored until GPO equals `0x13579BDF`.

The kit probe checks INIT and a write on the live A port. It does not
measure the unused B port.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD429` |
| GPI [15:0] | 16-bit window of sampled `A1Q` |
| GPO [31] | A-port write enable |
| GPO [30] | A-port enable |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
