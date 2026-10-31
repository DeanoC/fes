# The API version is supplied by the explicitly selected Quartus installation.
package require -exact qsys 20.1

set_module_property NAME fes_de25_mailbox
set_module_property VERSION 1.0
set_module_property DISPLAY_NAME "FES DE25 diagnostic mailbox"
set_module_property DESCRIPTION "Identity, version, byte-enabled scratch and clock counter; bridge-relative byte offsets only."
set_module_property GROUP "FES/Diagnostics"
set_module_property AUTHOR "FES"
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false

add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_mailbox
add_fileset_file fes_de25_mailbox.v VERILOG PATH fes_de25_mailbox.v TOP_LEVEL_FILE
add_fileset_file generated/fes_de25_mailbox.vh VERILOG_INCLUDE PATH generated/fes_de25_mailbox.vh

add_interface clk clock sink
add_interface_port clk clk clk Input 1

add_interface reset reset sink
set_interface_property reset associatedClock clk
set_interface_property reset synchronousEdges DEASSERT
add_interface_port reset reset_n reset_n Input 1

add_interface avs avalon end
set_interface_property avs associatedClock clk
set_interface_property avs associatedReset reset
set_interface_property avs addressUnits SYMBOLS
set_interface_property avs bitsPerSymbol 8
set_interface_property avs explicitAddressSpan 32
set_interface_property avs maximumPendingReadTransactions 1
set_interface_property avs minimumResponseLatency 1
set_interface_property avs readLatency 0
set_interface_property avs readWaitTime 0
set_interface_property avs writeWaitTime 0
set_interface_property avs waitrequestAllowance 0
set_interface_property avs setupTime 0
set_interface_property avs holdTime 0
set_interface_property avs timingUnits Cycles
set_interface_property avs isMemoryDevice false
set_interface_property avs isNonVolatileStorage false
add_interface_port avs address address Input 5
add_interface_port avs read read Input 1
add_interface_port avs write write Input 1
add_interface_port avs byteenable byteenable Input 4
add_interface_port avs writedata writedata Input 32
add_interface_port avs readdata readdata Output 32
add_interface_port avs readdatavalid readdatavalid Output 1
add_interface_port avs waitrequest waitrequest Output 1
