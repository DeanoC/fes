// SPDX-License-Identifier: GPL-2.0-or-later
// Machine simulation: Z80, ULA, memory, tape and a probe card in the edge
// socket. The build model is the SPECTRUM_MODEL_* define, as in the producer. The
// bench plays the media store's part on the tape stream and the disk fetch port.
`include "spectrum_bus.vh"
`include "spectrum_model.vh"

module spectrum_sim_top (
    input  wire        clk_sys,
    input  wire        reset,
    input  wire [39:0] matrix,
    input  wire [4:0]  kempston,
    input  wire        tape_ready,
    input  wire [24:0] tape_size,
    output wire        tape_stream_enable,
    input  wire        tape_stream_valid,
    input  wire [7:0]  tape_stream_data,
    output wire        tape_stream_take,
    output wire        tape_playing,
    input  wire        turbo_always,
    output wire        turbo,
    input  wire        disk_ready,
    input  wire [22:0] disk_size,
    output wire        disk_fetch,
    output wire [21:0] disk_fetch_addr,
    input  wire        disk_fetch_ready,
    input  wire [7:0]  disk_fetch_data,
    output wire        pixel_we,
    output wire [7:0]  pixel_row,
    output wire [5:0]  pixel_chunk,
    output wire [15:0] pixel_data,
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
    wire [`SP_BUS_RSP-1:0] response1;

`ifdef SPECTRUM_ROM_64K
    localparam ROM_FILE = "build/diagnostics/fes-spectrum/firmware-64k.hex";
`elsif SPECTRUM_ROM_48K
    localparam ROM_FILE = "build/diagnostics/fes-spectrum/firmware-48k.hex";
`elsif SPECTRUM_ROM_32K
    localparam ROM_FILE = "build/diagnostics/fes-spectrum/firmware-32k.hex";
`else
    localparam ROM_FILE = "build/diagnostics/fes-spectrum/firmware.hex";
`endif
    spectrum_machine #(.ROM_FILE(ROM_FILE)) machine (
        .clk_sys(clk_sys), .reset(reset), .matrix(matrix), .kempston(kempston),
        .tape_ready(tape_ready), .tape_size(tape_size),
        .tape_stream_enable(tape_stream_enable), .tape_stream_valid(tape_stream_valid),
        .tape_stream_data(tape_stream_data), .tape_stream_take(tape_stream_take),
        .tape_playing(tape_playing), .turbo_always(turbo_always), .turbo(turbo),
        .disk_ready(disk_ready), .disk_size(disk_size), .disk_fetch(disk_fetch),
        .disk_fetch_addr(disk_fetch_addr), .disk_fetch_ready(disk_fetch_ready),
        .disk_fetch_data(disk_fetch_data),
        .bus_request(bus_request),
        .response1(response1),
        .pixel_we(pixel_we), .pixel_row(pixel_row), .pixel_chunk(pixel_chunk),
        .pixel_data(pixel_data), .frame_done(frame_done), .tick(tick),
        .speaker(speaker), .mic(), .ear(ear), .ay_pcm(ay_pcm), .slot_audio(),
        .cpu_retired(cpu_retired), .cpu_illegal(cpu_illegal), .cpu_halted(),
        .frame_int_n(frame_int_n), .cpu_pc(cpu_pc)
    );

    wire [`SP_BUS_REQ-1:0] slot1_plug_request;
    wire [`SP_BUS_RSP-1:0] slot1_plug_response;
    spectrum_slot_socket1 slot1 (
        .clock(clk_sys), .request(bus_request), .response(response1),
        .plug_request(slot1_plug_request), .plug_response(slot1_plug_response)
    );
    cart probe1 (
        .FPGA_CLK1_50(clk_sys), .plug_addr(slot1_plug_request), .plug_rdata(slot1_plug_response)
    );
endmodule
