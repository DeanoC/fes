# 50 MHz DE10-Nano input. nextpnr-mistral derives 52.224 MHz system,
# 12.288 MHz audio and 74.25 MHz pixel clocks from the two PLL cells.
create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
