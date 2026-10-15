// SPDX-License-Identifier: MIT
// Real-firmware diagnostic with vacant cartridge sockets. Media is preloaded
// into the actual disk store; no CPU, RAM, CIA or IEC behavior is substituted.
`include "c64_bus.vh"
module c64_boot_sim_top (
`ifndef FES_C64_EXTERNAL_MEDIUM_SIM
    input wire medium_ready,
    input wire [17:0] medium_write_addr,
    input wire [15:0] medium_write_data,
    input wire [1:0] medium_write_enable,
    output wire drive_flux, medium_cache_ready,
    output wire [6:0] medium_halftrack,
    output wire [21:0] medium_rotation,
    output wire [12:0] medium_byte_addr,
    output wire [7:0] medium_byte,
`else
    input wire drive_flux,
`endif
    output wire drive_ref_ce,
    output wire [7:0] drive_pins, drive_ddr,
    output wire [15:0] drive_pc,
    input wire clk_sys, pixel_clk, reset,
    input wire [143:0] keyboard_rows,
    output wire [7:0] red, green, blue,
    output wire de, vsync,
    output wire signed [15:0] audio_sample,
    output wire [15:0] debug_pc
);
    assign drive_ref_ce=machine.drive_ref_ce;
    assign drive_pins=machine.drive_pins;
    assign drive_ddr=machine.drive_ddr;
    assign drive_pc=machine.drive_pc;
`ifndef FES_C64_EXTERNAL_MEDIUM_SIM
    assign drive_flux=machine.drive_flux;
    assign medium_cache_ready=machine.medium_cache_ready;
    assign medium_halftrack=machine.medium_halftrack;
    assign medium_rotation=machine.medium_rotation;
    assign medium_byte_addr=machine.medium_byte_addr;
    assign medium_byte=machine.medium_byte;
`endif
    c64_machine machine (
`ifdef FES_C64_EXTERNAL_MEDIUM_SIM
        .drive_flux(drive_flux),
`endif
        .clk_sys(clk_sys), .reset(reset), .video_clk(pixel_clk),
        .keyboard_rows(keyboard_rows), .controller_buttons(16'd0),
`ifdef FES_C64_EXTERNAL_MEDIUM_SIM
        .media_write_addr(18'd0), .media_write_data(16'd0),
        .media_write_enable(2'b00), .disk_ready(1'b1),
`else
        .media_write_addr(medium_write_addr), .media_write_data(medium_write_data),
        .media_write_enable(medium_write_enable), .disk_ready(medium_ready),
`endif
        .slot1_response(28'd0), .slot2_response(28'd0),
        .slot1_request(), .slot2_request(),
        .red(red), .green(green), .blue(blue), .de(de),
        .hsync(), .vsync(vsync), .audio_sample(audio_sample),
        .debug_stage(), .debug_error(), .debug_iec(), .debug_pc(debug_pc)
    );
endmodule
