# 870 narrow true-dual-port M10K

`make sim EXP=870_m10k_narrow` checks logical INIT and an A-port write on
an 8192-by-1 `MISTRAL_M10K_TDP`. `make oss EXP=870_m10k_narrow` requires
nextpnr to pack that native narrow geometry. The kit probe writes then
reads because the physical 8192x1 INIT order is not the logical address
map. Locked Yosys has no matching inference rule. Quartus comparison is
not implemented.

GPO `[12:0]` is the address, bit 13 is write data, bit 30 enables the A
port, and bit 31 writes after `0x13579BDF`. GPI signature `0xD870`.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
