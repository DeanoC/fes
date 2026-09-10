# 720 inferred M10K asynchronous clear

`make sim EXP=720_m10k_aclr_infer` tests INIT, write/read, and an
asynchronous zero clear of the registered read output in the inferred
512-by-20 table. `make oss EXP=720_m10k_aclr_infer` requires Yosys to map
that reset onto `ACLR1` of `MISTRAL_M10K`. OSS does not patch that port.
Quartus comparison is not implemented.

Writes use the 50 MHz board clock. Reads use an independently gated 25 MHz
PLL output. GPI signature `0xD428`. Address `a` powers up as
`((a * 73) ^ (a >> 1) ^ 20'hA6)`. GPO bit 31 is write enable, bit 30 is
read enable, bit 29 gates the read clock, bit 5 is the inferred `ACLR1`
net, `[28:9]` are write data, `[28:27]` select the output window, and the
table address is `{GPO[8:6], 2'b00, GPO[3:0]}`. Writes are ignored until
GPO equals `0x13579BDF`.

The kit probe checks INIT, output-register clear on GPO[5], restored
memory contents after release, a post-clear write, and a second clear.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD428` |
| GPI [15:0] | 16-bit window of sampled `q` |
| GPO [31] | Write enable |
| GPO [30] | Read enable |
| GPO [29] | Read-clock enable |
| GPO [5] | Inferred `ACLR1` |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
