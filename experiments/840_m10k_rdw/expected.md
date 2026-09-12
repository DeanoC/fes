# 840 M10K TDP read-during-write contract

`make sim EXP=840_m10k_rdw` checks INIT and a same-port write-through on
`MISTRAL_M10K_TDP`. `make oss EXP=840_m10k_rdw` sets `CFG_RDW_MODE_A` and
`CFG_RDW_MODE_B` to `NEW_DATA_NO_NBE_READ` and `CFG_RDW_MODE_MIXED` to
`DONT_CARE` after synthesis. Quartus comparison is not implemented.

GPO bit 30 enables the A port. Bit 31 writes after `0x13579BDF`. GPI
signature `0xD840`.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
