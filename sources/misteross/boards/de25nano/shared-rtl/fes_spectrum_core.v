// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
`include "spectrum_bus.vh"
`include "spectrum_model.vh"
// ZX Spectrum machine, control, media and capture for a board without linked
// ROM lanes or an HPS DDR media window (fes.computer 1.0 with
// fes.fabric.firmware-upload). The machine sources are the DE10 core's; this
// replaces its shell. clk_sys is the machine's 52.224 MHz. Firmware arrives
// through the mailbox front end before execution can be released, the tape
// (media unit 0) and, on the +2A/+3 and the Pentagon, the disk (media unit 1)
// live in board SDRAM, and the edge sockets are vacant.
// Complete 50 Hz frames leave as a 720p60 word for the session display.
// Board wrappers own clocks, HPS transport, the SDRAM controller and pads,
// HDMI and audio serialization. No FPGA-family primitive belongs here.
module fes_spectrum_core #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk_sys, pixel_clk, reset_hold,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire [31:0] fetch_count,
    output wire [15:0] fetch_address,
    output wire [27:0] machine_video,
    output wire signed [15:0] audio_sample,
    output wire display_request,
    output wire [6:0] display_opcode,
    output wire [7:0] display_index,
    output wire [15:0] display_argument,
    input wire display_response_valid, display_response_error,
    input wire [15:0] display_response_data,
    input wire display_quiesced,
    // Board SDRAM, one halfword a command (fes_de25_sdram).
    input wire sdram_reset,
    output wire [31:0] sdram_address,
    output wire sdram_read, sdram_write,
    output wire [15:0] sdram_writedata,
    output wire [1:0] sdram_byteenable,
    input wire sdram_waitrequest,
    input wire [15:0] sdram_readdata,
    input wire sdram_readdatavalid, sdram_writeresponsevalid
);
    localparam integer MODEL = `SPECTRUM_MODEL;
    localparam bit DISK = MODEL == 2 || MODEL == 3;
`ifdef SPECTRUM_ROM_64K
    localparam [31:0] FIRMWARE_BYTES = 32'd65536;
`elsif SPECTRUM_ROM_48K
    localparam [31:0] FIRMWARE_BYTES = 32'd49152;
`elsif SPECTRUM_ROM_32K
    localparam [31:0] FIRMWARE_BYTES = 32'd32768;
