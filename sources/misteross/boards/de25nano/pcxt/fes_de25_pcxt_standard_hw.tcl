package require -exact qsys 20.1
set_module_property NAME fes_de25_pcxt_standard
set_module_property VERSION 1.0
set_module_property DISPLAY_NAME "FES DE25 standard PC/XT"
set_module_property GROUP FES
set_module_property AUTHOR FES
set_module_property INSTANTIATE_IN_SYSTEM_MODULE true
set_module_property EDITABLE false
add_parameter BUILD_ID STD_LOGIC_VECTOR 0x0
set_parameter_property BUILD_ID WIDTH 128
set_parameter_property BUILD_ID HDL_PARAMETER true
set_parameter_property BUILD_ID AFFECTS_GENERATION false
add_fileset QUARTUS_SYNTH QUARTUS_SYNTH
set_fileset_property QUARTUS_SYNTH TOP_LEVEL fes_de25_pcxt_standard
add_fileset_file fes_de25_pcxt_standard.v VERILOG PATH fes_de25_pcxt_standard.v TOP_LEVEL_FILE
foreach file {fes_de25_computer_transport.v sdram_addon_port_de25.v sdram_cas3_capture.v fes_gp_crossing.v
    fes_de25_scanout.v fes_display_cdc.v fes_menu_video.v fes_menu_reader_de25.v coleco_video_dpram_de25.v} {
    add_fileset_file $file VERILOG PATH $file
}
# The portable machine, mailbox and display sources use SystemVerilog sized
# casts and typed parameters under a .v name too.
foreach file {fes_pcxt_core.v fes_pcxt_firmware.v fes_firmware_upload.v
    fes_computer_mailbox.v fes_de25_session_display.v fes_display_plane.v fes_menu_control.v
    fes_video_part_direct.v
    pcxt_isa_bridge.sv pcxt_isa_boundary.sv pcxt_isa_endpoint.sv pcxt_isa_probe.sv pcxt_6845.sv
    pcxt_display_card.sv pcxt_cga_card.sv pcxt_mda_card.sv pcxt_hercules_card.sv pcxt_fdc_card.sv pcxt_display_bus.sv KF8255.sv
    KF8255_Control_Logic.sv KF8255_Group.sv KF8255_Port.sv KF8255_Port_C.sv pcxt_xtio.sv pcxt_xt_keyboard.sv
    pcxt_speaker.sv pcxt_motherboard.sv pcxt_logic_operand1_mux.sv mcl86_adder.sv mcl86_eu_core.sv
    mcl86_biu_max.sv mcl86_ucode.sv i8088.sv pcxt_cpu_bus.sv pcxt_cpu.sv pcxt_memory.sv pcxt_video_de25.sv pcxt_video_dynamic_de25.sv
    pcxt_video_output.sv pcxt_machine.sv pcxt_machine_settings.sv pcxt_disk_loader.sv pcxt_sector_writer.sv pcxt_media_arbiter.sv
    pcxt_system.sv pcxt_media_writer.sv pcxt_media.sv pcxt_boot_media_gate.sv pcxt_hid_keyboard.sv
    pcxt_xt_keyboard_bat.sv KF8259.sv KF8259_Bus_Control_Logic.sv KF8259_Control_Logic.sv KF8259_In_Service.sv
    KF8259_Interrupt_Request.sv KF8259_Priority_Resolver.sv KF8253.sv KF8253_Control_Logic.sv
    KF8253_Counter.sv fes_dma8237.sv} {
    add_fileset_file $file SYSTEM_VERILOG PATH $file
}
# The 8088 control ROM's byte lanes and its generated facts, read by name
# during elaboration.
foreach file {mcl86_ucode_byte0.mem mcl86_ucode_byte1.mem mcl86_ucode_byte2.mem mcl86_ucode_byte3.mem} {
    add_fileset_file $file OTHER PATH $file
}
foreach file {fes_de25_computer.vh fes_de25_scanout.vh} {
    add_fileset_file generated/$file VERILOG_INCLUDE PATH generated/$file
}
# Portable shared RTL includes these by basename, as the DE10 build's include
# path does. Qsys must emit them beside that RTL, rather than in generated/.
foreach file {fes_computer.vh fes_application.vh fes_display_plane.vh fes_firmware_upload.vh
    fes_video_part.vh fes_pcxt_isa.vh} {
    add_fileset_file $file VERILOG_INCLUDE PATH generated/$file
}
foreach file {mcl86_facts.vh KF8255_Definitions.svh KF8253_Definitions.svh KF8259_Common_Package.svh} {
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
# Board SDRAM pins and the machine PLL's three SDRAM clocks. The top level
# owns the DQ tri-state.
add_interface sdram conduit end
set_interface_property sdram associatedClock machine_clk
foreach {port role direction width} {dram_clk clk Output 1 dram_cke cke Output 1 dram_addr addr Output 13
    dram_ba ba Output 2 dram_cs_n cs_n Output 2 dram_dqm dqm Output 2 dram_ras_n ras_n Output 1
    dram_cas_n cas_n Output 1 dram_we_n we_n Output 1 dram_dq_i dq_i Input 16 dram_dq_o dq_o Output 16
    dram_dq_oe dq_oe Output 1 dram_clk_forward clk_forward Input 1 dram_clk_capture clk_capture Input 1
    dram_clk_handoff clk_handoff Input 1} {
    add_interface_port sdram $port $role $direction $width
}
# Read-only diagnostic observations. These never become a second production
# control or memory interface.
add_interface observations conduit end
foreach {port direction width} {video Output 28 ui_active Output 1 display_faulted Output 1 display_quiesced Output 1} {
    add_interface_port observations $port $port $direction $width
    set_port_property $port TERMINATION true
}
