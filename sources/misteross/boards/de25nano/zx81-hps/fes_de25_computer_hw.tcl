package require -exact qsys 20.1
set_module_property NAME fes_de25_computer
set_module_property VERSION 1.1
set_module_property DISPLAY_NAME "FES DE25 ZX81 keyboard and tape diagnostic"
set_module_property GROUP "FES/Diagnostics"
set_module_property AUTHOR "FES"
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_computer
add_fileset_file fes_de25_computer.v VERILOG PATH fes_de25_computer.v TOP_LEVEL_FILE
add_fileset_file generated/fes_de25_computer.vh VERILOG_INCLUDE PATH generated/fes_de25_computer.vh
add_fileset_file fes_computer.vh VERILOG_INCLUDE PATH generated/fes_computer.vh
add_fileset_file fes_computer_mailbox.v SYSTEM_VERILOG PATH fes_computer_mailbox.v
add_fileset_file zx81_model.vh VERILOG_INCLUDE PATH zx81_model.vh
add_fileset_file zx81_machine.sv SYSTEM_VERILOG PATH zx81_machine.sv
add_fileset_file zx81_machine_clock.v VERILOG PATH zx81_machine_clock.v
add_fileset_file zx81_keyboard.v VERILOG PATH zx81_keyboard.v
add_fileset_file zx81_tape_store_de25.v VERILOG PATH zx81_tape_store_de25.v
add_fileset_file zx81_dpram_de25.v VERILOG PATH zx81_dpram_de25.v
add_fileset_file video.v VERILOG PATH video.v
foreach name {fes_z80_alu fes_z80_engine fes_z80_bus fes_z80_nmos} {
    add_fileset_file ${name}.sv SYSTEM_VERILOG PATH ${name}.sv
}
add_fileset_file zx8x.mif MIF PATH zx8x.mif

add_interface clk clock sink
add_interface_port clk clk clk Input 1
add_interface pixel_clk clock sink
add_interface_port pixel_clk pixel_clk clk Input 1
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
add_interface_port avs address address Input 5
add_interface_port avs read read Input 1
add_interface_port avs write write Input 1
add_interface_port avs byteenable byteenable Input 4
add_interface_port avs writedata writedata Input 32
add_interface_port avs readdata readdata Output 32
add_interface_port avs readdatavalid readdatavalid Output 1
add_interface_port avs waitrequest waitrequest Output 1
add_interface_port avs writeresponsevalid writeresponsevalid Output 1
add_interface_port avs response response Output 2
add_interface video conduit end
add_interface_port video video_ready ready Input 1
add_interface_port video video_rgb rgb Output 24
add_interface_port video video_de de Output 1
add_interface_port video video_hs hs Output 1
add_interface_port video video_vs vs Output 1

add_parameter EXTERNAL_RAM INTEGER 0
set_parameter_property EXTERNAL_RAM HDL_PARAMETER true
add_interface expansion conduit end
set_interface_property expansion associatedClock clk
foreach {port direction width} {expansion_running Input 1 diagnostic_address Input 8 expansion_response Input 20 expansion_request Output 46 memory_peek Output 8} {
    add_interface_port expansion $port $port $direction $width
}
add_fileset_file zx81_bus_pack.vh VERILOG_INCLUDE PATH zx81_bus_pack.vh
