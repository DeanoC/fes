package require -exact qsys 20.1
set_module_property VERSION 1.0
set_module_property GROUP "FES/Diagnostics"
set_module_property AUTHOR "FES"
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
add_fileset_file generated/fes_de25_ramtest.vh VERILOG_INCLUDE PATH generated/fes_de25_ramtest.vh
add_fileset_file scan.v VERILOG PATH scan.v
add_fileset_file endpoint.v VERILOG PATH endpoint.v
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
set_interface_property avs explicitAddressSpan 256
set_interface_property avs maximumPendingReadTransactions 1
set_interface_property avs maximumPendingWriteTransactions 1
set_interface_property avs minimumResponseLatency 1
set_interface_property avs readLatency 0
set_interface_property avs waitrequestAllowance 0
add_interface_port avs address address Input 8
add_interface_port avs read read Input 1
add_interface_port avs write write Input 1
add_interface_port avs byteenable byteenable Input 4
add_interface_port avs writedata writedata Input 32
add_interface_port avs readdata readdata Output 32
add_interface_port avs readdatavalid readdatavalid Output 1
add_interface_port avs waitrequest waitrequest Output 1
add_interface_port avs writeresponsevalid writeresponsevalid Output 1
add_interface_port avs response response Output 2
set_module_property NAME fes_de25_sdram_ramtest
set_module_property DISPLAY_NAME "FES DE25 onboard SDRAM tester"
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_sdram_ramtest
add_fileset_file sdram.v VERILOG PATH sdram.v
add_fileset_file sdram_endpoint.v VERILOG PATH sdram_endpoint.v
add_interface dram conduit end
add_interface_port dram dram_clk clk Output 1
add_interface_port dram dram_cke cke Output 1
add_interface_port dram dram_addr addr Output 13
add_interface_port dram dram_ba ba Output 2
add_interface_port dram dram_cs_n cs_n Output 2
add_interface_port dram dram_ras_n ras_n Output 1
add_interface_port dram dram_cas_n cas_n Output 1
add_interface_port dram dram_we_n we_n Output 1
add_interface_port dram dram_dqm dqm Output 2
add_interface_port dram dram_dq dq Bidir 16
