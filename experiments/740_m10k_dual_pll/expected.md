# 740 dual-PLL M10K router2 retry

`make sim EXP=740_m10k_dual_pll` tests INIT and independent-clock write/read
on a 512-by-20 table plus a second 74.25 MHz PLL. `make oss EXP=740_m10k_dual_pll`
uses default router2. When the packed netlist has two `altera_pll` cells and
one `MISTRAL_M10K`, nextpnr retries ordinary nets with router1 if any
constrained clock has less than 10% margin. Quartus comparison is not
implemented.

Writes use the 50 MHz board clock. Reads use an independently gated 25 MHz
PLL output. The second PLL produces 74.25 MHz. GPI signature `0xD42A`.
Address `a` powers up as `((a * 73) ^ (a >> 1) ^ 20'hA6)`. GPO bit 31 is
write enable, bit 30 is read enable, bit 29 gates the read clock, `[28:9]`
are write data, `[28:27]` select the output window, and `[8:0]` are the
address. Writes are ignored until GPO equals `0x13579BDF`.

The kit probe checks INIT and a write. It does not prove the router1 retry
fired; that is a nextpnr log observation.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD42A` |
| GPI [15:0] | 16-bit window of sampled `q` |
| GPO [31] | Write enable |
| GPO [30] | Read enable |
| GPO [29] | Read-clock enable |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
