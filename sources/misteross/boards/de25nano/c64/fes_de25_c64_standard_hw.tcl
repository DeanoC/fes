package require -exact qsys 20.1
set_module_property NAME fes_de25_c64_standard
set_module_property VERSION 1.0
set_module_property DISPLAY_NAME "FES DE25 standard Commodore 64"
set_module_property GROUP FES
set_module_property AUTHOR FES
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_parameter BUILD_ID STD_LOGIC_VECTOR 0x0
set_parameter_property BUILD_ID WIDTH 128
set_parameter_property BUILD_ID HDL_PARAMETER true
set_parameter_property BUILD_ID AFFECTS_GENERATION false
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_c64_standard
add_fileset_file fes_de25_c64_standard.v VERILOG PATH fes_de25_c64_standard.v TOP_LEVEL_FILE
foreach file {fes_de25_computer_transport.v fes_gp_crossing.v
    fes_de25_scanout.v fes_display_cdc.v fes_menu_video.v fes_menu_reader_de25.v} {
    add_fileset_file $file VERILOG PATH $file
}
# The portable machine, mailbox and display sources use SystemVerilog sized
# casts and typed parameters under a .v name too.
foreach file {fes_c64_core.v fes_firmware_upload.v fes_c64_firmware_rom.v
    fes_computer_mailbox.v fes_de25_session_display.v fes_display_plane.v fes_menu_control.v
    c64_machine_de25.sv c64_ram_de25.v c64_color_ram_de25.v c64_disk_store_de25.v c64_frame_store_de25.sv
    c64_vic.v c64_vic_graphics_pixels.sv c64_vic_sprite_pixels.sv c64_vic_collisions.sv
    c64_vic_fetch.sv c64_vic_sprite_fetch.sv c64_sid.v c64_sid_envelope.sv c64_sid_oscillator.sv
    c64_sid_wave_tables.sv c64_sid_svf.sv c64_cia.v c64_drive1541.sv c64_via.v c64_gcr_read.v
    c64_d64_medium.sv c64_keyboard.v cpu6502.v cpu6502_alu.v} {
    add_fileset_file $file SYSTEM_VERILOG PATH $file
}
foreach file {fes_de25_computer.vh fes_de25_scanout.vh} {
    add_fileset_file generated/$file VERILOG_INCLUDE PATH generated/$file
}
# Portable shared RTL includes these by basename, as the DE10 build's include
# path does. Qsys must emit them beside that RTL, rather than in generated/.
foreach file {fes_computer.vh fes_application.vh fes_display_plane.vh fes_firmware_upload.vh} {
    add_fileset_file $file VERILOG_INCLUDE PATH generated/$file
}
foreach file {c64_bus.vh c64_pal_timing.vh c64_sid_combined_waves.vh c64_sid_svf_tables.vh} {
    add_fileset_file $file VERILOG_INCLUDE PATH $file
}
foreach {clock port reset reset_port} {clk_sys clk_sys system_reset reset_n clk pixel_clk reset pixel_reset_n} {
    add_interface $clock clock sink
    add_interface_port $clock $port clk Input 1
    add_interface $reset reset sink
    set_interface_property $reset associatedClock $clock
    set_interface_property $reset synchronousEdges DEASSERT
    add_interface_port $reset $reset_port reset_n Input 1
}
# The machine clock has no bus interface: its reset release and every
# crossing to the two bus clocks are inside the component.
add_interface machine_clk clock sink
add_interface_port machine_clk machine_clk clk Input 1
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
foreach {signal direction width} {address Output 32 burstcount Output 7 read Output 1 byteenable Output 16
    waitrequest Input 1 readdata Input 128 readdatavalid Input 1 response Input 2} {
    add_interface_port avm avm_$signal $signal $direction $width
}
add_interface video conduit end
foreach {port role width} {video_rgb rgb 24 video_de de 1 video_hs hs 1 video_vs vs 1} {
    add_interface_port video $port $role Output $width
}
add_interface ready conduit end
add_interface_port ready video_ready export Input 1
add_interface_port ready stream_ready stream_ready Input 1
add_interface clock_fault conduit end
add_interface_port clock_fault clock_fault export Input 1
add_interface audio conduit end
set_interface_property audio associatedClock machine_clk
add_interface_port audio audio_sample sample Output 16
add_interface_port audio exec_reset reset Output 1
# Board SDRAM pins. The top level owns the DQ tri-state.
add_interface sdram conduit end
set_interface_property sdram associatedClock machine_clk
foreach {port role direction width} {dram_clk clk Output 1 dram_cke cke Output 1 dram_addr addr Output 13
    dram_ba ba Output 2 dram_cs_n cs_n Output 2 dram_dqm dqm Output 2 dram_ras_n ras_n Output 1
    dram_cas_n cas_n Output 1 dram_we_n we_n Output 1 dram_dq_i dq_i Input 16 dram_dq_o dq_o Output 16
    dram_dq_oe dq_oe Output 1} {
    add_interface_port sdram $port $role $direction $width
}
# Read-only diagnostic observations. These never become a second production
# control or memory interface.
add_interface observations conduit end
foreach {port direction width} {video Output 28 ui_active Output 1 display_faulted Output 1 display_quiesced Output 1} {
    add_interface_port observations $port $port $direction $width
    set_port_property $port TERMINATION true
}
