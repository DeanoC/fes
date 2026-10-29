# Private, source-only copy of Terasic's RTL LPDDR4 reference.
package require -exact qsys 26.3
load_system @EMIF_SOURCE@
set_project_property DEVICE_FAMILY "Agilex 5"
set_project_property DEVICE A5EB013BB23BE4SCS
foreach interface {clock_310m_out_clk emif_lpddr4a_mem emif_lpddr4a_mem_ck emif_lpddr4a_mem_reset_n emif_lpddr4a_oct emif_lpddr4a_ref_clk emif_lpddr4a_s0_axi4 emif_lpddr4a_s0_axi4_ctrl_ready emif_lpddr4a_s0_axi4lite emif_lpddr4a_s0_axi4lite_clock emif_lpddr4a_s0_axi4lite_reset_n iopll_locked iopll_outclk_axi4_lite iopll_refclk iopll_reset emif_lpddr4b_s0_axi4lite_clock} {
    remove_interface $interface
}
foreach instance {emif_lpddr4a iopll clock_310m} {remove_instance $instance}
add_instance fabric100 altera_clock_bridge 19.2.0
set_instance_parameter_value fabric100 EXPLICIT_CLOCK_RATE 100000000
add_interface fabric100 clock sink
set_interface_property fabric100 EXPORT_OF fabric100.in_clk
add_connection fabric100.out_clk reset_in.clk
add_connection fabric100.out_clk emif_lpddr4b.s0_axi4_clock_in
add_instance control50 altera_clock_bridge 19.2.0
set_instance_parameter_value control50 EXPLICIT_CLOCK_RATE 50000000
add_interface control50 clock sink
set_interface_property control50 EXPORT_OF control50.in_clk
add_connection control50.out_clk emif_lpddr4b.s0_axi4lite_clock
save_system @EMIF_SYSTEM@

