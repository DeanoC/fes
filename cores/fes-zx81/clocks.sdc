# 50 MHz DE10-Nano input clock. PLL outputs are derived.
create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
derive_pll_clocks
derive_clock_uncertainty
set_clock_groups -asynchronous \
    -group [get_clocks {*system_clock*}] \
    -group [get_clocks {*video_clock*}]
