# 880 native 1024x10 asynchronous M10K ROM

`make sim EXP=880_m10k_async_rom` checks INIT on a 1024-by-10 combinational
B-port. `make oss EXP=880_m10k_async_rom` requires nextpnr to borrow a live
design clock for the folded write clock, route both CLKIN sinks, and
program `TOP_CE0_SEL` without `CFG_BYTE_ENABLE`. Quartus comparison is not
implemented.

GPO `[9:0]` is the read address. GPI signature `0xD880`.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
