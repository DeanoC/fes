package require -exact qsys 20.1
set_module_property NAME fes_de25_zx81_standard
set_module_property VERSION 1.0
set_module_property DISPLAY_NAME "FES DE25 standard ZX81"
set_module_property GROUP FES
set_module_property AUTHOR FES
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_parameter BUILD_ID STD_LOGIC_VECTOR 0x0
set_parameter_property BUILD_ID WIDTH 128
set_parameter_property BUILD_ID HDL_PARAMETER true
set_parameter_property BUILD_ID AFFECTS_GENERATION false
add_parameter EXPANSION_SOCKET INTEGER 1
set_parameter_property EXPANSION_SOCKET ALLOWED_RANGES {0 1}
set_parameter_property EXPANSION_SOCKET HDL_PARAMETER true
add_parameter FIRMWARE_INIT STRING "zx8x.mif"
set_parameter_property FIRMWARE_INIT HDL_PARAMETER true
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_zx81_standard
add_fileset_file fes_de25_zx81_standard.v VERILOG PATH fes_de25_zx81_standard.v TOP_LEVEL_FILE
foreach file {fes_de25_computer.v fes_zx81_core.v zx81_machine_clock.v zx81_keyboard.v
    zx81_tape_store_de25.v zx81_dpram_de25.v zx81_video_720p_de25.v
    fes_de25_scanout.v fes_display_cdc.v fes_menu_video.v fes_menu_reader_de25.v} {
    add_fileset_file $file VERILOG PATH $file
}
# The portable .v display/control sources use SystemVerilog sized casts too.
foreach file {zx81_machine.sv fes_computer_mailbox.v fes_de25_session_display.v fes_display_plane.v
    fes_menu_control.v fes_z80_alu.sv fes_z80_engine.sv fes_z80_bus.sv fes_z80_nmos.sv} {
    add_fileset_file $file SYSTEM_VERILOG PATH $file
}
foreach file {fes_de25_computer.vh fes_de25_scanout.vh} {
    add_fileset_file generated/$file VERILOG_INCLUDE PATH generated/$file
}
# Portable shared RTL includes these by basename, as the DE10 build's include
# path does. Qsys must emit them beside that RTL, rather than in generated/.
foreach file {fes_computer.vh fes_application.vh fes_display_plane.vh} {
    add_fileset_file $file VERILOG_INCLUDE PATH generated/$file
}
foreach file {zx81_model.vh zx81_bus_pack.vh} {
    add_fileset_file $file VERILOG_INCLUDE PATH $file
}
add_fileset_file zx8x.mif MIF PATH zx8x.mif
foreach {clock port reset reset_port} {clk_sys clk_sys system_reset reset_n clk pixel_clk reset pixel_reset_n} {
    add_interface $clock clock sink
    add_interface_port $clock $port clk Input 1
    add_interface $reset reset sink
    set_interface_property $reset associatedClock $clock
    set_interface_property $reset synchronousEdges DEASSERT
    add_interface_port $reset $reset_port reset_n Input 1
}
foreach {name prefix clock reset bits span} {
    computer computer_ clk_sys system_reset 5 32
    avs scanout_ clk reset 7 128
} {
    add_interface $name avalon end
    set_interface_property $name associatedClock $clock
    set_interface_property $name associatedReset $reset
    set_interface_property $name explicitAddressSpan $span
    foreach {key value} {addressUnits SYMBOLS bitsPerSymbol 8
        maximumPendingReadTransactions 1 maximumPendingWriteTransactions 1 minimumResponseLatency 1
        readLatency 0 readWaitTime 0 writeWaitTime 0 waitrequestAllowance 0 setupTime 0 holdTime 0
        timingUnits Cycles isMemoryDevice false} {
        set_interface_property $name $key $value
    }
    add_interface_port $name ${prefix}address address Input $bits
    foreach {signal direction width} {read Input 1 write Input 1
        byteenable Input 4 writedata Input 32 readdata Output 32 readdatavalid Output 1
        waitrequest Output 1 writeresponsevalid Output 1 response Output 2} {
        add_interface_port $name ${prefix}${signal} $signal $direction $width
    }
}
add_interface avm avalon start
set_interface_property avm associatedClock clk
set_interface_property avm associatedReset reset
foreach {key value} {addressUnits SYMBOLS bitsPerSymbol 8 burstcountUnits WORDS maximumPendingReadTransactions 16
    waitrequestAllowance 0 linewrapBursts false burstOnBurstBoundariesOnly false doStreamReads false doStreamWrites false} {
    set_interface_property avm $key $value
}
foreach {signal direction width} {address Output 32 burstcount Output 5 read Output 1 byteenable Output 16
    waitrequest Input 1 readdata Input 128 readdatavalid Input 1 response Input 2} {
    add_interface_port avm avm_$signal $signal $direction $width
}
add_interface video conduit end
foreach {port role width} {video_rgb rgb 24 video_de de 1 video_hs hs 1 video_vs vs 1} {
    add_interface_port video $port $role Output $width
}
add_interface ready conduit end
add_interface_port ready video_ready export Input 1
add_interface clock_fault conduit end
add_interface_port clock_fault clock_fault export Input 1
add_interface audio conduit end
set_interface_property audio associatedClock clk_sys
add_interface_port audio audio_sample sample Output 16
add_interface_port audio exec_reset reset Output 1
add_interface expansion conduit end
set_interface_property expansion associatedClock clk_sys
foreach {port direction width} {expansion_running Input 1 bus_addr Output 16 bus_wdata Output 8
    bus_mreq_n Output 1 bus_iorq_n Output 1 bus_rd_n Output 1 bus_wr_n Output 1 bus_m1_n Output 1
    bus_rfsh_n Output 1 bus_cpu_clock Output 1 bus_rdata Input 8 bus_peek_data Input 8
    bus_dsel Input 1 bus_romcs Input 1 bus_wait Input 1 bus_ram_present Input 1} {
    add_interface_port expansion $port $port $direction $width
}
# Read-only diagnostic observations and unused software peek address. These
# never become a second production control or memory interface.
add_interface observations conduit end
foreach {port direction width} {video Output 28 peek_addr Input 16 peek_data Output 8
    cpu_addr Output 16 ram_write_enable Output 1 ui_active Output 1 display_faulted Output 1 display_quiesced Output 1} {
    add_interface_port observations $port $port $direction $width
    set_port_property $port TERMINATION true
    if {$direction eq "Input"} {set_port_property $port TERMINATION_VALUE 0}
}
