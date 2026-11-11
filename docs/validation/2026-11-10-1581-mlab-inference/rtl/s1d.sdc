# The pc386 system clock (SYSTEM_CLOCK_HZ = 61.44 MHz in build_fes_pc386_quartus.py).
create_clock -name clk -period 16.276 [get_ports {clk}]
derive_clock_uncertainty
