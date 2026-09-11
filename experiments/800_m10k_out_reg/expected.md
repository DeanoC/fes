# 800 M10K registered B-port output

`make sim EXP=800_m10k_out_reg` checks INIT and that a registered B-port
read holds the previous word for one sample after an address change.
`make oss EXP=800_m10k_out_reg` instantiates `MISTRAL_M10K` and sets
`CFG_OUT_REG_B` after synthesis. Quartus comparison is not implemented.

GPO bit 29 selects the later sample of the registered output. Bit 31
writes after `0x13579BDF`. GPI signature `0xD42D`.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
