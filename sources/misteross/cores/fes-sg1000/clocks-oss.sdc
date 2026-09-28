# 50 MHz DE10-Nano input clock. The shared PLL derives 52.224 MHz system and
# 12.288 MHz audio; a second PLL derives 74.25 MHz video.
create_clock -name FPGA_CLK1_50 -period 20.000 [get_ports {FPGA_CLK1_50}]
