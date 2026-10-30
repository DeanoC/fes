# Insert in the pinned, private ZX81 diagnostic peripheral system.
add_instance fes_embedded fes_de25_embedded 1.0
add_connection periph_clk.out_clk fes_embedded.clk
add_connection periph_rst_in.out_reset fes_embedded.reset
add_connection pb_cpu_0.m0 fes_embedded.avs
set_connection_parameter_value pb_cpu_0.m0/fes_embedded.avs baseAddress 0x10400
