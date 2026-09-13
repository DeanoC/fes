# 860 M10K dual-clock selector pair

`make sim EXP=860_m10k_selectors` checks distinct INIT on two 512-by-20
SDP M10Ks and a write that does not disturb the other bank. `make oss
EXP=860_m10k_selectors` requires two `MISTRAL_M10K` cells with live
`CLK1`/`CLK2` packed on unique sites. Quartus comparison is not
implemented.

GPO bit 9 selects the bank. Read enables are tied high and OSS sets
`CFG_OUT_REG_B` so nextpnr packs `ENABLE.1`/`WREN.0`. Bit 31 writes
after `0x13579BDF`. GPI signature `0xD860`.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
