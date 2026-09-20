# 892 reserved-slot M10K cart fragment

`make sim EXP=892_slot_m10k_cart` checks INIT on the same BEL-locked M10K as
890, with a dummy HPS shell. `make oss EXP=892_slot_m10k_cart` is the cart
bitstream for CRAM overlay onto 891. GPI signature `0xD892`. Quartus
comparison is not implemented. Shares `experiments/890_slot_m10k/pins.qsf`.
