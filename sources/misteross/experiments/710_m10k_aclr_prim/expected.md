# 710 M10K primitive asynchronous clear

`make sim EXP=710_m10k_aclr_prim` instantiates `MISTRAL_M10K` with
connected `ACLR1` and a digital stand-in that clears the registered read
output. `make oss EXP=710_m10k_aclr_prim` requires Yosys to emit `ACLR1`
as a JSON input; it does not patch that port after synthesis. Quartus
comparison is not implemented.

Writes use the 50 MHz board clock. Reads use an independently gated 25 MHz
PLL output. GPI signature `0xD427`. Address `a` powers up as
`((a * 73) ^ (a >> 1) ^ 20'hA6)`. GPO bit 31 is write enable, bit 30 is
read enable, bit 29 gates the read clock, bit 5 is `ACLR1`, `[28:9]` are
write data, `[28:27]` select the output window, and the table address is
`{GPO[8:6], 2'b00, GPO[3:0]}`. Writes are ignored until GPO equals
`0x13579BDF`. `ACLR0` is tied low.

The kit probe checks INIT, output-register clear on GPO[5], restored
memory contents after release, a post-clear write, and a second clear.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD427` |
| GPI [15:0] | 16-bit window of sampled `q` |
| GPO [31] | Write enable |
| GPO [30] | Read enable |
| GPO [29] | Read-clock enable |
| GPO [5] | Primitive `ACLR1` |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
