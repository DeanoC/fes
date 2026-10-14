// SPDX-License-Identifier: MIT
// Real-firmware diagnostic with vacant cartridge sockets. Media is preloaded
// into the actual disk store; no CPU, RAM, CIA or IEC behavior is substituted.
`include "c64_bus.vh"
module c64_boot_sim_top (
`ifdef FES_C64_REAL_DRIVE_SIM
    input wire drive_flux,
    output wire drive_ref_ce,
    output wire [7:0] drive_pins, drive_ddr,
    output wire [15:0] drive_pc,
`endif
    input wire clk_sys, pixel_clk, reset,
    input wire [143:0] keyboard_rows,
    output wire [7:0] red, green, blue,
    output wire de, vsync,
    output wire signed [15:0] audio_sample,
    output wire [15:0] debug_pc
);
    c64_machine machine (
`ifdef FES_C64_REAL_DRIVE_SIM
        .drive_flux(drive_flux), .drive_ref_ce(drive_ref_ce),
        .drive_pins(drive_pins), .drive_ddr(drive_ddr), .drive_pc(drive_pc),
`endif
        .clk_sys(clk_sys), .reset(reset), .video_clk(pixel_clk),
        .keyboard_rows(keyboard_rows), .controller_buttons(16'd0),
        .media_write_addr(18'd0), .media_write_data(16'd0),
        .media_write_enable(2'b00), .disk_ready(1'b1),
        .slot1_response(28'd0), .slot2_response(28'd0),
        .slot1_request(), .slot2_request(),
        .red(red), .green(green), .blue(blue), .de(de),
        .hsync(), .vsync(vsync), .audio_sample(audio_sample),
        .debug_stage(), .debug_error(), .debug_iec(), .debug_pc(debug_pc)
    );
endmodule
