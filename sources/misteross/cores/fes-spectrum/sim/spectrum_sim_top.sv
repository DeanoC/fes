// SPDX-License-Identifier: GPL-2.0-or-later
// Machine simulation: Z80, ULA, memory, tape and probe cards in sockets 1 and
// 3. The build model is the SPECTRUM_MODEL_* define, as in the producer.
`include "spectrum_bus.vh"

module spectrum_sim_top (
    input  wire        clk_sys,
    input  wire        reset,
    input  wire [39:0] matrix,
    input  wire [4:0]  kempston,
    input  wire [1:0]  unit_state,
    input  wire [31:0] unit_size,
    output wire        pixel_we,
    output wire [7:0]  pixel_row,
    output wire [5:0]  pixel_chunk,
    output wire [31:0] pixel_data,
    output wire        frame_done,
    output wire        tick,
    output wire        ear,
    output wire        speaker,
    output wire [7:0]  ay_pcm,
    output wire        cpu_retired,
    output wire        cpu_illegal,
    output wire        frame_int_n,
    output wire [15:0] cpu_pc
);
    wire [`SP_BUS_REQ-1:0] bus_request;
    wire [`SP_BUS_RSP-1:0] response1, response2, response3, response4;

    spectrum_machine machine (
        .clk_sys(clk_sys), .reset(reset), .matrix(matrix), .kempston(kempston),
        .unit_state(unit_state), .unit_size(unit_size),
        .media_write_addr(16'h0000), .media_write_data(16'h0000), .media_write_enable(2'b00),
        .bus_request(bus_request),
        .response1(response1), .response2(response2),
        .response3(response3), .response4(response4),
        .pixel_we(pixel_we), .pixel_row(pixel_row), .pixel_chunk(pixel_chunk),
        .pixel_data(pixel_data), .frame_done(frame_done), .tick(tick),
        .speaker(speaker), .mic(), .ear(ear), .ay_pcm(ay_pcm), .slot_audio(),
        .cpu_retired(cpu_retired), .cpu_illegal(cpu_illegal), .cpu_halted(),
        .frame_int_n(frame_int_n), .cpu_pc(cpu_pc)
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
endmodule
