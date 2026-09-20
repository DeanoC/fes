# 50 MHz DE10-Nano input clock. nextpnr-mistral derives the 52.224 MHz,
# 74.25 MHz and 12.288 MHz PLL outputs from the altera_pll cells.
create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
