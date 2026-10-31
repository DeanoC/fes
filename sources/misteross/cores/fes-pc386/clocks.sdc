# Quartus seal of the pc386 shell (scripts/build_fes_pc386_quartus.py). The
# 50 MHz DE10-Nano reference drives two PLLs: system_clock makes the system
# and 12.288 MHz audio clocks, video_clock the 74.25 MHz pixel clock. Paths
# between the two PLLs are not timed, so every crossing must be synchronised
# in the RTL.
create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
derive_pll_clocks
derive_clock_uncertainty
set_clock_groups -asynchronous \
    -group [get_clocks {*system_clock*}] \
    -group [get_clocks {*video_clock*}]
