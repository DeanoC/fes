# Runs while the captured GHRD peripheral system is loaded, before top reload.
add_instance fes_pixel_clk altera_clock_bridge 19.2.0
set_instance_parameter_value fes_pixel_clk EXPLICIT_CLOCK_RATE 74250000
add_interface fes_scanout_pixel_clk clock sink
set_interface_property fes_scanout_pixel_clk EXPORT_OF fes_pixel_clk.in_clk
foreach {instance clock edges} {
    fes_scanout_pixel_reset fes_pixel_clk.out_clk deassert
    fes_scanout_system_reset periph_clk.out_clk deassert
} {
    add_instance $instance altera_reset_bridge 19.2.0
    set_instance_parameter_value $instance ACTIVE_LOW_RESET 1
    set_instance_parameter_value $instance SYNCHRONOUS_EDGES $edges
    set_instance_parameter_value $instance O_SYNCHRONOUS_EDGES $edges
    set_instance_parameter_value $instance NUM_RESET_OUTPUTS 1
    set_instance_parameter_value $instance USE_RESET_REQUEST 0
    add_connection $clock $instance.clk
    add_interface $instance reset sink
    set_interface_property $instance EXPORT_OF $instance.in_reset
}
add_instance fes_scanout fes_de25_scanout 1.0
set_instance_parameter_value fes_scanout BUILD_ID @SCANOUT_BUILD_ID@
add_connection fes_pixel_clk.out_clk fes_scanout.clk
add_connection fes_scanout_pixel_reset.out_reset fes_scanout.reset
add_interface fes_scanout_video conduit end
set_interface_property fes_scanout_video EXPORT_OF fes_scanout.video
add_interface fes_scanout_ready conduit end
set_interface_property fes_scanout_ready EXPORT_OF fes_scanout.ready
add_interface fes_scanout_clock_fault conduit end
set_interface_property fes_scanout_clock_fault EXPORT_OF fes_scanout.clock_fault
foreach {instance width address_bits max_burst response_depth} {
    fes_ctrl_cdc 32 7 1 4
    fes_mem_cdc 128 32 16 256
} {
    add_instance $instance mm_ccb 19.4.1
    foreach {name value} [list DATA_WIDTH $width SYMBOL_WIDTH 8 ADDRESS_WIDTH $address_bits \
            USE_AUTO_ADDRESS_WIDTH 0 ADDRESS_UNITS SYMBOLS MAX_BURST_SIZE $max_burst \
            COMMAND_FIFO_DEPTH 4 RESPONSE_FIFO_DEPTH $response_depth \
            MASTER_SYNC_DEPTH 2 SLAVE_SYNC_DEPTH 2 ENABLE_RESPONSE 1 SYNC_RESET 0 PIPELINE_ENABLE 0] {
        set_instance_parameter_value $instance $name $value
    }
    # Reset release follows each side's own clock. STOP/CLEAR and post-startup
    # pixel-lock loss never reset these bridges.
    if {[string compare $instance "fes_ctrl_cdc"] == 0} {
        add_connection fes_scanout_system_reset.out_reset $instance.s0_reset
        add_connection fes_scanout_pixel_reset.out_reset $instance.m0_reset
    } else {
        add_connection fes_scanout_pixel_reset.out_reset $instance.s0_reset
        add_connection fes_scanout_system_reset.out_reset $instance.m0_reset
    }
}
add_connection periph_clk.out_clk fes_ctrl_cdc.s0_clk
add_connection fes_pixel_clk.out_clk fes_ctrl_cdc.m0_clk
add_connection fes_pixel_clk.out_clk fes_mem_cdc.s0_clk
add_connection periph_clk.out_clk fes_mem_cdc.m0_clk
add_instance fes_scanout_guard fes_de25_scanout_guard 1.0
add_connection periph_clk.out_clk fes_scanout_guard.clk
add_connection periph_rst_in.out_reset fes_scanout_guard.reset
add_connection pb_cpu_0.m0 fes_scanout_guard.avs
set_connection_parameter_value pb_cpu_0.m0/fes_scanout_guard.avs baseAddress @SCANOUT_BASE@
add_connection fes_scanout_guard.avm fes_ctrl_cdc.s0
set_connection_parameter_value fes_scanout_guard.avm/fes_ctrl_cdc.s0 baseAddress 0
add_connection fes_ctrl_cdc.m0 fes_scanout.avs
set_connection_parameter_value fes_ctrl_cdc.m0/fes_scanout.avs baseAddress 0
add_connection fes_scanout.avm fes_mem_cdc.s0
set_connection_parameter_value fes_scanout.avm/fes_mem_cdc.s0 baseAddress 0
add_interface fes_scanout_mem avalon start
set_interface_property fes_scanout_mem EXPORT_OF fes_mem_cdc.m0
