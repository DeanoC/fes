# 891 empty reserved slot (base)

`make sim EXP=891_slot_m10k_base` checks the fabric probe signature with no
M10K in the netlist. `make oss EXP=891_slot_m10k_base` is the empty-slot
bitstream for CRAM overlay. GPI signature `0xD89100A6`. Quartus comparison is
not implemented. Shares `experiments/890_slot_m10k/pins.qsf`.
