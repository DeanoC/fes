// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
`include "fes_computer.vh"
`include "spectrum_bus.vh"
`include "spectrum_model.vh"
`ifndef FES_SPECTRUM_BUILD_ID
`define FES_SPECTRUM_BUILD_ID 128'h00000000000000000000000000000000
`endif

// DE10-Nano shell for the FES ZX Spectrum models (fes.computer 1.0). The
// mailbox, machine, tape and edge sockets run at 52.224 MHz; the ULA derives
// the model's T state from it, 5 clocks per T state while a tape loads in
// turbo, when the sound is muted. Complete 50 Hz frames are shown 3x in 720p60
// through the shared session-display plane. Audio leaves through 48 kHz I2S.
// Sockets 1-4 are physical and vacant here; a linked card replaces the
// vacant response. HPS DDR port 0 feeds the plane; port 1 holds the tape and,
// on the Pentagon and +2A/+3, port 2 the disk (media unit 1).
module top #(
    parameter [127:0] BUILD_ID = `FES_SPECTRUM_BUILD_ID
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA,
    output wire        HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire clk_sys;
    wire pixel_clk, pixel_locked;
    wire audio_clk, audio_locked;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;

    spectrum_system_pll system_clock (
        .refclk(FPGA_CLK1_50), .rst(1'b0),
        .outclk_0(clk_sys), .audio_clk(audio_clk), .locked(audio_locked)
    );
    spectrum_pixel_pll video_clock (
        .refclk(FPGA_CLK1_50), .rst(1'b0), .outclk_0(pixel_clk), .locked(pixel_locked)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps), .gp_out(hps_to_fpga)
    );

    wire hdmi_scl_low, hdmi_sda_low, hdmi_scl_in, hdmi_sda_in;
    MISTRAL_IO hdmi_scl_pad (.I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO hdmi_sda_pad (.I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in),
        .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );

    wire exec_reset;
    wire [143:0] keyboard_rows;
    wire [15:0] controller_buttons;
    wire [23:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable;
    wire media_write_ready;
    wire [1:0] unit0_state, unit1_state;
    wire [31:0] unit0_size, unit1_size;
    wire media_write_unit;
    localparam bit DISK = `SPECTRUM_MODEL == 3 || `SPECTRUM_MODEL == 2;
    wire display_request, display_response_valid, display_response_error, display_quiesced;
    wire [6:0] display_opcode;
    wire [7:0] display_index;
    wire [15:0] display_argument, display_response_data;
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1),
        .ENABLE_SPECTRUM_TAPE(1), .ENABLE_SESSION_DISPLAY(1), .ENABLE_SPECTRUM_DISK(DISK),
        .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(24),
        .UNIT0_MIN(`FES_COMPUTER_SPECTRUM_TAPE_MIN_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_SPECTRUM_TAPE_MAX_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(controller_buttons),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_busy(1'b0), .media_changed(1'b0), .media_frozen(),
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
        .display_quiesced(display_quiesced)
    );

    // Controller port 0 is a Kempston joystick (bit 0 right, 1 left, 2 down,
    // 3 up, 4 fire from A or B); its Start is ENTER and Select is SPACE.
    // Port 1 is Sinclair joystick 1: keys 6 left, 7 right, 8 down, 9 up, 0 fire.
    wire [39:0] keyboard_matrix;
    spectrum_keyboard keyboard (
        .clk(clk_sys), .reset(exec_reset), .rows(keyboard_rows), .matrix(keyboard_matrix)
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

    // The tape image lives in the DDR window at offset 16 MiB, clear of the
    // plane's two 4 MiB slots. The deck reads it back as a byte stream.
    wire tape_stream_enable, tape_stream_valid, tape_stream_take;
    wire [7:0] tape_stream_data;
    wire ddr_hold, ddr0_reset, ddr0_drained, ddr0_read, ddr0_waitrequest, ddr0_readdatavalid;
    wire [27:0] ddr0_address;
    wire [7:0] ddr0_burstcount;
    wire [127:0] ddr0_readdata;
    wire ddr1_reset, ddr1_read, ddr1_write, ddr1_waitrequest, ddr1_readdatavalid;
    wire [28:0] ddr1_address;
    wire [7:0] ddr1_burstcount, ddr1_byteenable;
    wire [63:0] ddr1_writedata, ddr1_readdata;
    wire tape_write_ready, disk_write_ready;
    assign media_write_ready = tape_write_ready | disk_write_ready;
    fes_media_ddr #(
        .BASE(`FES_APPLICATION_HPS_DDR_WINDOW_BASE + 32'h01000000), .AW(24)
    ) tape_store (
        .clk(clk_sys), .reset(ddr1_reset),
        .write_addr(media_write_addr), .write_data(media_write_data),
        .write_enable(media_write_unit ? 2'b00 : media_write_enable),
        .write_ready(tape_write_ready),
        .stream_enable(tape_stream_enable), .stream_bytes(unit0_size[24:0]),
        .stream_valid(tape_stream_valid), .stream_data(tape_stream_data),
        .stream_take(tape_stream_take),
        .fetch(1'b0), .fetch_addr(24'd0), .fetch_ready(), .fetch_data(),
        .address(ddr1_address), .burstcount(ddr1_burstcount), .read(ddr1_read),
        .write(ddr1_write), .writedata(ddr1_writedata), .byteenable(ddr1_byteenable),
        .waitrequest(ddr1_waitrequest), .readdata(ddr1_readdata),
        .readdatavalid(ddr1_readdatavalid)
    );

    // The disk image lives at window offset 32 MiB; the disk controller
    // reads its tracks a byte at a time through the fetch port.
    wire disk_fetch, disk_fetch_ready;
    wire [21:0] disk_fetch_addr;
    wire [7:0] disk_fetch_data;
    wire ddr2_reset, ddr2_read, ddr2_write, ddr2_waitrequest, ddr2_readdatavalid;
    wire [28:0] ddr2_address;
    wire [7:0] ddr2_burstcount, ddr2_byteenable;
    wire [63:0] ddr2_writedata, ddr2_readdata;
    generate if (DISK) begin : disk
        fes_media_ddr #(
            .BASE(`FES_APPLICATION_HPS_DDR_WINDOW_BASE + 32'h02000000), .AW(22)
        ) disk_store (
            .clk(clk_sys), .reset(ddr2_reset),
            .write_addr(media_write_addr[21:0]), .write_data(media_write_data),
            .write_enable(media_write_unit ? media_write_enable : 2'b00),
            .write_ready(disk_write_ready),
            .stream_enable(1'b0), .stream_bytes(23'd0), .stream_valid(), .stream_data(),
            .stream_take(1'b0),
            .fetch(disk_fetch), .fetch_addr(disk_fetch_addr),
            .fetch_ready(disk_fetch_ready), .fetch_data(disk_fetch_data),
            .address(ddr2_address), .burstcount(ddr2_burstcount), .read(ddr2_read),
            .write(ddr2_write), .writedata(ddr2_writedata), .byteenable(ddr2_byteenable),
            .waitrequest(ddr2_waitrequest), .readdata(ddr2_readdata),
            .readdatavalid(ddr2_readdatavalid)
        );
    end else begin : no_disk
        assign disk_write_ready = 1'b0;
        assign disk_fetch_ready = 1'b0;
        assign disk_fetch_data = 8'd0;
        assign ddr2_address = 29'd0;
        assign ddr2_burstcount = 8'd0;
        assign ddr2_read = 1'b0;
        assign ddr2_write = 1'b0;
        assign ddr2_writedata = 64'd0;
        assign ddr2_byteenable = 8'd0;
    end endgenerate

    wire [`SP_BUS_REQ-1:0] bus_request;
    wire [`SP_BUS_RSP-1:0] response1;
    wire pixel_we, frame_done, tick, turbo, speaker, mic, ear;
    wire [7:0] pixel_row, ay_pcm;
    wire [5:0] pixel_chunk;
    wire [15:0] pixel_data;
    wire signed [15:0] slot_audio;
    spectrum_machine machine (
        .clk_sys(clk_sys), .reset(exec_reset), .matrix(matrix), .kempston(kempston),
        .tape_ready(unit0_state == 2'd3), .tape_size(unit0_size[24:0]),
        .disk_ready(unit1_state == 2'd3), .disk_size(unit1_size[22:0]),
        .disk_fetch(disk_fetch), .disk_fetch_addr(disk_fetch_addr),
        .disk_fetch_ready(disk_fetch_ready), .disk_fetch_data(disk_fetch_data),
        .tape_stream_enable(tape_stream_enable), .tape_stream_valid(tape_stream_valid),
        .tape_stream_data(tape_stream_data), .tape_stream_take(tape_stream_take),
        .tape_playing(), .turbo_always(1'b0), .turbo(turbo),
        .bus_request(bus_request),
        .response1(response1),
        .pixel_we(pixel_we), .pixel_row(pixel_row), .pixel_chunk(pixel_chunk),
        .pixel_data(pixel_data), .frame_done(frame_done),
        .tick(tick), .speaker(speaker), .mic(mic), .ear(ear), .ay_pcm(ay_pcm),
        .slot_audio(slot_audio),
        .cpu_retired(), .cpu_illegal(), .cpu_halted(), .frame_int_n(), .cpu_pc()
    );

    (* keep *) wire [`SP_BUS_REQ-1:0] slot1_plug_request;
    spectrum_slot_socket1 slot1 (
        .clock(clk_sys), .request(bus_request), .response(response1),
        .plug_request(slot1_plug_request), .plug_response(`SP_BUS_RSP'd0)
    );

    wire [7:0] red, green, blue;
    wire machine_de, machine_hs, machine_vs;
    spectrum_video video (
        .clk_sys(clk_sys), .pixel_we(pixel_we), .pixel_row(pixel_row),
        .pixel_chunk(pixel_chunk), .pixel_data(pixel_data), .frame_done(frame_done),
        .pixel_clk(pixel_clk), .red(red), .green(green), .blue(blue),
        .de(machine_de), .hsync(machine_hs), .vsync(machine_vs)
    );

    wire [27:0] hdmi_video;
    // Shipped HDMI contract: the shared fes.video.session-display plane,
    // backed by fes.memory.hps-ddr. It blends launcher frames over the machine
    // word and delays RGB and sync together. A closed plane returns machine
    // pixels on the next frame boundary and does not reprogram the core.
    fes_display_plane #(
        .PICTURE_LEFT(16'd160), .PICTURE_TOP(16'd0),
        .PICTURE_WIDTH(16'd960), .PICTURE_HEIGHT(16'd720)
    ) display (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(~pixel_locked),
        .exec_reset(exec_reset), .request(display_request), .opcode(display_opcode),
        .index(display_index), .argument(display_argument),
        .response_valid(display_response_valid), .response_error(display_response_error),
        .response_data(display_response_data), .quiesced_sys(display_quiesced),
        .machine({1'b1, machine_vs, machine_hs, machine_de, red, green, blue}),
        .video(hdmi_video), .ui_active(), .locked(), .displayed_sequence(), .underflows(), .faulted(),
        .ddr_hold(ddr_hold), .ddr_reset(ddr0_reset), .ddr_drained(ddr0_drained),
        .ddr_address(ddr0_address), .ddr_burstcount(ddr0_burstcount), .ddr_read(ddr0_read),
        .ddr_waitrequest(ddr0_waitrequest), .ddr_readdata(ddr0_readdata),
        .ddr_readdatavalid(ddr0_readdatavalid)
    );
    // Port 0 reads launcher frames in the pixel clock; ports 1 and 2 read
    // and write the tape and disk in the system clock.
    fes_hps_ddr #(.P0_WRITE_ENABLE(1'b0), .P1_ENABLE(1'b1), .P2_ENABLE(DISK)) memory (
        .hold(ddr_hold),
        .p0_clk(pixel_clk), .p0_reset(ddr0_reset), .p0_drained(ddr0_drained),
        .p0_address(ddr0_address), .p0_burstcount(ddr0_burstcount),
        .p0_waitrequest(ddr0_waitrequest), .p0_readdata(ddr0_readdata),
        .p0_readdatavalid(ddr0_readdatavalid), .p0_read(ddr0_read),
        .p0_writedata(128'd0), .p0_byteenable(16'd0), .p0_write(1'b0),
        .p1_clk(clk_sys), .p1_reset(ddr1_reset), .p1_drained(),
        .p1_address(ddr1_address), .p1_burstcount(ddr1_burstcount),
        .p1_waitrequest(ddr1_waitrequest), .p1_readdata(ddr1_readdata),
        .p1_readdatavalid(ddr1_readdatavalid), .p1_read(ddr1_read),
        .p1_writedata(ddr1_writedata), .p1_byteenable(ddr1_byteenable), .p1_write(ddr1_write),
        .p2_clk(clk_sys), .p2_reset(ddr2_reset), .p2_drained(),
        .p2_address(ddr2_address), .p2_burstcount(ddr2_burstcount),
        .p2_waitrequest(ddr2_waitrequest), .p2_readdata(ddr2_readdata),
        .p2_readdatavalid(ddr2_readdatavalid), .p2_read(ddr2_read),
        .p2_writedata(ddr2_writedata), .p2_byteenable(ddr2_byteenable), .p2_write(ddr2_write)
    );
    assign HDMI_TX_D = hdmi_video[23:0];
    assign HDMI_TX_DE = hdmi_video[24];
    assign HDMI_TX_HS = hdmi_video[25];
    assign HDMI_TX_VS = hdmi_video[26];
    assign HDMI_TX_CLK = pixel_clk;

    wire signed [15:0] audio_sample;
    spectrum_audio audio_mix (
        .clk(clk_sys), .reset(exec_reset), .tick(tick), .mute(turbo), .speaker(speaker), .mic(mic),
        .ear(ear), .ay_pcm(ay_pcm), .slot_audio(slot_audio), .sample(audio_sample)
    );
    fes_audio_output audio (
        .source_clk(clk_sys), .audio_clk(audio_clk), .locked(audio_locked), .hold(exec_reset),
        .left_sample(audio_sample), .right_sample(audio_sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
    assign HDMI_MCLK = audio_clk;
endmodule
