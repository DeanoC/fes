# 790 M10K address stall

`make sim EXP=790_m10k_addrstall` checks INIT and that `ADDRSTALLA`
holds the A-port address. `make oss EXP=790_m10k_addrstall` instantiates
`MISTRAL_M10K_TDP` and attaches GPO[29] to `ADDRSTALLA` after synthesis.
Quartus comparison is not implemented.

GPO bit 30 enables the A port. Packed `ADDRSTALLA` holds the A-port
address while GPO[29] is 0 and samples a new address while GPO[29] is 1.
Bit 31 writes after `0x13579BDF`. GPI signature `0xD42C`.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
