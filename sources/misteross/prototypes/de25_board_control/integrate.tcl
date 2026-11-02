# Insert in the pinned, private ZX81 diagnostic peripheral system.
add_instance fes_embedded fes_de25_embedded 1.0
set_instance_parameter_value fes_embedded MEMORY_BACKEND @MEMORY_BACKEND@
set_instance_parameter_value fes_embedded DDR_BASE @DDR_BASE@
set_instance_parameter_value fes_embedded DDR_BYTES @DDR_BYTES@
add_connection periph_clk.out_clk fes_embedded.clk
add_connection periph_rst_in.out_reset fes_embedded.reset
add_connection pb_cpu_0.m0 fes_embedded.avs
set_connection_parameter_value pb_cpu_0.m0/fes_embedded.avs baseAddress 0x10400
if {@MEMORY_BACKEND@} {
    add_interface fes_embedded_mem axi4 start
    set_interface_property fes_embedded_mem EXPORT_OF fes_embedded.mem
    add_interface fes_maintenance_reset reset start
    set_interface_property fes_maintenance_reset EXPORT_OF fes_embedded.maintenance_reset
}
