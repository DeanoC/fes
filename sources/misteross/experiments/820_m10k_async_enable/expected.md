# 820 asynchronous M10K read with constant-high enable

`make sim EXP=820_m10k_async_enable` checks INIT and a write through a
combinational `B1DATA` stand-in. `make oss EXP=820_m10k_async_enable`
instantiates `MISTRAL_M10K` with Yosys's clocked `B1EN` port, then sets
`CFG_ASYNC_READ` and drops `B1EN`/`CLK2`. The packer routes that omitted
enable as constant-high `ENABLE[0]`. Quartus comparison is not implemented.

There is no second clock and no read enable. GPO bits [3:0] plus [8:6]
select the table address. Bit 31 writes after the arm token
`0x13579BDF`. GPI signature `0xD42F`. The low 16 bits of GPI are the
sampled combinational read. The 1.5 ns host timing arc is an estimate,
not silicon characterization.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
