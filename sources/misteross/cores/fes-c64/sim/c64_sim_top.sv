// SPDX-License-Identifier: GPL-2.0-or-later
// Machine, both cartridge sockets and the open probe cards. The disk image
// is the synthetic D64 preloaded into the disk store.
`include "c64_bus.vh"

module c64_sim_top (
    input  wire        clk_sys,
    input  wire        pixel_clk,
    input  wire        reset,
    input  wire [143:0] keyboard_rows,
    input  wire [15:0] controller_buttons,
    output wire [7:0]  red,
    output wire [7:0]  green,
    output wire [7:0]  blue,
    output wire        de,
    output wire        hsync,
    output wire        vsync,
    output wire signed [15:0] audio_sample,
    output wire [7:0]  debug_stage,
    output wire [7:0]  debug_error,
    output wire [7:0]  debug_iec,
    output wire [15:0] debug_pc
);
    wire [`C64_BUS_REQ-1:0] slot1_request, slot2_request;
    wire [`C64_BUS_REQ-1:0] slot1_plug_request, slot2_plug_request;
    wire [`C64_BUS_RSP-1:0] slot1_response, slot2_response;
    wire [`C64_BUS_RSP-1:0] slot1_plug_response, slot2_plug_response;

    c64_machine machine (
        .clk_sys(clk_sys), .reset(reset),
        .keyboard_rows(keyboard_rows), .controller_buttons(controller_buttons),
        .media_write_addr(18'd0), .media_write_data(16'd0), .media_write_enable(2'b00),
        .disk_ready(1'b1),
        .slot1_request(slot1_request), .slot2_request(slot2_request),
        .slot1_response(slot1_response), .slot2_response(slot2_response),
        .video_clk(pixel_clk),
        .red(red), .green(green), .blue(blue), .de(de), .hsync(hsync), .vsync(vsync),
        .audio_sample(audio_sample),
        .debug_stage(debug_stage), .debug_error(debug_error), .debug_iec(debug_iec),
        .debug_pc(debug_pc)
    );
    c64_slot_socket1 slot1 (
        .clock(clk_sys), .request(slot1_request), .response(slot1_response),
        .plug_request(slot1_plug_request), .plug_response(slot1_plug_response)
    );
    cart #(.MODE(0)) rom_card (
        .FPGA_CLK1_50(clk_sys), .plug_addr(slot1_plug_request), .plug_rdata(slot1_plug_response)
    );
    c64_slot_socket2 slot2 (
        .clock(clk_sys), .request(slot2_request), .response(slot2_response),
        .plug_request(slot2_plug_request), .plug_response(slot2_plug_response)
    );
    cart #(.MODE(1)) io_card (
        .FPGA_CLK1_50(clk_sys), .plug_addr(slot2_plug_request), .plug_rdata(slot2_plug_response)
    );
endmodule