`else
    localparam [31:0] FIRMWARE_BYTES = 32'd16384;
`endif
    wire machine_reset = exec_reset || reset_hold;
    wire [31:0] mailbox_gpo, mailbox_gpi;
    wire [15:0] firmware_write_addr, firmware_write_data;
    wire [1:0] firmware_write_enable;
    fes_firmware_upload #(.AW(16), .BYTES(FIRMWARE_BYTES)) firmware (
        .clk(clk_sys), .gpo(gpo), .gpi(gpi),
        .mailbox_gpo(mailbox_gpo), .mailbox_gpi(mailbox_gpi), .exec_reset(exec_reset),
        .ready(), .write_addr(firmware_write_addr), .write_data(firmware_write_data),
        .write_enable(firmware_write_enable), .write_ready(1'b1)
    );

    wire [143:0] keyboard_rows;
    wire [15:0] controller_buttons;
    wire [23:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable, unit0_state, unit1_state;
    wire [31:0] unit0_size, unit1_size;
    wire media_write_ready, media_write_unit;
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1),
        .ENABLE_SPECTRUM_TAPE(1), .ENABLE_SESSION_DISPLAY(1), .ENABLE_HPS_DDR_WINDOW(0),
        .ENABLE_SPECTRUM_DISK(DISK),
        .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(24),
        .UNIT0_MIN(`FES_COMPUTER_SPECTRUM_TAPE_MIN_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_SPECTRUM_TAPE_MAX_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(mailbox_gpo), .build_id(BUILD_ID), .gpi(mailbox_gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(controller_buttons),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_busy(1'b0), .media_changed(1'b0), .media_reset(1'b0), .media_frozen(),
        .media_read_req(), .media_read_addr(), .media_read_ready(1'b0), .media_read_data(8'd0),
        .save_ready(1'b0), .save_busy(1'b0), .save_size(15'd0),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_unit(media_write_unit),
        .media_write_ready(media_write_ready),
        .unit0_state(unit0_state), .unit0_size(unit0_size),
        .unit1_state(unit1_state), .unit1_size(unit1_size),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid),
        .display_response_error(display_response_error),
        .display_response_data(display_response_data),
        .display_quiesced(display_quiesced),
        .storage_request(), .storage_opcode(), .storage_index(), .storage_argument(),
        .storage_response_valid(1'b0), .storage_response_error(1'b0), .storage_response_data(16'd0),
        .execution_accepted()
    );

    // Controller port 0 is a Kempston joystick (bit 0 right, 1 left, 2 down,
    // 3 up, 4 fire from A or B); its Start is ENTER and Select is SPACE.
    // Port 1 is Sinclair joystick 1: keys 6 left, 7 right, 8 down, 9 up, 0 fire.
    wire [39:0] keyboard_matrix;
    spectrum_keyboard keyboard (
        .clk(clk_sys), .reset(machine_reset), .rows(keyboard_rows), .matrix(keyboard_matrix)
    );
    wire [7:0] pad0 = controller_buttons[7:0];
    wire [7:0] pad1 = controller_buttons[15:8];
    wire [4:0] kempston = {pad0[4] | pad0[5], pad0[0], pad0[1], pad0[2], pad0[3]};
    reg [39:0] pad_keys;
    always @* begin
        pad_keys = 40'hffffffffff;
        if (pad0[7]) pad_keys[30] = 1'b0;          // ENTER
        if (pad0[6]) pad_keys[35] = 1'b0;          // SPACE
        if (pad1[2]) pad_keys[24] = 1'b0;          // 6
        if (pad1[3]) pad_keys[23] = 1'b0;          // 7
        if (pad1[1]) pad_keys[22] = 1'b0;          // 8
        if (pad1[0]) pad_keys[21] = 1'b0;          // 9
        if (pad1[4] | pad1[5]) pad_keys[20] = 1'b0; // 0
    end
    wire [39:0] matrix = keyboard_matrix & pad_keys;

    // One store holds both units. The tape image is the first 16 MiB of the
    // SDRAM and the deck reads it back as a byte stream from byte 0, so the
    // stream never leaves unit 0. The disk image is the next 16 MiB (the unit
    // number is the top address bit) and the disk controller reads its tracks
    // a byte at a time through the fetch port. A model without a disk never
    // writes unit 1 or fetches.
    wire tape_stream_enable, tape_stream_valid, tape_stream_take;
    wire [7:0] tape_stream_data;
    wire disk_fetch, disk_fetch_ready;
    wire [21:0] disk_fetch_addr;
    wire [7:0] disk_fetch_data;
    fes_media_sdram #(.BASE(32'd0), .AW(25)) media_store (
        .clk(clk_sys), .reset(sdram_reset),
        .write_addr({media_write_unit, media_write_addr}), .write_data(media_write_data),
        .write_enable(media_write_enable), .write_ready(media_write_ready),
        .stream_enable(tape_stream_enable), .stream_bytes({1'b0, unit0_size[24:0]}),
        .stream_valid(tape_stream_valid), .stream_data(tape_stream_data),
        .stream_take(tape_stream_take),
        .fetch(DISK && disk_fetch), .fetch_addr({3'b100, disk_fetch_addr}),
        .fetch_ready(disk_fetch_ready), .fetch_data(disk_fetch_data),
        .address(sdram_address), .read(sdram_read), .write(sdram_write),
        .writedata(sdram_writedata), .byteenable(sdram_byteenable),
        .waitrequest(sdram_waitrequest), .readdata(sdram_readdata),
        .readdatavalid(sdram_readdatavalid), .writeresponsevalid(sdram_writeresponsevalid)
    );

    wire pixel_we, frame_done, tick, turbo, speaker, mic, ear;
    wire [`SP_BUS_REQ-1:0] edge_bus;
    reg [31:0] observed_fetch_count = 32'd0;
    reg [15:0] observed_fetch_address = 16'd0;
    // The edge bus strobe is one request, including when the CPU waits.
    // Exclude refresh, interrupt acknowledge, writes and reset.
    always @(posedge clk_sys)
        if (!machine_reset && edge_bus[`SP_BUS_STROBE] && edge_bus[`SP_BUS_M1] &&
            edge_bus[`SP_BUS_MREQ] && edge_bus[`SP_BUS_RD]) begin
            observed_fetch_count <= observed_fetch_count + 32'd1;
            observed_fetch_address <= edge_bus[`SP_BUS_A];
        end
    assign fetch_count = observed_fetch_count;
    assign fetch_address = observed_fetch_address;
    wire [7:0] pixel_row, ay_pcm;
    wire [5:0] pixel_chunk;
    wire [15:0] pixel_data;
    wire signed [15:0] slot_audio;
    spectrum_machine machine (
        .clk_sys(clk_sys), .reset(machine_reset), .matrix(matrix), .kempston(kempston),
        .tape_ready(unit0_state == 2'd3), .tape_size(unit0_size[24:0]),
        .disk_ready(DISK && unit1_state == 2'd3), .disk_size(unit1_size[22:0]),
        .disk_fetch(disk_fetch), .disk_fetch_addr(disk_fetch_addr),
        .disk_fetch_ready(disk_fetch_ready), .disk_fetch_data(disk_fetch_data),
        .tape_stream_enable(tape_stream_enable), .tape_stream_valid(tape_stream_valid),
        .tape_stream_data(tape_stream_data), .tape_stream_take(tape_stream_take),
        .tape_playing(), .turbo_always(1'b0), .turbo(turbo),
        .bus_request(edge_bus), .response1(`SP_BUS_RSP'd0),
        .pixel_we(pixel_we), .pixel_row(pixel_row), .pixel_chunk(pixel_chunk),
        .pixel_data(pixel_data), .frame_done(frame_done),
        .tick(tick), .speaker(speaker), .mic(mic), .ear(ear), .ay_pcm(ay_pcm),
        .slot_audio(slot_audio),
        .cpu_retired(), .cpu_illegal(), .cpu_halted(), .frame_int_n(), .cpu_pc(),
        .firmware_write_addr(firmware_write_addr), .firmware_write_data(firmware_write_data),
        .firmware_write_enable(firmware_write_enable)
    );

    wire [7:0] red, green, blue;
    wire machine_de, machine_hs, machine_vs;
    spectrum_video video (
        .clk_sys(clk_sys), .pixel_we(pixel_we), .pixel_row(pixel_row),
        .pixel_chunk(pixel_chunk), .pixel_data(pixel_data), .frame_done(frame_done),
        .pixel_clk(pixel_clk), .red(red), .green(green), .blue(blue),
        .de(machine_de), .hsync(machine_hs), .vsync(machine_vs)
    );
    assign machine_video = {1'b1, machine_vs, machine_hs, machine_de, red, green, blue};

    spectrum_audio audio_mix (
        .clk(clk_sys), .reset(machine_reset), .tick(tick), .mute(turbo), .speaker(speaker), .mic(mic),
        .ear(ear), .ay_pcm(ay_pcm), .slot_audio(slot_audio), .sample(audio_sample)
    );
endmodule
