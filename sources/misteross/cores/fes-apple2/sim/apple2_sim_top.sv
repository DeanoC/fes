// SPDX-License-Identifier: GPL-2.0-or-later
// Simulation board: machine, built-in slot 6 Disk II card and drive, video,
// and a disk store preloaded from the synthetic test image. The testbench
// drives both clocks, host reset, disk presence and keyboard events.
`include "apple2_bus.vh"

module apple2_sim_top (
    input  wire        clk_sys,
    input  wire        pixel_clk,
    input  wire        reset,
    input  wire        reset_key,
    input  wire        key_event,
    input  wire [6:0]  key_code,
    input  wire        disk_present,
    output wire [7:0]  red,
    output wire [7:0]  green,
    output wire [7:0]  blue,
    output wire        de,
    output wire        hsync,
    output wire        vsync,
    output wire [15:0] bus_addr,
    output wire        bus_cycle,
    output wire [7:0]  bus_data,
    output wire        bus_write,
    output wire        speaker,
    output wire [7:0]  quarter_track,
    output wire        motor_on
);
    wire [`A2_BUS_REQ-1:0] slot_request;
    wire [7:0] slot_devsel;
    wire [7:0] slot_iosel;
    wire [`A2_BUS_RSP-1:0] disk_response;
    wire text_mode, mixed_mode, page2, hires_mode;
    wire [15:0] video_addr;
    wire [7:0] video_data;

    /* verilator lint_off PINCONNECTEMPTY */
    apple2_machine machine (
        .clk_sys(clk_sys), .reset(reset), .reset_key(reset_key),
        .key_event(key_event), .key_code(key_code),
        .buttons(3'b000), .paddles(32'h80808080), .cassette_in(1'b0),
        .slot_request(slot_request), .slot_devsel(slot_devsel), .slot_iosel(slot_iosel),
        .slot_response({`A2_BUS_RSP'd0, disk_response, {6{`A2_BUS_RSP'd0}}}),
        .video_text(text_mode), .video_mixed(mixed_mode),
        .video_page2(page2), .video_hires(hires_mode), .annunciators(),
        .video_clk(pixel_clk), .video_addr(video_addr), .video_data(video_data),
        .speaker(speaker), .cassette_out(), .slot_audio(),
        .debug_pc_addr(bus_addr), .cpu_cycle(bus_cycle),
        .debug_bus_data(bus_data), .debug_bus_write(bus_write)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    wire [3:0] phases;
    wire drive2;
    wire bit_ce, read_bit, write_protect;
    apple2_disk2_card disk_card (
        .clk(clk_sys), .request(slot_request), .devsel(slot_devsel[6]),
        .response(disk_response), .phases(phases), .motor_on(motor_on),
        .drive2(drive2), .bit_ce(bit_ce), .read_bit(read_bit),
        .write_protect(write_protect)
    );

    wire [17:0] media_addr;
    reg [7:0] media_q;
    reg [7:0] disk_image [0:143359];
    initial $readmemh("build/diagnostics/fes-apple2/disk.hex", disk_image);
    always @(posedge clk_sys)
        media_q <= disk_image[media_addr];

    /* verilator lint_off PINCONNECTEMPTY */
    apple2_disk2_drive drive (
        .clk(clk_sys), .reset(reset), .cpu_ce(bus_cycle), .phases(phases),
        .motor_on(motor_on), .drive2(drive2), .disk_present(disk_present),
        .media_addr(media_addr), .media_q(media_q), .bit_ce(bit_ce),
        .read_bit(read_bit), .write_protect(write_protect), .track(),
        .quarter_track(quarter_track)
    );

    apple2_video video (
        .pixel_clk(pixel_clk), .text_mode(text_mode), .mixed_mode(mixed_mode),
        .page2(page2), .hires_mode(hires_mode), .ram_addr(video_addr),
        .ram_data(video_data), .red(red), .green(green), .blue(blue),
        .de(de), .hsync(hsync), .vsync(vsync), .frame_tick()
    );
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
