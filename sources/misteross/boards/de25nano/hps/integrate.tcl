# This script runs only in the extracted private GHRD copy.
package require -exact qsys 26.3
set peripheral_system @PERIPHERAL_SYSTEM@
load_system $peripheral_system
set_project_property DEVICE_FAMILY "Agilex 5"
set_project_property DEVICE A5EB013BB23BE4SCS
add_instance fes_mailbox fes_de25_mailbox 1.0
add_connection periph_clk.out_clk fes_mailbox.clk
add_connection periph_rst_in.out_reset fes_mailbox.reset
add_connection pb_cpu_0.m0 fes_mailbox.avs
set_connection_parameter_value pb_cpu_0.m0/fes_mailbox.avs baseAddress @MAILBOX_BASE@
add_instance fes_ddr_probe fes_de25_ddr_probe 1.0
add_connection periph_clk.out_clk fes_ddr_probe.clk
add_connection periph_rst_in.out_reset fes_ddr_probe.reset
add_connection pb_cpu_0.m0 fes_ddr_probe.avs
set_connection_parameter_value pb_cpu_0.m0/fes_ddr_probe.avs baseAddress @PROBE_BASE@
add_interface fes_probe_mem avalon start
set_interface_property fes_probe_mem EXPORT_OF fes_ddr_probe.avm
save_system $peripheral_system

# Refresh the changed child interface before wiring its exported read master.
load_system @TOP_SYSTEM@
set_project_property DEVICE_FAMILY "Agilex 5"
set_project_property DEVICE A5EB013BB23BE4SCS
add_connection subsys_periph.fes_probe_mem subsys_hps.f2sdram_adapter_axi4_sub
set_connection_parameter_value subsys_periph.fes_probe_mem/subsys_hps.f2sdram_adapter_axi4_sub baseAddress 0x0
save_system @TOP_SYSTEM@
