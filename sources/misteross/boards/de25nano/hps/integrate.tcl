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
save_system $peripheral_system
