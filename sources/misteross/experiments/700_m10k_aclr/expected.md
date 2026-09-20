# 700 M10K asynchronous clear

`make sim EXP=700_m10k_aclr` tests INIT and independent-clock write/read on
the inferred 512-by-20 table. Simulation does not model the M10K output
register clear: locked Yosys has no `ACLR` ports on `MISTRAL_M10K`.
`make oss EXP=700_m10k_aclr` attaches GPO[5] to `ACLR1` after synthesis so
nextpnr can pack a fabric output clear. Quartus comparison is not
implemented.

Writes use the 50 MHz board clock. Reads use an independently gated 25 MHz
PLL output. GPI signature `0xD426`. Address `a` powers up as
`((a * 73) ^ (a >> 1) ^ 20'hA6)`. GPO bit 31 is write enable, bit 30 is
read enable, bit 29 gates the read clock, bit 5 is the fabric `ACLR1` net,
`[28:9]` are write data, `[28:27]` select the output window, and the RAM
address is `{GPO[8:6], 2'b00, GPO[3:0]}`. Writes are ignored until GPO
equals `0x13579BDF`.

nextpnr maps `ACLR1` onto physical `ACLR[1]` and enables the bottom
output-clear register. `ACLR0` stays omitted/`PIN_0`. Address-clear enables
stay disabled. The kit probe checks that asserting GPO[5] clears the sampled
output while leaving INIT (or a later write) intact after the clear is
released.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD426` |
| GPI [15:0] | 16-bit window of sampled `q` |
| GPO [31] | Write enable |
| GPO [30] | Read enable |
| GPO [29] | Read-clock enable |
| GPO [5] | Fabric `ACLR1` |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
