# Quartus SDC subset accepted by nextpnr-mistral.
create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
derive_pll_clocks
derive_clock_uncertainty
set_clock_groups -asynchronous \
    -group [get_clocks {FPGA_CLK1_50}] \
    -group [get_clocks {clk25}]
