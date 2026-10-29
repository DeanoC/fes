# Insert while the preserved GHRD peripheral system is loaded.
foreach {instance channel local_memory base} {
    fes_ramtest_hps 0 0 @HPS_BASE@
    fes_ramtest_fpga 1 1 @FPGA_BASE@
} {
    add_instance $instance fes_de25_ramtest 1.0
    set_instance_parameter_value $instance CHANNEL $channel
    set_instance_parameter_value $instance LOCAL_MEMORY $local_memory
    add_connection periph_clk.out_clk $instance.clk
    add_connection periph_rst_in.out_reset $instance.reset
    add_connection pb_cpu_0.m0 $instance.avs
    set_connection_parameter_value pb_cpu_0.m0/$instance.avs baseAddress $base
    add_interface ${instance}_mem avalon start
    set_interface_property ${instance}_mem EXPORT_OF $instance.avm
    add_interface ${instance}_calibration conduit end
    set_interface_property ${instance}_calibration EXPORT_OF $instance.calibration
}
add_instance fes_ramtest_sdram fes_de25_sdram_ramtest 1.0
add_instance fes_ramtest_clk50 altera_clock_bridge 19.2.0
set_instance_parameter_value fes_ramtest_clk50 EXPLICIT_CLOCK_RATE 50000000
add_interface fes_ramtest_clk50 clock sink
set_interface_property fes_ramtest_clk50 EXPORT_OF fes_ramtest_clk50.in_clk
add_connection fes_ramtest_clk50.out_clk fes_ramtest_sdram.clk
add_connection periph_rst_in.out_reset fes_ramtest_sdram.reset
add_connection pb_cpu_0.m0 fes_ramtest_sdram.avs
set_connection_parameter_value pb_cpu_0.m0/fes_ramtest_sdram.avs baseAddress @SDRAM_BASE@
add_interface fes_ramtest_dram conduit end
set_interface_property fes_ramtest_dram EXPORT_OF fes_ramtest_sdram.dram

