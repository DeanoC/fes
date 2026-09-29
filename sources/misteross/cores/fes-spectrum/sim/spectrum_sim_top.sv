// SPDX-License-Identifier: GPL-2.0-or-later
// Machine simulation: Z80, ULA, tape, video and probe cards in sockets 1 and 3.
`include "spectrum_bus.vh"

module spectrum_sim_top (
    input  wire        clk_sys,
    input  wire        pixel_clk,
    input  wire        reset,
    input  wire [39:0] matrix,
    input  wire [4:0]  kempston,
    input  wire [1:0]  unit_state,
    input  wire [31:0] unit_size,
    output wire [7:0]  red,
    output wire [7:0]  green,
    output wire [7:0]  blue,
    output wire        de,
    output wire [10:0] pixel_x,
    output wire [9:0]  pixel_y,
    output wire        ear,
    output wire        cpu_cycle,
    output wire [2:0]  border,
    output wire [7:0]  sig8000,
    output wire [7:0]  sig8001,
    output wire [7:0]  sig8002,
    output wire [7:0]  sig8003,
    output wire [7:0]  sig8004
);
    wire [`SP_BUS_REQ-1:0] bus_request;
    wire [`SP_BUS_RSP-1:0] response1, response2, response3, response4;
    wire flash_on, speaker;
    wire [15:0] video_addr;
    wire [7:0] video_data;
    wire signed [15:0] slot_audio;

    spectrum_machine machine (
        .clk_sys(clk_sys), .reset(reset), .matrix(matrix), .kempston(kempston),
        .unit_state(unit_state), .unit_size(unit_size),
        .media_write_addr(16'h0000), .media_write_data(16'h0000), .media_write_enable(2'b00),
        .bus_request(bus_request),
        .response1(response1), .response2(response2),
        .response3(response3), .response4(response4),
        .border(border), .flash_on(flash_on), .speaker(speaker), .ear(ear),
        .cpu_cycle(cpu_cycle), .slot_audio(slot_audio),
        .video_clk(pixel_clk), .video_addr(video_addr), .video_data(video_data),
        .sig8000(sig8000), .sig8001(sig8001), .sig8002(sig8002),
        .sig8003(sig8003), .sig8004(sig8004)
    );

    wire [`SP_BUS_REQ-1:0] slot1_plug_request, slot3_plug_request;
    wire [`SP_BUS_RSP-1:0] slot1_plug_response, slot3_plug_response;
    spectrum_slot_socket1 slot1 (
        .clock(clk_sys), .request(bus_request), .response(response1),
        .plug_request(slot1_plug_request), .plug_response(slot1_plug_response)
    );
    spectrum_slot_socket2 slot2 (
        .clock(clk_sys), .request(bus_request), .response(response2),
        .plug_request(), .plug_response(`SP_BUS_RSP'd0)
    );
    spectrum_slot_socket3 slot3 (
        .clock(clk_sys), .request(bus_request), .response(response3),
        .plug_request(slot3_plug_request), .plug_response(slot3_plug_response)
    );
    spectrum_slot_socket4 slot4 (
        .clock(clk_sys), .request(bus_request), .response(response4),
        .plug_request(), .plug_response(`SP_BUS_RSP'd0)
    );
    cart #(.SOCKET(1)) probe1 (
        .FPGA_CLK1_50(clk_sys), .plug_addr(slot1_plug_request), .plug_rdata(slot1_plug_response)
    );
    cart #(.SOCKET(3)) probe3 (
        .FPGA_CLK1_50(clk_sys), .plug_addr(slot3_plug_request), .plug_rdata(slot3_plug_response)
    );

    spectrum_video video (
        .pixel_clk(pixel_clk), .border(border), .flash_on(flash_on),
        .ram_addr(video_addr), .ram_data(video_data),
        .red(red), .green(green), .blue(blue),
        .de(de), .hsync(), .vsync(), .pixel_x(pixel_x), .pixel_y(pixel_y)
    );
endmodule
