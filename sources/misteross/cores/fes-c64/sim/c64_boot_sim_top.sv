// SPDX-License-Identifier: MIT
// Real-firmware diagnostic with vacant cartridge sockets. Media is preloaded
// into the actual disk store; no CPU, RAM, CIA or IEC behavior is substituted.
`include "c64_bus.vh"
module c64_boot_sim_top (
    input wire clk_sys, pixel_clk, reset,
    output wire [7:0] red, green, blue,
    output wire de, vsync,
    output wire signed [15:0] audio_sample,
    output wire [15:0] debug_pc
);
    c64_machine machine (
        .clk_sys(clk_sys), .reset(reset), .video_clk(pixel_clk),
        .keyboard_rows(144'd0), .controller_buttons(16'd0),
        .media_write_addr(18'd0), .media_write_data(16'd0),
        .media_write_enable(2'b00), .disk_ready(1'b1),
        .slot1_response(28'd0), .slot2_response(28'd0),
        .slot1_request(), .slot2_request(),
        .red(red), .green(green), .blue(blue), .de(de),
        .hsync(), .vsync(vsync), .audio_sample(audio_sample),
        .debug_stage(), .debug_error(), .debug_iec(), .debug_pc(debug_pc)
    );
endmodule
