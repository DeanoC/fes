# 780 Quartus SDC/QSF PLL measurement

`make sim EXP=780_quartus_sdc` reuses the 090 digital toggling stand-in
and meter. `make oss EXP=780_quartus_sdc` routes with Quartus SDC/QSF
forms: `get_clocks`, `derive_pll_clocks`, `derive_clock_uncertainty`,
multiline `set_clock_groups`, and `-entity` on `set_instance_assignment`.
PLL clocks still come from the packed `altera_pll`. Quartus comparison
is not implemented.

Hardware acceptance is the 090 meter: GPI `0xD780`, **2048 ±1** over
2^20 reference cycles, lock=1, no sampled lock loss, three trials.

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
