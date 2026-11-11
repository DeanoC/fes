package require -exact qsys 20.1
set_module_property NAME fes_de25_pr_control
set_module_property VERSION 1.0
set_module_property DISPLAY_NAME "FES DE25 HPS PR control diagnostic"
set_module_property DESCRIPTION "One bounded sequenced freeze/drain/probe slot; no configuration master or DMA."
set_module_property GROUP "FES/Diagnostics"
set_module_property AUTHOR FES
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_link_shell
add_fileset_file shell.v VERILOG PATH shell.v TOP_LEVEL_FILE
add_fileset_file generated/fes_de25_pr_control.vh VERILOG_INCLUDE PATH generated/fes_de25_pr_control.vh

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
set_interface_property avs explicitAddressSpan 64
set_interface_property avs maximumPendingReadTransactions 1
set_interface_property avs maximumPendingWriteTransactions 1
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
add_interface_port avs address address Input 6
add_interface_port avs read read Input 1
add_interface_port avs write write Input 1
add_interface_port avs byteenable byteenable Input 4
add_interface_port avs writedata writedata Input 32
add_interface_port avs readdata readdata Output 32
add_interface_port avs readdatavalid readdatavalid Output 1
add_interface_port avs waitrequest waitrequest Output 1
add_interface_port avs writeresponsevalid writeresponsevalid Output 1
add_interface_port avs response response Output 2
add_interface slot conduit end
set_interface_property slot associatedClock clk
set_interface_property slot associatedReset reset
foreach {port role direction width} {req req Output 1 slot_address address Output 8 slot_reset reset Output 1 rsp_valid rsp_valid Input 1 rsp_data rsp_data Input 32} {
    add_interface_port slot $port $role $direction $width
}
add_interface diagnostic conduit end
set_interface_property diagnostic associatedClock clk
foreach {port width} {frozen 1 running 1 isolated_data 32 static_count 32 retired 32 last_data 32} {
    add_interface_port diagnostic $port $port Output $width
}
