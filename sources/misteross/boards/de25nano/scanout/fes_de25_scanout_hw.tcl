package require -exact qsys 20.1
set_module_property NAME fes_de25_scanout
set_module_property VERSION 1.0
set_module_property DISPLAY_NAME "FES DE25 allocated-buffer scanout diagnostic"
set_module_property GROUP "FES/Diagnostics"
set_module_property AUTHOR "FES"
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_scanout
add_fileset_file fes_de25_scanout.v VERILOG PATH fes_de25_scanout.v TOP_LEVEL_FILE
add_fileset_file generated/fes_de25_scanout.vh VERILOG_INCLUDE PATH generated/fes_de25_scanout.vh
add_fileset_file fes_menu_reader_de25.v VERILOG PATH fes_menu_reader_de25.v
add_fileset_file fes_menu_video.v VERILOG PATH fes_menu_video.v
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
set_interface_property avs explicitAddressSpan 128
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
add_interface_port avs address address Input 7
add_interface_port avs read read Input 1
add_interface_port avs write write Input 1
add_interface_port avs byteenable byteenable Input 4
add_interface_port avs writedata writedata Input 32
add_interface_port avs readdata readdata Output 32
add_interface_port avs readdatavalid readdatavalid Output 1
add_interface_port avs waitrequest waitrequest Output 1
add_interface_port avs writeresponsevalid writeresponsevalid Output 1
add_interface_port avs response response Output 2
add_interface avm avalon start
set_interface_property avm associatedClock clk
set_interface_property avm associatedReset reset
set_interface_property avm addressUnits SYMBOLS
set_interface_property avm bitsPerSymbol 8
set_interface_property avm burstcountUnits WORDS
set_interface_property avm maximumPendingReadTransactions 1
set_interface_property avm waitrequestAllowance 0
set_interface_property avm linewrapBursts false
set_interface_property avm burstOnBurstBoundariesOnly false
set_interface_property avm doStreamReads false
set_interface_property avm doStreamWrites false
add_interface_port avm avm_address address Output 32
add_interface_port avm avm_burstcount burstcount Output 5
add_interface_port avm avm_read read Output 1
add_interface_port avm avm_byteenable byteenable Output 16
add_interface_port avm avm_waitrequest waitrequest Input 1
add_interface_port avm avm_readdata readdata Input 128
add_interface_port avm avm_readdatavalid readdatavalid Input 1
add_interface_port avm avm_response response Input 2

add_interface video conduit end
add_interface_port video video_rgb rgb Output 24
add_interface_port video video_de de Output 1
add_interface_port video video_hs hs Output 1
add_interface_port video video_vs vs Output 1
add_interface clock_fault conduit end
add_interface_port clock_fault clock_fault export Input 1
